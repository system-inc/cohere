package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/types/program"
)

// Discovery is how one cohere checks a repository that holds more than one project: TypeScript programs,
// Swift packages and Go workspaces and modules side by side, or nested below one another (#f9nftxz). From
// the root it walks every directory the repository keeps, and each tsconfig.json, Package.swift, go.work or
// go.mod it meets is a project, run by its own engine. A directory holding a go.work and a go.mod is one Go
// project, the workspace, and a go.mod whose directory a workspace's go.work uses is not a project of its
// own: the workspace's run builds it, as go does.
//
// What the walk never enters is the whole of its correctness, since the markers are everywhere a
// repository keeps other people's code:
//
//   - Whatever .gitignore ignores where cohere runs (Kirk, 2026-10-04: "if its gitignored where you run
//     cohere we skip it"). Ignored means not this repository's: ahra ignores every clone under
//     projects/, and each of those has its own gate.
//   - node_modules, .build, .cache, .git and Go's testdata, ignored or not. node_modules holds packages'
//     tsconfig.json files, cohere's own swift/.build/checkouts holds dozens of SwiftPM checkouts with a
//     Package.swift each, and testdata holds fixtures, tsconfigs among them.
//   - A directory with a repository of its own, a clone or a submodule. Its projects are its own to
//     discover, and one inside a program, such as Structure or Base, is checked as part of that program,
//     its drift reported read-only, as it always was.
//
// A marker the root's ignorePatterns match is refused rather than run, and named.

// neverDescended are the directory names discovery never enters, whatever any ignore file says.
var neverDescended = map[string]bool{
	"node_modules": true,
	".build":       true,
	".cache":       true,
	".git":         true,
	"testdata":     true,
}

// discoveryParallelism is how many directories discovery reads at once.
const discoveryParallelism = 16

// discoveredProject is one project discovery found.
type discoveredProject struct {
	// Directory is the project's root, relative to the discovery root and slash-separated: "." for the
	// root itself.
	Directory string
	Engine    projectEngine

	// ConfigFile is a TypeScript project's tsconfig when it is not the tsconfig.json in Directory: one a
	// solution root references by another name, such as Vite's tsconfig.app.json. Empty for the rest.
	ConfigFile string
}

// discovery is everything one walk found, including what it passed over and why.
type discovery struct {
	// Root is the absolute directory the walk started from.
	Root string

	// Projects are in walk order, the root's first, and a directory holding more than one engine's marker is
	// a project for each, TypeScript's first, then Swift's, then Go's.
	Projects []discoveredProject

	// Refused are markers the root's ignorePatterns match, relative to Root.
	Refused []string

	// UnusedModules are Go modules below a discovered workspace whose go.work does not use them, each run as
	// a module of its own, for the notes to say why.
	UnusedModules []unusedModule

	// BrokenWorkspaces are Go workspaces whose go.work does not parse. Each still runs, and fails on it, while
	// the modules below it run on their own, so one bad file fails its own project and nothing else.
	BrokenWorkspaces []brokenWorkspace

	// NestedRepositories are directories holding an independent repository of their own, relative to Root,
	// not entered, and Submodules counts the submodules, part of the program above them, not entered either.
	NestedRepositories []string
	Submodules         int

	// Ignored counts the directories .gitignore ignores, and NeverDescended the dependency, build, cache
	// and fixture directories, none of them entered.
	Ignored        int
	NeverDescended int

	// Ownership is what reading the TypeScript projects decided. See ownership.go.
	Ownership ownership

	// SwiftCoverage is what asking the Swift projects which packages their runs cover decided. See
	// swift_coverage.go.
	SwiftCoverage swiftCoverage

	// Listings are every directory listing the walk read, for the build to serve its tsconfig's include
	// enumeration from rather than listing the same directories again (#bjv0tg4).
	Listings *program.DirectoryListings
}

// unusedModule is a Go module that runs on its own though a workspace sits above it, which a reader who
// expected the workspace to build it would want named.
type unusedModule struct {
	// Directory is the module's, and Workspace the nearest discovered workspace's above it, both relative to
	// the discovery root and slash-separated.
	Directory string
	Workspace string
}

// brokenWorkspace is a Go workspace whose go.work discovery could not read, and why.
type brokenWorkspace struct {
	// Directory is the workspace's, relative to the discovery root and slash-separated.
	Directory string
	Reason    string
}

// ignoreScope is what discovery asks of the gitignore matcher for one directory: internal/gitignore's
// Matcher (#ndtgy1w), whose paths are relative to the repository's root. Enter returns the scope of a
// directory below, having read every .gitignore between, and fails on a file it cannot read faithfully
// rather than skipping it. Ignored answers for an entry of the scope's own directory.
type ignoreScope interface {
	Enter(relativeDirectory string) (ignoreScope, error)
	Ignored(relativePath string, isDirectory bool) bool
}

// newIgnoreScope builds the scope of a repository's root. A variable so a test can lift every ignore.
var newIgnoreScope = newGitignoreScope

// discoveryRoot is where discovery starts: the root the single-project walk up chose, unless the caller is
// standing in a repository that holds no marker at its own level or above inside it. Then it is that
// repository's root, which is how cohere's own repository, with Swift in swift/ and nothing at its root,
// finds its package. Outside any repository and any project it is the working directory.
func discoveryRoot(location projectLocation, locateError error, workingDirectory string) string {
	repository := enclosingRepository(workingDirectory)
	if locateError == nil {
		// A marker above the repository the caller is in belongs to some other project.
		if repository != "" && strings.HasPrefix(repository, location.Root+string(filepath.Separator)) {
			return repository
		}
		return location.Root
	}
	if repository != "" {
		return repository
	}
	return workingDirectory
}

// enclosingRepository is the nearest directory at or above start holding a .git directory, an independent
// repository's root, or "" when there is none. A submodule's .git is a file, so a walk up from inside one
// passes it, the way locateProject does.
func enclosingRepository(start string) string {
	directory := filepath.Clean(start)
	for {
		if information, statError := os.Stat(
			filepath.Join(directory, ".git"),
		); statError == nil && information.IsDir() {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

// nestedRepository is what kind of repository, if any, a directory is the root of.
type nestedRepository int

const (
	noRepository nestedRepository = iota

	// cloneRepository has a .git directory: an independent repository with its own gate.
	cloneRepository

	// linkedRepository has a .git file pointing elsewhere: a submodule, or a worktree.
	linkedRepository
)

// nearestRepository is the nearest directory at or above start that is a repository's root of either kind,
// a clone or a submodule, whose ignore files are the ones that govern start, or "" when there is none.
func nearestRepository(start string) string {
	directory := filepath.Clean(start)
	for {
		if repositoryKind(directory) != noRepository {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

func repositoryKind(directory string) nestedRepository {
	information, statError := os.Lstat(filepath.Join(directory, ".git"))
	switch {
	case statError != nil:
		return noRepository
	case information.IsDir():
		return cloneRepository
	default:
		return linkedRepository
	}
}

// discoverProjects walks root and returns every project it finds. ignorePatterns are the root's, as lint
// globs relative to root.
func discoverProjects(root string, ignorePatterns []string) (discovery, error) {
	found := discovery{Root: root, Listings: program.NewDirectoryListings()}

	// The matcher is the repository's, a submodule's own when the root is inside one, so the .gitignore
	// files between its root and this one apply too.
	repository := nearestRepository(root)
	if repository == "" {
		repository = root
	}
	rootScope, err := newIgnoreScope(repository)
	if err != nil {
		return discovery{}, err
	}
	fromRepository := func(relative string) string {
		// Ignored: repository is root or a directory above it, both absolute, and Rel fails only between paths
		// with no relative way from one to the other.
		inRepository, _ := filepath.Rel(repository, filepath.Join(root, relative))
		inRepository = filepath.ToSlash(inRepository)
		if inRepository == "." {
			return ""
		}
		return inRepository
	}
	if prefix := fromRepository("."); prefix != "" {
		if rootScope, err = rootScope.Enter(prefix); err != nil {
			return discovery{}, err
		}
	}

	// Directories are read in parallel, since the walk is reading directories and little else: on ahra,
	// 3,138 of them cost 42ms one at a time. Each carries its own ignore scope, which is immutable, and the
	// lock guards found.
	var lock sync.Mutex
	var group sync.WaitGroup
	reading := make(chan struct{}, discoveryParallelism)
	var firstError error
	var workspaces []string
	fail := func(err error) {
		lock.Lock()
		defer lock.Unlock()
		if firstError == nil {
			firstError = err
		}
	}
	// visit reads one directory. Below the root, a directory holding a .git is a repository of its own and
	// is counted, not entered; its listing says so, which spares a stat per directory. Anything else is
	// entered, scope taking that directory's .gitignore, and searched.
	var visit func(relative string, scope ignoreScope)
	visit = func(relative string, scope ignoreScope) {
		defer group.Done()
		reading <- struct{}{}
		entries, err := os.ReadDir(filepath.Join(root, relative))
		<-reading
		if err != nil {
			fail(fmt.Errorf("reading %s: %w", filepath.Join(root, relative), err))
			return
		}
		found.Listings.Add(filepath.Join(root, relative), entries)

		names := map[string]bool{}
		for _, entry := range entries {
			if entry.Name() == ".git" && relative != "." {
				lock.Lock()
				if entry.IsDir() {
					found.NestedRepositories = append(found.NestedRepositories, relative)
				} else {
					// A submodule is part of the program above it and is checked there, its drift
					// reported read-only, as it always was; it is not a project of its own to find.
					found.Submodules++
				}
				lock.Unlock()
				return
			}
			if !entry.IsDir() {
				names[entry.Name()] = true
			}
		}
		directoryScope := scope
		if relative != "." {
			if directoryScope, err = scope.Enter(fromRepository(relative)); err != nil {
				fail(err)
				return
			}
		}
		lock.Lock()
		// A go.work found here makes the directory's go.mod part of its project rather than one of its own.
		workspaceHere := false
		for _, marker := range []struct {
			name   string
			engine projectEngine
		}{
			{projectMarker, engineTypeScript},
			{swiftProjectMarker, engineSwift},
			{goWorkspaceMarker, engineGo},
			{goModuleMarker, engineGo},
		} {
			if !names[marker.name] || (marker.name == goModuleMarker && workspaceHere) {
				continue
			}
			markerPath := path.Join(relative, marker.name)
			if configuration.MatchAny(ignorePatterns, markerPath) {
				found.Refused = append(found.Refused, markerPath)
				continue
			}
			found.Projects = append(found.Projects, discoveredProject{Directory: relative, Engine: marker.engine})
			if marker.name == goWorkspaceMarker {
				workspaceHere = true
				workspaces = append(workspaces, relative)
			}
		}
		candidates := []string{}
		for _, entry := range entries {
			// A symbolic link is not followed: it is a way out of the repository, or a loop.
			if !entry.IsDir() {
				continue
			}
			child := path.Join(relative, entry.Name())
			if neverDescended[entry.Name()] {
				// A repository's own .git is not something a reader would count as passed over.
				if entry.Name() != ".git" {
					found.NeverDescended++
				}
				continue
			}
			if directoryScope.Ignored(fromRepository(child), true) {
				found.Ignored++
				continue
			}
			candidates = append(candidates, child)
		}
		lock.Unlock()

		for _, child := range candidates {
			group.Add(1)
			go visit(child, directoryScope)
		}
	}
	group.Add(1)
	visit(".", rootScope)
	group.Wait()
	if firstError != nil {
		return discovery{}, firstError
	}

	found.sortProjects()
	sort.Strings(found.Refused)
	sort.Strings(found.NestedRepositories)
	found.claimWorkspaceModules(workspaces)
	return found, nil
}

// claimWorkspaceModules removes from Projects each Go module a discovered workspace's go.work uses, since the
// workspace's run builds it, and keeps, for the notes, each module left below a workspace that does not use
// it. workspaces are the Go workspace projects' directories, relative to Root.
//
// Each use resolves against its workspace's directory. One outside the root names nothing discovery found,
// and so does one inside a submodule or a nested repository, since the walk never enters them, so neither
// claims anything. A use naming another workspace claims nothing either: go builds that one by its own go.work.
// A go.work that does not parse claims nothing and is kept in BrokenWorkspaces, so it fails its own project's
// run rather than the discovery every other project depends on.
func (found *discovery) claimWorkspaceModules(workspaces []string) {
	if len(workspaces) == 0 {
		return
	}
	isWorkspace := map[string]bool{}
	claimed := map[string]bool{}
	broken := map[string]bool{}
	for _, workspace := range workspaces {
		isWorkspace[workspace] = true
		uses, readError := workspaceUses(filepath.Join(found.Root, filepath.FromSlash(workspace)))
		if readError != nil {
			// Not a failure of discovery: a TypeScript or Swift project beside it has nothing to do with it. The
			// workspace's own run fails on the file, and it claims nothing, so its modules are still checked.
			found.BrokenWorkspaces = append(found.BrokenWorkspaces,
				brokenWorkspace{Directory: workspace, Reason: readError.Error()})
			broken[workspace] = true
			continue
		}
		for _, use := range uses {
			module := path.Join(workspace, use)
			if module == ".." || strings.HasPrefix(module, "../") {
				continue
			}
			claimed[module] = true
		}
	}
	running := found.Projects[:0:0]
	for _, project := range found.Projects {
		if project.Engine == engineGo && !isWorkspace[project.Directory] {
			// Any workspace that uses a module claims it, the nearest or not: that workspace's run builds the
			// module through the use, so running the module alone as well would check its files twice. The walk
			// up from inside such a module can still stop at the module (goModuleRoot), because go standing
			// there reads the nearer go.work, which does not use it; both builds are ones go itself would run.
			if claimed[project.Directory] {
				continue
			}
			// A module below a workspace that could not be read is named by that workspace's note instead.
			if workspace, below := nearestWorkspace(project.Directory, workspaces); below && !broken[workspace] {
				found.UnusedModules = append(found.UnusedModules,
					unusedModule{Directory: project.Directory, Workspace: workspace})
			}
		}
		running = append(running, project)
	}
	found.Projects = running
}

// nearestWorkspace is the deepest of the workspaces' directories that holds a directory, and whether there is
// one: the go.work go would read there.
func nearestWorkspace(directory string, workspaces []string) (string, bool) {
	nearest, below := "", false
	for _, workspace := range workspaces {
		if isBelow(directory, workspace) && (!below || isBelow(workspace, nearest)) {
			nearest, below = workspace, true
		}
	}
	return nearest, below
}

// sortProjects puts parents before children, TypeScript, then Swift, then Go in one directory, and a
// directory's tsconfig.json before its other tsconfigs, whatever order the reads finished in, so the report
// reads the same on every run.
func (found *discovery) sortProjects() {
	sort.SliceStable(found.Projects, func(left, right int) bool {
		leftProject, rightProject := found.Projects[left], found.Projects[right]
		if leftProject.Directory != rightProject.Directory {
			return pathBefore(leftProject.Directory, rightProject.Directory)
		}
		if leftProject.Engine != rightProject.Engine {
			return engineRank(leftProject.Engine) < engineRank(rightProject.Engine)
		}
		return leftProject.ConfigFile < rightProject.ConfigFile
	})
}

// engineRank is where an engine's project comes among the projects of one directory, the order engineAt
// reads the markers in, so the report lists them the way a single run would choose between them.
func engineRank(engine projectEngine) int {
	switch engine {
	case engineTypeScript:
		return 0
	case engineSwift:
		return 1
	case engineGo:
		return 2
	default:
		return 3
	}
}

// pathBefore orders slash paths segment by segment, so "." comes first and a directory comes before
// everything inside it.
func pathBefore(left string, right string) bool {
	if left == "." || right == "." {
		return left == "." && right != "."
	}
	leftSegments, rightSegments := strings.Split(left, "/"), strings.Split(right, "/")
	for index := 0; index < len(leftSegments) && index < len(rightSegments); index++ {
		if leftSegments[index] != rightSegments[index] {
			return leftSegments[index] < rightSegments[index]
		}
	}
	return len(leftSegments) < len(rightSegments)
}

// rootIgnorePatterns reads the ignorePatterns of the settings file at the root, as globs relative to it.
// A root with no settings file has none.
func rootIgnorePatterns(root string) ([]string, error) {
	settings := filepath.Join(root, "CohereSettings.json")
	if !isRegularFile(settings) {
		return nil, nil
	}
	return configuration.IgnorePatternsOf(settings)
}
