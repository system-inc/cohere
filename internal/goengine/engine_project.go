package goengine

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"github.com/system-inc/cohere/internal/goengine/contract"
	"github.com/system-inc/cohere/internal/goengine/project"
)

// generatedReason is why a generated file is not ours: its output is its generator's to fix.
const generatedReason = "generated, marked // Code generated ... DO NOT EDIT"

// leadingUnderscoreReason is why go builds a file no package holds that no constraint excluded either.
const leadingUnderscoreReason = "ignored by go, which builds no file whose name starts with _ or ."

// buildTarget is what go builds for under the run's environment, which decides the files build constraints
// leave out.
type buildTarget struct {
	operatingSystem string
	architecture    string
	cgoEnabled      bool
}

// moduleDescription is one owned module as the project record names it.
type moduleDescription struct {
	// directory is absolute.
	directory string
	path      string
	goVersion string
}

// describedProject is what the run looks at, read once before the first record, since each part of it can
// refuse the run and a refusal must come before any record.
type describedProject struct {
	workspace project.Workspace

	// manifest is the root's go.work, or its go.mod for a module root: where a diagnostic that names no file
	// is placed, since the front door refuses a finding with no position.
	manifest string

	// record is the project record.
	record contract.ProjectRecord

	// inScope are the files that are ours, not excluded, and inside the paths, sorted; unreadable ones too.
	inScope    []string
	inScopeSet map[string]bool

	// contents are the in-scope files that could be read, as they were read.
	contents map[string][]byte

	// unreadable are the in-scope files that could not be read, each written before the first phase.
	unreadable []contract.UnreadableRecord
}

// describeProject reads the workspace, takes its census, decides which files are ours and in scope, and reads
// each of those once. It refuses a root that is not a Go root, and a path outside the root.
func describeProject(request invocation) (*describedProject, error) {
	started := time.Now()
	root := filepath.Clean(request.root)
	information, statError := os.Stat(root)
	if statError != nil {
		return nil, fmt.Errorf("reading the root %s: %w", root, statError)
	}
	if !information.IsDir() {
		return nil, fmt.Errorf("the root %s is not a directory", root)
	}
	manifest := ""
	for _, name := range []string{"go.work", "go.mod"} {
		if _, manifestError := os.Stat(filepath.Join(root, name)); manifestError == nil {
			manifest = filepath.Join(root, name)
			break
		}
	}
	if manifest == "" {
		return nil, fmt.Errorf("%s holds neither a go.work nor a go.mod, so it is not a Go root", root)
	}

	workspace, workspaceError := project.ReadWorkspace(root)
	if workspaceError != nil {
		return nil, fmt.Errorf("reading the workspace at %s: %w", root, workspaceError)
	}
	census, censusError := project.TakeCensus(workspace)
	if censusError != nil {
		return nil, fmt.Errorf("taking the census of %s: %w", root, censusError)
	}
	modules, modulesError := describeModules(workspace)
	if modulesError != nil {
		return nil, modulesError
	}
	target, targetError := readBuildTarget(root, workspace.Environment())
	if targetError != nil {
		return nil, targetError
	}
	scope, scopeDescription, scopeError := readScope(root, request.paths)
	if scopeError != nil {
		return nil, scopeError
	}

	described := &describedProject{
		workspace:  workspace,
		manifest:   manifest,
		inScopeSet: map[string]bool{},
		contents:   map[string][]byte{},
	}
	described.record = contract.ProjectRecord{
		Root:             root,
		Package:          packageName(workspace, modules),
		FilesInPackage:   len(census.Files),
		ScopeDescription: scopeDescription,
		Targets:          targets(modules, census.Files),
		Excluded:         []contract.ExcludedFile{},
	}
	for _, exclusion := range census.Excluded {
		described.record.Excluded = append(described.record.Excluded,
			contract.ExcludedFile{File: exclusion.File, Reason: exclusion.Reason})
	}

	for _, file := range census.Ours {
		contents, readError := os.ReadFile(file)
		inScope := insideScope(file, scope)
		if readError != nil {
			// A file nothing could read is ours until something says otherwise; inside the scope it is a gap
			// the run names, and outside it the load says what it makes of the file.
			if inScope {
				described.unreadable = append(described.unreadable,
					contract.UnreadableRecord{File: file, Error: unreadableSentence(readError)})
				described.addInScope(file)
			}
			described.record.FilesOurs++
			continue
		}
		if reason, excluded := exclusionOf(target, file, contents); excluded {
			described.record.Excluded = append(described.record.Excluded,
				contract.ExcludedFile{File: file, Reason: reason})
			continue
		}
		described.record.FilesOurs++
		if inScope {
			described.addInScope(file)
			described.contents[file] = contents
		}
	}
	slices.SortFunc(described.record.Excluded, func(left contract.ExcludedFile, right contract.ExcludedFile) int {
		return cmp.Compare(left.File, right.File)
	})
	described.record.FilesInScope = len(described.inScope)
	described.record.ElapsedMilliseconds = int(time.Since(started).Milliseconds())
	return described, nil
}

// addInScope records one file as in scope. census.Ours is sorted, so inScope stays sorted.
func (described *describedProject) addInScope(file string) {
	described.inScope = append(described.inScope, file)
	described.inScopeSet[file] = true
}

// readable are the in-scope files that could be read, in order: the files a phase that reads source is given.
func (described *describedProject) readable() []string {
	files := make([]string, 0, len(described.contents))
	for _, file := range described.inScope {
		if _, read := described.contents[file]; read {
			files = append(files, file)
		}
	}
	return files
}

// scopeWords says what the fix phase formatted, for its record's formatScope.
func (described *describedProject) scopeWords() string {
	if described.record.ScopeDescription != "" {
		return described.record.ScopeDescription
	}
	if described.workspace.FromWorkspace {
		return "every Go file in the workspace"
	}
	return "every Go file in the module"
}

// describeModules reads each owned module's go.mod for the name and the go version the project record gives it.
func describeModules(workspace project.Workspace) ([]moduleDescription, error) {
	modules := make([]moduleDescription, 0, len(workspace.Modules))
	for _, module := range workspace.Modules {
		directory := filepath.Join(workspace.Root, filepath.FromSlash(module.Directory))
		goModPath := filepath.Join(directory, "go.mod")
		contents, readError := os.ReadFile(goModPath)
		if readError != nil {
			return nil, fmt.Errorf("reading %s: %w", goModPath, readError)
		}
		parsed, parseError := modfile.ParseLax(goModPath, contents, nil)
		if parseError != nil {
			return nil, fmt.Errorf("parsing %s: %w", goModPath, parseError)
		}
		described := moduleDescription{directory: directory, path: module.Path}
		if parsed.Module != nil {
			described.path = parsed.Module.Mod.Path
		}
		if parsed.Go != nil {
			described.goVersion = parsed.Go.Version
		}
		modules = append(modules, described)
	}
	return modules, nil
}

// packageName is the project record's `package`: the workspace's directory name, or a module root's module path.
func packageName(workspace project.Workspace, modules []moduleDescription) string {
	if !workspace.FromWorkspace && len(modules) == 1 && modules[0].path != "" {
		return modules[0].path
	}
	return filepath.Base(workspace.Root)
}

// targets is one project target per module: its path (its directory when go.mod names none), and the files go
// sees in it. A file belongs to the module whose directory is the deepest one holding it, since an owned module
// nested in another is a module of its own.
func targets(modules []moduleDescription, files []string) []contract.ProjectTarget {
	counts := make([]int, len(modules))
	for _, file := range files {
		owner, ownerDepth := -1, -1
		for index, module := range modules {
			if insideScope(file, []string{module.directory}) && len(module.directory) > ownerDepth {
				owner, ownerDepth = index, len(module.directory)
			}
		}
		if owner >= 0 {
			counts[owner]++
		}
	}
	described := make([]contract.ProjectTarget, 0, len(modules))
	for index, module := range modules {
		name := cmp.Or(module.path, module.directory)
		described = append(described, contract.ProjectTarget{
			Name: name, Kind: "module", Files: counts[index], LanguageMode: module.goVersion,
		})
	}
	return described
}

// readScope checks each path the command line named and says what they narrow to, in words relative to the
// root. No paths is the whole project, described as nothing.
func readScope(root string, paths []string) ([]string, string, error) {
	if len(paths) == 0 {
		return []string{root}, "", nil
	}
	scope := make([]string, 0, len(paths))
	described := make([]string, 0, len(paths))
	for _, path := range paths {
		cleaned := filepath.Clean(path)
		relative, relativeError := filepath.Rel(root, cleaned)
		if relativeError != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, "", fmt.Errorf("%s is outside the root %s, so the run cannot narrow to it", path, root)
		}
		if _, statError := os.Stat(cleaned); statError != nil {
			return nil, "", fmt.Errorf("reading %s, a path the run was narrowed to: %w", path, statError)
		}
		scope = append(scope, cleaned)
		described = append(described, filepath.ToSlash(relative))
	}
	return scope, strings.Join(described, ", "), nil
}

// insideScope reports whether file is one of the paths, or inside one of them.
func insideScope(file string, scope []string) bool {
	for _, path := range scope {
		if file == path || strings.HasPrefix(file, path+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// readBuildTarget asks go what it builds for in root under the workspace's environment. Read here rather than
// taken from the load's, because the load runs after the fix phase and the project record comes first.
func readBuildTarget(root string, environment []string) (buildTarget, error) {
	command := exec.Command("go", "env", "GOOS", "GOARCH", "CGO_ENABLED")
	command.Dir = root
	command.Env = append(os.Environ(), environment...)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return buildTarget{}, fmt.Errorf("reading GOOS, GOARCH and CGO_ENABLED with go env in %s: %w: %s", root,
			runError, strings.TrimSpace(standardError.String()))
	}
	values := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(values) != 3 {
		return buildTarget{}, fmt.Errorf("reading GOOS, GOARCH and CGO_ENABLED with go env in %s: want three "+
			"lines, got %q", root, output)
	}
	cgoEnabled, parseError := strconv.ParseBool(values[2])
	if parseError != nil {
		return buildTarget{}, fmt.Errorf("reading CGO_ENABLED with go env in %s: %w", root, parseError)
	}
	return buildTarget{operatingSystem: values[0], architecture: values[1], cgoEnabled: cgoEnabled}, nil
}

// exclusionOf says why a file that is ours is left out anyway, from the contents already read: go would not
// build it for this target, or a generator wrote it.
func exclusionOf(target buildTarget, file string, contents []byte) (string, bool) {
	name := filepath.Base(file)
	if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
		return leadingUnderscoreReason, true
	}
	buildContext := build.Default
	buildContext.GOOS = target.operatingSystem
	buildContext.GOARCH = target.architecture
	buildContext.CgoEnabled = target.cgoEnabled
	buildContext.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(contents)), nil
	}
	constraintsReason := fmt.Sprintf("excluded by build constraints on %s/%s", target.operatingSystem,
		target.architecture)
	matched, matchError := buildContext.MatchFile(filepath.Dir(file), name)
	// A constraint go cannot read is an error the load reports against the file, so the file stays in.
	if matchError == nil && !matched {
		return constraintsReason, true
	}

	// The package clause, the imports and the comments above them are all that is read here. A file that does
	// not get that far is the fix and types phases' to report, so it stays in.
	header, parseError := parser.ParseFile(token.NewFileSet(), file, contents, parser.ImportsOnly|parser.ParseComments)
	if parseError != nil {
		return "", false
	}
	if ast.IsGenerated(header) {
		return generatedReason, true
	}
	// MatchFile keeps a cgo file whatever CGO_ENABLED says; go's own import leaves it out when cgo is off.
	if !target.cgoEnabled {
		for _, imported := range header.Imports {
			if imported.Path.Value == `"C"` {
				return constraintsReason + " with cgo off", true
			}
		}
	}
	return "", false
}

// unreadableSentence says why a file could not be read, without the path the record already names.
func unreadableSentence(readError error) string {
	var pathError *fs.PathError
	if errors.As(readError, &pathError) {
		return pathError.Err.Error()
	}
	return readError.Error()
}
