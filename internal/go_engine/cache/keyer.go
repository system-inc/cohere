package cache

import (
	"crypto/sha256"
	"fmt"

	"golang.org/x/tools/go/packages"
)

// fileDigest is the sha256 of a file's bytes.
type fileDigest [sha256.Size]byte

// keyer computes keys depth first, each package once.
type keyer struct {
	// context is the sha256 of the run's Context, hashed into every key.
	context Key

	// extraInputs is Keys' hook, or nil.
	extraInputs func(*packages.Package) ([]string, error)

	// fileDigests is every file of every package keyed, by path.
	fileDigests map[string]fileDigest

	// keys are the packages keyed so far.
	keys map[*packages.Package]Key

	// visiting are the packages whose keys wait on their dependencies', so an import cycle is an error rather
	// than a hang.
	visiting map[*packages.Package]bool
}

// key is one package's key, computed after every package it imports.
func (keyer *keyer) key(keyed *packages.Package) (Key, error) {
	if key, known := keyer.keys[keyed]; known {
		return key, nil
	}
	if keyer.visiting[keyed] {
		return Key{}, fmt.Errorf("keying %s: an import cycle runs back through it", keyed.ID)
	}
	keyer.visiting[keyed] = true
	defer delete(keyer.visiting, keyed)

	importPaths := sortedImportPaths(keyed)
	dependencyKeys := make([]Key, len(importPaths))
	for index, importPath := range importPaths {
		dependencyKey, keyError := keyer.key(keyed.Imports[importPath])
		if keyError != nil {
			return Key{}, keyError
		}
		dependencyKeys[index] = dependencyKey
	}

	parts := newPartHash()
	parts.bytes(keyer.context[:])
	if pathOnly(keyed) {
		parts.text("path only")
		parts.text(keyed.PkgPath)
		key := parts.sum()
		keyer.keys[keyed] = key
		return key, nil
	}
	parts.text("package")
	parts.texts([]string{keyed.ID, keyed.PkgPath, keyed.Name})
	hashModule(parts, keyed.Module)
	for _, files := range fileLists(keyed) {
		parts.count(len(files))
		for _, file := range files {
			digest := keyer.fileDigests[file]
			parts.text(file)
			parts.bytes(digest[:])
		}
	}
	parts.count(len(importPaths))
	for _, importPath := range importPaths {
		parts.text(importPath)
		parts.text(keyed.Imports[importPath].ID)
	}
	parts.count(len(importPaths))
	for index, importPath := range importPaths {
		parts.text(importPath)
		parts.bytes(dependencyKeys[index][:])
	}
	var extra []string
	if keyer.extraInputs != nil {
		inputs, extraError := keyer.extraInputs(keyed)
		if extraError != nil {
			return Key{}, fmt.Errorf("reading the extra inputs of %s: %w", keyed.ID, extraError)
		}
		extra = inputs
	}
	parts.texts(extra)

	key := parts.sum()
	keyer.keys[keyed] = key
	return key, nil
}
