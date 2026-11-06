package load

import (
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
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
	// toolchain's or a pinned version's, which VERSION, go.mod and go.sum stand for. None is above a module root, nor
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

// inputSet gathers the paths of Inputs, each once.
type inputSet struct {
	files       map[string]bool
	directories map[string]bool
	absent      map[string]bool
}

// collectInputs is what a load read: every file of every package in the graph, the standard library's and the module
// cache's included; the census, which the load reads for build constraints; every module's go.mod and each module
// root's go.sum and vendor/modules.txt; the go.work in use and its go.work.sum, or, with none in use, a go.work above
// each module root as absent; PATH's go executable, and the go, VERSION and go.env of the toolchain that ran; the go
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

// addModule names a package's module's go.mod, and its replacement's, and keeps a main module's directory as a module
// root and a replacement by a local directory, which has no version to pin it, as one to walk too. The standard
// library's packages have no module.
//
// Mutates: set gains the go.mod files; moduleRoots gains a main module's directory; localReplacements gains a local
// replacement's directory
func (set *inputSet) addModule(
	module *packages.Module,
	moduleRoots map[string]bool,
	localReplacements map[string]bool,
) {
	if module == nil {
		return
	}
	if module.GoMod != "" {
		set.files[filepath.Clean(module.GoMod)] = true
	}
	if module.Replace != nil && module.Replace.GoMod != "" {
		set.files[filepath.Clean(module.Replace.GoMod)] = true
	}
	if module.Replace != nil && module.Replace.Version == "" && module.Replace.Dir != "" {
		localReplacements[filepath.Clean(module.Replace.Dir)] = true
	}
	if module.Main && module.Dir != "" {
		moduleRoots[filepath.Clean(module.Dir)] = true
	}
}

// addWorkspaceFile names the go.work in use, its go.work.sum and the modules it uses as module roots. With none in
// use and `GOWORK` not `off`, go looked for one from the workspace upward, and from each module root it was told of,
// so a go.work in any directory above them is absent, since one appearing would change the load.
//
// Mutates: set gains the go.work and its sum, or their absences; moduleRoots gains the modules the go.work uses
func (set *inputSet) addWorkspaceFile(workspace string, workspaceFile string, moduleRoots map[string]bool) {
	switch workspaceFile {
	case "off":
		return
	case "":
		starts := append([]string{workspace}, slices.Sorted(maps.Keys(moduleRoots))...)
		for _, start := range starts {
			for directory := filepath.Clean(start); ; directory = filepath.Dir(directory) {
				set.absent[filepath.Join(directory, "go.work")] = true
				if filepath.Dir(directory) == directory {
					break
				}
			}
		}
		return
	}
	workspaceFile = filepath.Clean(workspaceFile)
	set.files[workspaceFile] = true
	set.addFileOrAbsent(workspaceFile + ".sum")
	contents, readError := os.ReadFile(workspaceFile)
	if readError != nil {
		// Named already: a go.work nobody can read is one the front door counts as moved.
		return
	}
	work, parseError := modfile.ParseWork(workspaceFile, contents, nil)
	if parseError != nil {
		// Named already, and go refused the same file, so the load reported what it made of it.
		return
	}
	for _, use := range work.Use {
		directory := filepath.FromSlash(use.Path)
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(filepath.Dir(workspaceFile), directory)
		}
		moduleRoots[filepath.Clean(directory)] = true
	}
}

// addGoFlagFiles names the files a flag of `GOFLAGS` has go read in place of the usual ones: `-modfile`'s go.mod and
// its go.sum, and `-overlay`'s file. A relative path is go's, from the workspace.
//
// Mutates: set gains each file the flags name
func (set *inputSet) addGoFlagFiles(workspace string, goFlags []string) {
	for _, flag := range goFlags {
		name, value, hasValue := strings.Cut(strings.TrimLeft(flag, "-"), "=")
		if !hasValue || value == "" || (name != "modfile" && name != "overlay") {
			continue
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(workspace, value)
		}
		set.addFileOrAbsent(value)
		if name == "modfile" {
			set.addFileOrAbsent(strings.TrimSuffix(value, ".mod") + ".sum")
		}
	}
}

// addReachableDirectories names every directory under a module root that a `./...` pattern could reach, walked as go
// walks one: never into a directory whose name starts with `.` or `_`, nor testdata, nor below a directory with a
// go.mod of its own, which is named, since a go.mod leaving it would make its packages the module's. More is named
// than go matches (a vendor tree, a directory a go.mod `ignore` directive names), which only costs a miss.
//
// Mutates: set gains each directory reached
func (set *inputSet) addReachableDirectories(root string) {
	// Ignored: the walk's callback returns no error, so neither does the walk.
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || !entry.IsDir() {
			// A directory that cannot be read was named when the walk reached it, and the front door, which cannot
			// list it either, counts it as moved.
			return nil
		}
		if path != root {
			name := entry.Name()
			if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
		}
		set.directories[filepath.Clean(path)] = true
		if path != root && exists(filepath.Join(path, "go.mod")) {
			return filepath.SkipDir
		}
		return nil
	})
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

// addFileOrAbsent names path as a file when it exists and as absent when it does not.
//
// Mutates: set gains path in one list or the other
func (set *inputSet) addFileOrAbsent(path string) {
	path = filepath.Clean(path)
	if exists(path) {
		set.files[path] = true
		return
	}
	set.absent[path] = true
}
