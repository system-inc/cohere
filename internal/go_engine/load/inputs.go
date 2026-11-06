package load

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"golang.org/x/tools/go/packages"
)

// Inputs are the paths a load read to decide what it returned, which the engine writes as its inputs record so the
// front door can replay a verdict without starting the engine when none of them has moved. The front door cannot see
// these reads: `go list` runs in a subprocess, and the load parses in this one.
//
// The standing rule: whatever the load reads to decide a verdict, and whatever a later cache of the engine's reads,
// is named here. A read left out is a replay that goes stale without a word, and a path named that did not matter
// only costs a miss, so when in doubt a path is named. TestInputsHoldEveryFileTheLoadListed holds Files to the
// load's own package lists, so a loader that reads more fails it rather than going stale.
//
// Each list is absolute, cleaned, sorted and without duplicates.
type Inputs struct {
	// Files are the files whose bytes the load's result can depend on.
	Files []string

	// Directories are the directories whose listing decides which packages or files exist: every directory under a
	// module root, or a local replacement's, that `./...` could reach, and each package directory and embedded
	// directory among them. A directory outside them, of the standard library or the module cache, is the
	// toolchain's or a pinned version's, which `VERSION`, go.mod and go.sum stand for. None is above a module root, nor
	// in the build cache, nor a directory whose name starts with `.` or below one, such as the root's `.cache`: the
	// run, or the front door around it, writes there, and a listing that moves on every run would never replay.
	Directories []string

	// Absent are paths whose absence the load depends on: a go.work above a module root while none is in use, and a
	// settings file of go's that is not there.
	Absent []string
}

// reportedEnvironment is what go env reported in the workspace beyond the build target: the root of the toolchain that
// ran, which with `GOTOOLCHAIN` switching is one in the module cache rather than PATH's, the go env file, the go.mod
// and go.work go found (`/dev/null`, empty or `off` when none), the build cache, and the flags of `GOFLAGS`.
type reportedEnvironment struct {
	goRoot          string
	environmentFile string
	moduleFile      string
	workspaceFile   string
	buildCache      string
	goFlags         []string
}

// collectInputs is what a load read: every file of every package in the graph, the standard library's and the module
// cache's included; the census, which the load reads for build constraints; every module's go.mod and each module
// root's go.sum and vendor/modules.txt; the go.work in use and its go.work.sum, or, with none in use, a go.work above
// each module root as absent; PATH's go executable, and the go, `VERSION` and go.env of the toolchain that ran; the go
// env file; a go.mod or overlay `GOFLAGS` names; and the directories Inputs.Directories says. A path that could not be
// read about is still named, so the front door, which cannot read it either, counts it as moved.
//
// A file in the build cache is never named: go writes it during the run (a translated cgo file), so its time is
// always past the front door's clock, and its bytes follow from the source files and settings named already.
func collectInputs(
	workspace string,
	reported reportedEnvironment,
	roots []*packages.Package,
	census []string,
) Inputs {
	set := &inputSet{files: map[string]bool{}, directories: map[string]bool{}, absent: map[string]bool{}}
	moduleRoots := map[string]bool{}
	localReplacements := map[string]bool{}
	packageDirectories := map[string]bool{}
	packages.Visit(roots, nil, func(visited *packages.Package) {
		for _, file := range slices.Concat(visited.GoFiles, visited.CompiledGoFiles, visited.OtherFiles,
			visited.EmbedFiles, visited.IgnoredFiles) {
			set.files[filepath.Clean(file)] = true
		}
		if visited.Dir != "" {
			packageDirectories[filepath.Clean(visited.Dir)] = true
			// An embed pattern can name a directory, whose files go list globs, so each directory between an
			// embedded file and its package is listed too: a file added beside an embedded one changes the match.
			for _, file := range visited.EmbedFiles {
				directory := filepath.Dir(file)
				for inside(visited.Dir, directory) {
					packageDirectories[filepath.Clean(directory)] = true
					directory = filepath.Dir(directory)
				}
			}
		}
		set.addModule(visited.Module, moduleRoots, localReplacements)
	})
	for _, file := range census {
		set.files[filepath.Clean(file)] = true
	}

	// exec.Command looks go up on this process's PATH, as every go command of the load did.
	if goExecutable, lookError := exec.LookPath("go"); lookError == nil {
		if absoluteExecutable, absoluteError := filepath.Abs(goExecutable); absoluteError == nil {
			set.files[filepath.Clean(absoluteExecutable)] = true
		}
	}
	if reported.goRoot != "" {
		set.addFileOrAbsent(filepath.Join(reported.goRoot, "bin", "go"))
		set.addFileOrAbsent(filepath.Join(reported.goRoot, "VERSION"))
		set.addFileOrAbsent(filepath.Join(reported.goRoot, "go.env"))
	}
	if reported.environmentFile != "" && reported.environmentFile != "off" {
		set.addFileOrAbsent(reported.environmentFile)
	}
	if reported.moduleFile != "" && reported.moduleFile != os.DevNull {
		moduleRoots[filepath.Dir(filepath.Clean(reported.moduleFile))] = true
	}
	set.addWorkspaceFile(workspace, reported.workspaceFile, moduleRoots)
	set.addGoFlagFiles(workspace, reported.goFlags)

	for _, root := range slices.Sorted(maps.Keys(moduleRoots)) {
		set.files[filepath.Join(root, "go.mod")] = true
		set.addFileOrAbsent(filepath.Join(root, "go.sum"))
		set.addFileOrAbsent(filepath.Join(root, "vendor", "modules.txt"))
		set.addReachableDirectories(root)
	}
	walked := slices.Concat(slices.Sorted(maps.Keys(moduleRoots)), slices.Sorted(maps.Keys(localReplacements)))
	for _, root := range slices.Sorted(maps.Keys(localReplacements)) {
		set.addReachableDirectories(root)
	}
	for directory := range packageDirectories {
		set.directories[directory] = true
	}
	for directory := range set.directories {
		if !listable(directory, moduleRoots, walked, reported.buildCache) {
			delete(set.directories, directory)
		}
	}
	if reported.buildCache != "" {
		for file := range set.files {
			if inside(filepath.Clean(reported.buildCache), file) {
				delete(set.files, file)
			}
		}
	}
	return Inputs{
		Files:       slices.Sorted(maps.Keys(set.files)),
		Directories: slices.Sorted(maps.Keys(set.directories)),
		Absent:      slices.Sorted(maps.Keys(set.absent)),
	}
}

// listable reports whether a directory's listing may be an input: a walked root or inside one, above no module root,
// which a replacement by a parent directory would be, and outside the build cache, which `GOCACHE` can put inside a
// module and go writes during the run. The walk never enters a directory whose name starts with `.`, so the root's
// `.cache`, where the front door and the engine write, is never one.
func listable(directory string, moduleRoots map[string]bool, walked []string, buildCache string) bool {
	for root := range moduleRoots {
		if inside(directory, root) {
			return false
		}
	}
	if buildCache != "" && (directory == filepath.Clean(buildCache) || inside(filepath.Clean(buildCache), directory)) {
		return false
	}
	return slices.ContainsFunc(walked, func(root string) bool { return directory == root || inside(root, directory) })
}
