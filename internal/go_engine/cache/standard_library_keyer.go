package cache

import (
	"fmt"

	"golang.org/x/tools/go/packages"
)

// standardLibraryKeyer judges which packages are the standard library's, then keys those depth first, each package
// once.
type standardLibraryKeyer struct {
	// context is the sha256 of the run's StandardLibraryContext, hashed into every key.
	context Key

	// loader is the context's Loader, hashed into the own digest of a package with cgo files.
	loader string

	// goroot is the context's Goroot, which every file is keyed below.
	goroot string

	// rootRelative is each file of every package judged so far that is below goroot, by path, as its key names it.
	rootRelative map[string]string

	// fileDigests is every file of every standard-library package, by path.
	fileDigests map[string]fileDigest

	// standard is each package judged so far, and whether it is the standard library's.
	standard map[*packages.Package]bool

	// keys are the packages keyed so far.
	keys map[*packages.Package]Key

	// visiting are the packages whose judgment waits on their imports', so an import cycle is an error rather than
	// a hang.
	visiting map[*packages.Package]bool
}

// isStandard reports whether a package is the standard library's: no module, every file below goroot, and every
// package it imports the standard library's, judged first. An import map holding no package for a path is an error,
// as in Keys, but only of a package that is otherwise the standard library's: another is never looked into.
func (keyer *standardLibraryKeyer) isStandard(judged *packages.Package) (bool, error) {
	if standard, known := keyer.standard[judged]; known {
		return standard, nil
	}
	if keyer.visiting[judged] {
		return false, fmt.Errorf("keying %s: an import cycle runs back through it", judged.ID)
	}
	standard := judged.Module == nil && keyer.filesBelowRoot(judged)
	if standard {
		keyer.visiting[judged] = true
		defer delete(keyer.visiting, judged)
		for _, importPath := range sortedImportPaths(judged) {
			imported := judged.Imports[importPath]
			if imported == nil {
				return false, fmt.Errorf("keying %s: its import %s holds no package", judged.ID, importPath)
			}
			importStandard, judgeError := keyer.isStandard(imported)
			if judgeError != nil {
				return false, judgeError
			}
			if !importStandard {
				standard = false
				break
			}
		}
	}
	keyer.standard[judged] = standard
	return standard, nil
}

// filesBelowRoot reports whether every file of every list a package holds is below goroot, noting each one's path
// as its key names it.
func (keyer *standardLibraryKeyer) filesBelowRoot(judged *packages.Package) bool {
	for _, files := range fileLists(judged) {
		for _, file := range files {
			relative, below := belowRoot(keyer.goroot, file)
			if !below {
				return false
			}
			keyer.rootRelative[file] = relative
		}
	}
	return true
}

// key is one standard-library package's key, computed after every package it imports. It cannot fail: the judgment
// before it found every import a package and every package imported the standard library's, with no cycle among
// them.
func (keyer *standardLibraryKeyer) key(keyed *packages.Package) Key {
	if key, known := keyer.keys[keyed]; known {
		return key
	}
	importPaths := sortedImportPaths(keyed)
	parts := newPartHash()
	own := keyer.ownDigest(keyed)
	parts.bytes(own[:])
	parts.count(len(importPaths))
	for _, importPath := range importPaths {
		dependencyKey := keyer.key(keyed.Imports[importPath])
		parts.text(importPath)
		parts.bytes(dependencyKey[:])
	}
	key := parts.sum()
	keyer.keys[keyed] = key
	return key
}

// ownDigest is the part of a standard-library package's key that is its own: the context, then its path alone for a
// package keyed so, else its identity, its files below goroot, and the loader when it has cgo files. Its import map
// is not in it, unlike Keys' own digest, which the findings cache's second key reads apart from the key: no caller
// reads this one apart, and the key's second part holds each import path with its package's key.
func (keyer *standardLibraryKeyer) ownDigest(keyed *packages.Package) Key {
	parts := newPartHash()
	parts.bytes(keyer.context[:])
	if pathOnly(keyed) {
		parts.text("path only")
		parts.text(keyed.PkgPath)
		return parts.sum()
	}
	parts.text("package")
	parts.texts([]string{keyed.ID, keyed.PkgPath, keyed.Name})
	for _, files := range fileLists(keyed) {
		parts.count(len(files))
		for _, file := range files {
			digest := keyer.fileDigests[file]
			parts.text(keyer.rootRelative[file])
			parts.bytes(digest[:])
		}
	}
	if !hasCgoFiles(keyed) {
		parts.count(0)
		return parts.sum()
	}
	parts.count(1)
	parts.text(keyer.loader)
	return parts.sum()
}
