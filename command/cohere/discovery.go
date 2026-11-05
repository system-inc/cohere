package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/types/program"
)

// Discovery is how one cohere checks a repository that holds more than one project: TypeScript programs
// and Swift packages side by side, or nested below one another (#f9nftxz). From the root it walks every
// directory the repository keeps, and each tsconfig.json or Package.swift it meets is a project, run by
// its own engine.
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
//
// The walk also notes every .swift file it passes that no discovered Swift package's directory holds, since
// nothing checks it (#ybspptb). An app xcodegen generates has Swift sources and no Package.swift, and beside a
// TypeScript program, a green run read clean over code nobody looked at. Such files are named by the top
// directory holding them, in the notes and on the verdict line, and fail nothing: a run says what it covered
// as well as what it found. Files the walk never enters, or that .gitignore or the root's ignorePatterns
// match, are not counted, since nobody expects those checked.
//
// A file inside a package's directory that none of its targets compiles is not this walk's to judge: the
// directory claims it, and the engine knows which files it checks. ahraos-presence's CapturePhone sits inside
// the root package's directory, so naming it there is the engine's not-checked list made a gap, which the
// Xcode half of #ybspptb takes up.

// neverDescended are the directory names discovery never enters, whatever any ignore file says.
var neverDescended = map[string]bool{
	"node_modules": true,
	".build":       true,
	".cache":       true,
	".git":         true,
	"testdata":     true,
}

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

	// Projects are in walk order, the root's first, and a directory holding both markers is two projects,
	// TypeScript's first.
	Projects []discoveredProject

	// Refused are markers the root's ignorePatterns match, relative to Root.
	Refused []string

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

	// UnclaimedSwift are the top directories holding .swift files no discovered Swift package holds, which
	// no run checks, in path order.
	UnclaimedSwift []unclaimedSwiftDirectory

	// Listings are every directory listing the walk read, for the build to serve its tsconfig's include
	// enumeration from rather than listing the same directories again (#bjv0tg4).
	Listings *program.DirectoryListings
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
		if information, err := os.Stat(filepath.Join(directory, ".git")); err == nil && information.IsDir() {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

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

// nestedRepository is what kind of repository, if any, a directory is the root of.
type nestedRepository int

const (
	noRepository nestedRepository = iota

	// cloneRepository has a .git directory: an independent repository with its own gate.
	cloneRepository

	// linkedRepository has a .git file pointing elsewhere: a submodule, or a worktree.
	linkedRepository
)

func repositoryKind(directory string) nestedRepository {
	information, err := os.Lstat(filepath.Join(directory, ".git"))
	switch {
	case err != nil:
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
	var (
		lock       sync.Mutex
		group      sync.WaitGroup
		reading    = make(chan struct{}, discoveryParallelism)
		firstError error
		// swiftFiles counts the .swift files in each directory that holds any, by directory relative to root.
		swiftFiles = map[string]int{}
	)
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
		if relative != "." {
			if scope, err = scope.Enter(fromRepository(relative)); err != nil {
				fail(err)
				return
			}
		}
		// Counted from the listing already read, so noting Swift no package holds costs no pass of its own. A
		// file .gitignore or the root's ignorePatterns match is no more expected checked than an ignored
		// directory is.
		swiftFileCount := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".swift") {
				continue
			}
			filePath := path.Join(relative, entry.Name())
			if scope.Ignored(fromRepository(filePath), false) || configuration.MatchAny(ignorePatterns, filePath) {
				continue
			}
			swiftFileCount++
		}
		lock.Lock()
		if swiftFileCount > 0 {
			swiftFiles[relative] = swiftFileCount
		}
		for _, marker := range []struct {
			name   string
			engine projectEngine
		}{{projectMarker, engineTypeScript}, {swiftProjectMarker, engineSwift}} {
			if !names[marker.name] {
				continue
			}
			markerPath := path.Join(relative, marker.name)
			if configuration.MatchAny(ignorePatterns, markerPath) {
				found.Refused = append(found.Refused, markerPath)
				continue
			}
			found.Projects = append(found.Projects, discoveredProject{Directory: relative, Engine: marker.engine})
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
			if scope.Ignored(fromRepository(child), true) {
				found.Ignored++
				continue
			}
			candidates = append(candidates, child)
		}
		lock.Unlock()

		for _, child := range candidates {
			group.Add(1)
			go visit(child, scope)
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
	found.UnclaimedSwift = unclaimedSwiftDirectories(swiftFiles, found.Projects)
	return found, nil
}

// unclaimedSwiftDirectory is one top directory holding .swift files no discovered Swift package holds.
type unclaimedSwiftDirectory struct {
	// Directory is the shallowest directory under the root holding the files, relative to it and ending in
	// a slash, such as "CapturePhone/", or "." for files directly in the root.
	Directory string `json:"directory"`

	// Files counts the unclaimed .swift files at any depth below it.
	Files int `json:"files"`
}

// unclaimedSwiftDirectories is the .swift files no Swift package among projects holds, grouped by the top
// directory holding them. A package claims every file under its directory, whether it runs or another
// package's run covers it, since then the covering run checks its files. Every Swift package discovery found
// is one or the other, so the claim is decided here, before coverage is asked.
//
// The top directory is a path's first segment: that directory is outside every package, or the files below
// it would be claimed, so it is the shallowest one holding them. So CapturePhone/Sources/App and
// CapturePhone/Tests are both CapturePhone/.
func unclaimedSwiftDirectories(swiftFiles map[string]int, projects []discoveredProject) []unclaimedSwiftDirectory {
	var packages []string
	for _, project := range projects {
		if project.Engine == engineSwift {
			packages = append(packages, project.Directory)
		}
	}
	byTop := map[string]int{}
	for directory, count := range swiftFiles {
		if slices.ContainsFunc(packages, func(packageDirectory string) bool {
			return directory == packageDirectory || isBelow(directory, packageDirectory)
		}) {
			continue
		}
		top := "."
		if directory != "." {
			top, _, _ = strings.Cut(directory, "/")
			top += "/"
		}
		byTop[top] += count
	}
	unclaimed := make([]unclaimedSwiftDirectory, 0, len(byTop))
	for top, count := range byTop {
		unclaimed = append(unclaimed, unclaimedSwiftDirectory{Directory: top, Files: count})
	}
	// In path order, the root's first, so the report reads the same on every run.
	sort.Slice(unclaimed, func(left, right int) bool {
		return pathBefore(strings.TrimSuffix(unclaimed[left].Directory, "/"), strings.TrimSuffix(unclaimed[right].Directory, "/"))
	})
	return unclaimed
}

// sortProjects puts parents before children, TypeScript before Swift in one directory, and a directory's
// tsconfig.json before its other tsconfigs, whatever order the reads finished in, so the report reads the
// same on every run.
func (found *discovery) sortProjects() {
	sort.SliceStable(found.Projects, func(left, right int) bool {
		leftProject, rightProject := found.Projects[left], found.Projects[right]
		if leftProject.Directory != rightProject.Directory {
			return pathBefore(leftProject.Directory, rightProject.Directory)
		}
		if leftProject.Engine != rightProject.Engine {
			return leftProject.Engine == engineTypeScript
		}
		return leftProject.ConfigFile < rightProject.ConfigFile
	})
}

// discoveryParallelism is how many directories discovery reads at once.
const discoveryParallelism = 16

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
