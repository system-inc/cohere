package project

import (
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// ModulePaths is the module path of each module a root's settings govern, as its go.mod names it: every module its
// go.work uses, or the root's own go.mod without one, in the go.work's order. The settings ask it whether a root holds
// a module of ours (internal/our_tiers), before the project is described, so it reads only those files and runs no
// git. A go.work or go.mod it cannot read or parse names nothing here: describing the project refuses it in its own
// words, a moment later.
func ModulePaths(root string) []string {
	contents, readError := os.ReadFile(filepath.Join(root, "go.work"))
	if readError != nil {
		if path := modulePathAt(root); path != "" {
			return []string{path}
		}
		return nil
	}
	work, parseError := modfile.ParseWork("go.work", contents, nil)
	if parseError != nil {
		return nil
	}
	var paths []string
	for _, use := range work.Use {
		directory := use.Path
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(root, directory)
		}
		if path := modulePathAt(directory); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// modulePathAt is the module path the go.mod in directory names, "" when there is none to read.
func modulePathAt(directory string) string {
	contents, readError := os.ReadFile(filepath.Join(directory, "go.mod"))
	if readError != nil {
		return ""
	}
	return modfile.ModulePath(contents)
}
