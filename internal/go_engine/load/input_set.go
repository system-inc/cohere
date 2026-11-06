package load

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// inputSet gathers the paths of Inputs, each once.
type inputSet struct {
	files       map[string]bool
	directories map[string]bool
	absent      map[string]bool
}

// addModule names a package's module's go.mod, and its replacement's, and keeps a main module's directory as a module
// root and a replacement by a local directory, which has no version to pin it, as one to walk too. The standard
// library's packages have no module.
//
// Mutates: moduleRoots gains a main module's directory
// Mutates: localReplacements gains a local replacement's directory
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
// Mutates: moduleRoots gains the modules the go.work uses
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
	inUse := filepath.Clean(workspaceFile)
	set.files[inUse] = true
	set.addFileOrAbsent(inUse + ".sum")
	contents, readError := os.ReadFile(inUse)
	if readError != nil {
		// Named already: a go.work nobody can read is one the front door counts as moved.
		return
	}
	work, parseError := modfile.ParseWork(inUse, contents, nil)
	if parseError != nil {
		// Named already, and go refused the same file, so the load reported what it made of it.
		return
	}
	for _, use := range work.Use {
		directory := filepath.FromSlash(use.Path)
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(filepath.Dir(inUse), directory)
		}
		moduleRoots[filepath.Clean(directory)] = true
	}
}

// addGoFlagFiles names the files a flag of `GOFLAGS` has go read in place of the usual ones: `-modfile`'s go.mod and
// its go.sum, and `-overlay`'s file. A relative path is go's, from the workspace.
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
func (set *inputSet) addReachableDirectories(root string) {
	// Ignored: the walk's callback returns no error, so neither does the walk.
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || !entry.IsDir() {
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

// addFileOrAbsent names path as a file when it exists and as absent when it does not.
func (set *inputSet) addFileOrAbsent(path string) {
	cleaned := filepath.Clean(path)
	if exists(cleaned) {
		set.files[cleaned] = true
		return
	}
	set.absent[cleaned] = true
}
