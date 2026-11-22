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

	"github.com/system-inc/cohere/internal/go_engine/bounded"
	"github.com/system-inc/cohere/internal/go_engine/cache"
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
// each of those once. It refuses a root that is not a Go root, and a path outside the root. The editor's save is
// scoped to its one path, and that file is read as the buffer (invocation.read). The go commands that wait on neither
// the census nor the reads start beside them (earlyStart); a refusal joins them before it returns, and a description
// keeps what still runs for the pipeline to join before its first load (describedProject.awaitStarted).
func describeProject(request invocation) (described *describedProject, describeError error) {
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
	children := &load.Children{}
	// contents are every readable file's bytes as readOurs read them, which the build constraints are judged from, so
	// none is opened again. go env starts before they are read, and its build context opens a file through held, which
	// reads them: load.constraintContext only stores held as the context's OpenFile and never calls it as it makes
	// the context, and the context is first opened through by the judging below, once readOurs has filled the map, so
	// nothing reads it before it is written.
	var contents map[string][]byte
	held := func(file string) ([]byte, bool) {
		fileContents, isRead := contents[file]
		return fileContents, isRead
	}
	// Only a run whose lint phase is asked for reads them, since that phase is where every extra build is loaded and
	// checked (runLint): a file said checked by a build is one that build's run checks.
	readsBuilds := request.checksEveryBuild() && request.requests(contract.PhaseLint)
	// Under `--timing` the lister states and the builds' go commands run where they always have, one at a time
	// (startChildrenFirst), so each measurement of their children's CPU is of its own commands alone. A run that asks
	// for neither types nor lint, as the editor's save, may never load, so it fetches no lister state it might not
	// read.
	timed := request.switches["timing"]
	loads := request.requests(contract.PhaseTypes) || request.requests(contract.PhaseLint)
	// Opened before the go commands start, since the early start reads and writes each build's build context there,
	// and handed to the pipeline, so the run has one system cache, one store and one set of shard stamps (newPipeline).
	shardStamps := cache.NewShardStamps(nil)
	system := openSystemCache(request, shardStamps)
	early := startEarly(workspace, held, children, system.contexts(), !timed && loads, readsBuilds,
		request.hostFetchHeld)
	defer func() {
		// A refusal leaves no go command running.
		if describeError != nil {
			early.wait()
		}
	}()
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
	if request.stdinPath != "" {
		// Unchecked, unlike a path to narrow to: a save of a file outside the root, or of one not on disk yet, is not
		// refused but declined, as a file the run holds nothing for (runStdin).
		scope, scopeDescription = []string{request.stdinPath}, request.stdinPath
	}

	described = &describedProject{
		workspace:          workspace,
		census:             census,
		manifest:           manifest,
		inScopeSet:         map[string]bool{},
		constrainedInScope: map[string]bool{},
		contents:           map[string][]byte{},
		children:           children,
		started:            early,
		systemCache:        system,
		shardStamps:        shardStamps,
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
			contract.ExcludedFile{File: exclusion.File, Reason: exclusion.Reason, Source: exclusion.Source})
	}

	// In a run that reads builds, each file's read is held for the builds' selection, which takes it as soon as it is
	// read (heldReads); a run that reads none makes no slot. Abandoned on the way out: nothing returns between here and
	// readOurs filling every slot, so it guards a panic alone, which leaves no slot a reader waits on, and a refusal's
	// join above, deferred first, runs after it.
	var reads *heldReads
	if readsBuilds {
		reads = newHeldReads(census.Ours)
		defer reads.abandon()
		// Started here, beside the reads, rather than beside the census: nothing between them refuses the run, and it
		// waits on go env and the ports first all the same.
		early.startReadingBuilds(workspace, census, request.targets, !timed, children, reads.read)
	}
	var readable []string
	var unread []unreadFile
	readable, contents, unread = readOurs(census.Ours, reads, request.read, request.switches["single-threaded"])
	for _, failed := range unread {
		// A file nothing could read is ours until something says otherwise; inside the scope it is a gap the run
		// names, and outside it the load says what it makes of the file.
		if insideScope(failed.file, scope) {
			described.unreadable = append(described.unreadable,
				contract.UnreadableRecord{File: failed.file, Error: unreadableSentence(failed.readError)})
			described.inScope = append(described.inScope, failed.file)
		}
		described.record.FilesOurs++
	}
	described.readableOurs = readable
	// Only the files read are judged, from the bytes just read, every readable file and not only those in scope; a file
	// the judging had to open and could not would refuse all.
	goEnvironment, goEnvironmentError := early.hostGoEnvironment()
	constrained, constraintsError := load.ExcludedByConstraints(readable, goEnvironment, goEnvironmentError)
	if constraintsError != nil {
		return nil, fmt.Errorf("describing %s: %w", root, constraintsError)
	}
	// A census that judged no file keeps no answer and reads no builds, so each load asks go env for its own, and the
	// early answer goes unread.
	if len(readable) > 0 {
		described.goEnvironment = goEnvironment
		described.builds, described.unbuiltReasons, described.buildsError = early.awaitBuilds()
		if refused, isRefused := errors.AsType[refusedTarget](described.buildsError); isRefused {
			return nil, refused
		}
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
			// In a run of every build a file an extra build compiles is checked by it, and said so; one no build
			// compiles keeps the constraint's reason, a gap, as every such file does under `--host-build-only`.
			checkedBy, checked, checkedError := checkingBuild(described.builds, file, contents[file])
			if checkedError != nil {
				return nil, fmt.Errorf("describing %s: %w", root, checkedError)
			}
			if checked {
				reason = project.CheckedByBuild(checkedBy)
			} else if unbuiltReason, found := described.unbuiltReasons[file]; found {
				// Why no build compiles it, which says more than the host's constraint alone: no target platform
				// compiles it, or only another architecture does.
				reason = unbuiltReason
			}
			described.record.Excluded = append(described.record.Excluded,
				contract.ExcludedFile{File: file, Reason: reason})
			if insideScope(file, scope) {
				described.constrainedInScope[file] = true
				// Kept for another build that compiles it, whose //cohere:ignore comments here the merge across
				// builds reads (#72n9mq0); no phase of the host's build reads it.
				described.contents[file] = contents[file]
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

// unreadFile is a file of ours nothing could read, and why.
type unreadFile struct {
	file      string
	readError error
}

// readOurs reads each of files through read, as many at once as there are cores unless singleThreaded, holding each
// in its slot of reads as it is read when reads is not nil, and returns those it read, in files' order, with their
// bytes, and those it could not, in files' order too. Each read lands in its own file's place, so what the run names
// and in what order is the same whichever read finishes first. reads, when not nil, is of files.
//
// Mutates: reads holds every file's read
func readOurs(
	files []string, reads *heldReads, read func(file string) ([]byte, error), singleThreaded bool,
) ([]string, map[string][]byte, []unreadFile) {
	readContents := make([][]byte, len(files))
	readErrors := make([]error, len(files))
	bounded.ForEach(len(files), singleThreaded, func(index int) {
		readContents[index], readErrors[index] = read(files[index])
		if reads != nil {
			reads.hold(index, readContents[index], readErrors[index])
		}
	})
	var readable []string
	contents := make(map[string][]byte, len(files))
	var unread []unreadFile
	// Every read is written by the time ForEach has returned, so nothing waits here.
	for index, file := range files {
		if readErrors[index] != nil {
			unread = append(unread, unreadFile{file: file, readError: readErrors[index]})
			continue
		}
		readable = append(readable, file)
		contents[file] = readContents[index]
	}
	return readable, contents, unread
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
