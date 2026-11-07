package go_engine

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// moduleDescription is one owned module as the project record names it.
type moduleDescription struct {
	// directory is absolute.
	directory string
	path      string
	goVersion string
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
	scope, scopeDescription, scopeError := readScope(root, request.paths)
	if scopeError != nil {
		return nil, scopeError
	}

	described := &describedProject{
		workspace:          workspace,
		manifest:           manifest,
		inScopeSet:         map[string]bool{},
		constrainedInScope: map[string]bool{},
		contents:           map[string][]byte{},
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

	contents := map[string][]byte{}
	var readable []string
	for _, file := range census.Ours {
		fileContents, readError := os.ReadFile(file)
		if readError != nil {
			// A file nothing could read is ours until something says otherwise; inside the scope it is a gap
			// the run names, and outside it the load says what it makes of the file.
			if insideScope(file, scope) {
				described.unreadable = append(described.unreadable,
					contract.UnreadableRecord{File: file, Error: unreadableSentence(readError)})
				described.inScope = append(described.inScope, file)
			}
			described.record.FilesOurs++
			continue
		}
		contents[file] = fileContents
		readable = append(readable, file)
	}
	described.readableOurs = readable
	// Only the files read are judged, since the judging reads each again and a file it cannot open refuses all.
	constrained, constraintsError := load.ExcludedByConstraints(root, workspace.Environment(), readable)
	if constraintsError != nil {
		return nil, fmt.Errorf("describing %s: %w", root, constraintsError)
	}
	constraintReasons := map[string]string{}
	for _, excluded := range constrained {
		constraintReasons[excluded.File] = excluded.Reason
	}
	for _, file := range readable {
		// A generated file stays ours: format.Run skips it, and the driver keeps vet's findings there and withholds
		// a house rule's, so excluding it here would hide the bugs a generator ships.
		reason, excluded := constraintReasons[file]
		if excluded {
			described.record.Excluded = append(described.record.Excluded,
				contract.ExcludedFile{File: file, Reason: reason})
			if insideScope(file, scope) {
				described.constrainedInScope[file] = true
			}
			continue
		}
		described.record.FilesOurs++
		if insideScope(file, scope) {
			described.inScope = append(described.inScope, file)
			described.contents[file] = contents[file]
		}
	}
	slices.Sort(described.inScope)
	for _, file := range described.inScope {
		described.inScopeSet[file] = true
	}
	slices.SortFunc(described.record.Excluded, func(left contract.ExcludedFile, right contract.ExcludedFile) int {
		return cmp.Compare(left.File, right.File)
	})
	described.record.FilesInScope = len(described.inScope)
	described.record.ElapsedMilliseconds = int(time.Since(started).Milliseconds())
	return described, nil
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

// unreadableSentence says why a file could not be read, without the path the record already names.
func unreadableSentence(readError error) string {
	var pathError *fs.PathError
	if errors.As(readError, &pathError) {
		return pathError.Err.Error()
	}
	return readError.Error()
}
