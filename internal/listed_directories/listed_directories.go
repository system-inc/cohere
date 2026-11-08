// Package listed_directories reads, in this process, the directories a go list run in another process read, so a test
// that loads packages through go list has its result keyed on them (#cmpsz8p).
//
// Go's test cache keys a test's result on the linked test binary and on what the test's own process opens: a file by
// its size and modification time, a directory by every entry's name, size and modification time. Both loaders, the
// engine's load and the tests' go_types, list packages with go list in a subprocess, so nothing of a listing was
// keyed. A test whose fixtures go list found replayed its pass when a fixture file was added: a rule's fixtures read
// 70 diagnostics against 70 where the truth was 75 against 75, and a planted file no want expected read
// `ok (cached)`.
//
// Open opens each listed package's directory in the workspace or a main module, each main module's go.mod, and for a
// pattern ending in `/...` every directory go's walk of it looks in, so a new file is a changed entry, a new package a
// new one, somewhere this process read. What lies in the module cache or the toolchain is versioned by go.sum and the
// toolchain, which the test binary already carries. The reads cost a few directory opens beside a go list subprocess.
package listed_directories

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Package is what Open needs of one listed package.
type Package struct {
	ImportPath string
	Directory  string
	// Root is whether a pattern matched the package, rather than its being listed as a dependency.
	Root bool
	// MainModule is whether the package is in a main module, and MainGoMod that module's go.mod.
	MainModule bool
	MainGoMod  string
}

// Open opens, in this process, what a go list of patterns from workspace read in its own (see the package).
func Open(workspace string, patterns []string, packages []Package) {
	opened := map[string]bool{}
	open := func(path string) {
		if path == "" || opened[path] {
			return
		}
		opened[path] = true
		// Ignored: a path that cannot be opened, or closed, was read by go list or reported by it, and this read exists
		// only to be logged.
		if file, openError := os.Open(path); openError == nil {
			if closeError := file.Close(); closeError != nil {
				return
			}
		}
	}
	for _, listed := range packages {
		if listed.Directory == "" || (!listed.MainModule && !InDirectory(listed.Directory, workspace)) {
			continue
		}
		open(listed.Directory)
		if listed.MainModule {
			open(listed.MainGoMod)
		}
	}
	for _, pattern := range patterns {
		prefix, wildcard := strings.CutSuffix(pattern, "/...")
		if !wildcard || strings.Contains(prefix, "...") {
			continue
		}
		for _, base := range wildcardBases(workspace, prefix, packages) {
			// Ignored: a walk that fails reads less, and go list already answered for the directories it read.
			directories, _ := PackageDirectories(base, func(string) bool { return true })
			for _, directory := range directories {
				open(directory)
			}
		}
	}
}

// wildcardBases is the directory a `prefix/...` pattern walks: prefix itself for a directory pattern, and otherwise
// the directory of the package named prefix, read off each root that matched, since in module and in `GOPATH` mode
// alike a root's directory ends in its import path below prefix.
func wildcardBases(workspace string, prefix string, packages []Package) []string {
	if filepath.IsAbs(prefix) {
		return []string{prefix}
	}
	if prefix == "." || strings.HasPrefix(prefix, "./") || strings.HasPrefix(prefix, "../") {
		return []string{filepath.Join(workspace, prefix)}
	}
	seen := map[string]bool{}
	var bases []string
	for _, listed := range packages {
		if !listed.Root || listed.Directory == "" {
			continue
		}
		below, matched := strings.CutPrefix(listed.ImportPath, prefix)
		if !matched || (below != "" && !strings.HasPrefix(below, "/")) {
			continue
		}
		base, isBelow := strings.CutSuffix(filepath.ToSlash(listed.Directory), below)
		if !isBelow || seen[base] {
			continue
		}
		seen[base] = true
		bases = append(bases, filepath.FromSlash(base))
	}
	return bases
}

// PackageDirectories is every directory below root, root among them, that go's walk of a pattern looks in, in its
// order: it leaves out a directory whose name starts with a dot or an underscore, testdata, one that holds a module
// of its own, and one wanted declines, and never follows a symbolic link. A directory named vendor is looked in, as a
// wildcard matches one, but nothing below it is, as no wildcard matches a vendored package (cmd/go's pkgpattern). It
// returns the walk's failure as a sentence, empty when it read every directory.
func PackageDirectories(root string, wanted func(string) bool) ([]string, string) {
	var directories []string
	walkError := filepath.WalkDir(root, func(directory string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if !entry.IsDir() {
			return nil
		}
		if directory != root {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" ||
				!wanted(directory) {
				return filepath.SkipDir
			}
			if GoModAt(directory) {
				return filepath.SkipDir
			}
			if name == "vendor" {
				directories = append(directories, directory)
				return filepath.SkipDir
			}
		}
		directories = append(directories, directory)
		return nil
	})
	if walkError != nil {
		return nil, "walking " + root + ": " + walkError.Error()
	}
	return directories, ""
}

// GoModAt reports whether directory holds a go.mod, which makes it a module of its own that go's walk stops at.
func GoModAt(directory string) bool {
	info, statError := os.Stat(filepath.Join(directory, "go.mod"))
	return statError == nil && !info.IsDir()
}

// InDirectory reports whether path is directory or below it.
func InDirectory(path string, directory string) bool {
	relative, relativeError := filepath.Rel(directory, path)
	return relativeError == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
