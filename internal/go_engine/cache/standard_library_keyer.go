package cache

import (
	"fmt"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// standardLibraryKeyer judges which packages the system cache keeps, the standard library's and third-party modules',
// then keys those depth first, each package once.
type standardLibraryKeyer struct {
	// context is the sha256 of the run's StandardLibraryContext, hashed into every key.
	context Key

	// loader is the context's Loader, hashed into the own digest of a package with cgo files.
	loader string

	// cgoToolchain is the context's CgoToolchain, hashed beside loader when it is the cgo-translated one.
	cgoToolchain []string

	// goroot is the context's Goroot, which every standard-library file is keyed below.
	goroot string

	// moduleCache is the context's ModuleCache, which every file of a module's package is keyed below, empty when the
	// run keeps no module's package.
	moduleCache string

	// rootRelative is each file of every package judged so far that is below goroot or the module cache, by path, as
	// its key names it.
	rootRelative map[string]string

	// fileDigests is every file of every package the system cache keeps, by path.
	fileDigests map[string]fileDigest

	// system is each package judged so far, and whether the system cache keeps it.
	system map[*packages.Package]bool

	// keys are the packages keyed so far.
	keys map[*packages.Package]Key

	// visiting are the packages whose judgment waits on their imports', so an import cycle is an error rather than
	// a hang.
	visiting map[*packages.Package]bool
}

// isSystem reports whether the system cache keeps a package (filesKeptApart), and every package it imports, judged
// first, the same way, the standard library's or a module's. An import map holding no package for a path is an error,
// as in Keys, but only of a package whose own files the system cache would keep: another is never looked into.
func (keyer *standardLibraryKeyer) isSystem(judged *packages.Package) (bool, error) {
	if system, known := keyer.system[judged]; known {
		return system, nil
	}
	if keyer.visiting[judged] {
		return false, fmt.Errorf("keying %s: an import cycle runs back through it", judged.ID)
	}
	system := keyer.filesKeptApart(judged)
	if system {
		keyer.visiting[judged] = true
		defer delete(keyer.visiting, judged)
		for _, importPath := range sortedImportPaths(judged) {
			imported := judged.Imports[importPath]
			if imported == nil {
				return false, fmt.Errorf("keying %s: its import %s holds no package", judged.ID, importPath)
			}
			importSystem, judgeError := keyer.isSystem(imported)
			if judgeError != nil {
				return false, judgeError
			}
			if !importSystem {
				system = false
				break
			}
		}
	}
	keyer.system[judged] = system
	return system, nil
}

// filesKeptApart reports whether a package's own files are the system cache's, its imports aside: a package in no
// module, the standard library's, with every file below goroot, or a package of a module that is not a main module
// with every file below the module cache. A main module's package is the project's wherever its files are: under
// go.work every workspace module is a main module, and a project can sit below the module cache.
func (keyer *standardLibraryKeyer) filesKeptApart(judged *packages.Package) bool {
	switch {
	case judged.Module == nil:
		return keyer.filesBelow(keyer.goroot, rootMarker, judged)
	case judged.Module.Main || keyer.moduleCache == "":
		return false
	default:
		return keyer.filesBelow(keyer.moduleCache, moduleCacheMarker, judged)
	}
}

// filesBelow reports whether every file of every list a package holds is below directory, noting each one's path as
// its key names it, marker then its path below directory.
func (keyer *standardLibraryKeyer) filesBelow(directory string, marker string, judged *packages.Package) bool {
	for _, files := range fileLists(judged) {
		for _, file := range files {
			relative, below := belowDirectory(directory, marker, file)
			if !below {
				return false
			}
			keyer.rootRelative[file] = relative
		}
	}
	return true
}

// key is the key of one package the system cache keeps, computed after every package it imports. It cannot fail: the
// judgment before it found every import a package and every package imported one the system cache keeps, with no cycle
// among them.
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

// ownDigest is the part of the key of a package the system cache keeps that is its own: the context, then for a
// module's package the module cache and its module, then its path alone for a package keyed so, else its identity, its
// files below goroot or the module cache, and the loader when it has cgo files, with the C toolchain when the loader is
// cgo-translated. Its import map is not in it, unlike Keys' own digest, which the findings cache's second key reads
// apart from the key: no caller reads this one apart, and the key's second part holds each import path with its
// package's key.
func (keyer *standardLibraryKeyer) ownDigest(keyed *packages.Package) Key {
	parts := newPartHash()
	parts.bytes(keyer.context[:])
	// A standard-library package has no module and hashes nothing here, so its key is the same whatever the module
	// cache is. A module's package holds where the module cache is, as every key holds goroot, since its entry holds
	// this machine's paths, and its module as Keys hashes it: the module's go version is the language version its files
	// are checked under, which no file below the package says.
	if keyed.Module != nil {
		parts.text("module")
		parts.text(keyer.moduleCache)
		hashModule(parts, keyed.Module)
	}
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
	// Under the cgo-translated loader a package with cgo files has the cgo tool's outputs among its CompiledGoFiles,
	// which go keeps in its build cache, outside goroot and the module cache, so isSystem never judges it the system
	// cache's, a module's no more than the standard library's, nor what imports it: it gets a project key, as intended,
	// and the toolchain here keys whatever such a package a future loader keeps below either.
	if keyer.loader == load.LoaderCgoTranslated {
		parts.texts(keyer.cgoToolchain)
	}
	return parts.sum()
}
