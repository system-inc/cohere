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

	// keys are the packages keyed so far, and own each one's own digest, its key but for its dependencies' keys.
	keys map[*packages.Package]Key
	own  map[*packages.Package]Key

	// portable is each package's own digest but for the context (see writePortable).
	portable map[*packages.Package]Key

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

	own, portable, ownError := keyer.ownDigest(keyed, importPaths)
	if ownError != nil {
		return Key{}, ownError
	}
	parts := newPartHash()
	parts.bytes(own[:])
	parts.count(len(importPaths))
	for index, importPath := range importPaths {
		parts.text(importPath)
		parts.bytes(dependencyKeys[index][:])
	}
	key := parts.sum()
	keyer.keys[keyed] = key
	keyer.own[keyed] = own
	keyer.portable[keyed] = portable
	return key, nil
}

// ownDigest is the part of a package's key that is its own, the context then its portable part (see writePortable),
// and that portable part alone, its own digest in any build of the same files.
func (keyer *keyer) ownDigest(keyed *packages.Package, importPaths []string) (Key, Key, error) {
	var extra []string
	if keyer.extraInputs != nil && !pathOnly(keyed) {
		inputs, extraError := keyer.extraInputs(keyed)
		if extraError != nil {
			return Key{}, Key{}, fmt.Errorf("reading the extra inputs of %s: %w", keyed.ID, extraError)
		}
		extra = inputs
	}
	own := newPartHash()
	own.bytes(keyer.context[:])
	keyer.writePortable(own, keyed, importPaths, extra)
	// Written a second time rather than hashed from own's parts: the key must stay the hash it always was, context
	// and portable parts in one stream, so the store's entries keep their names, and the parts are a few hundred
	// bytes, the files' contents already digests.
	portable := newPartHash()
	keyer.writePortable(portable, keyed, importPaths, extra)
	return own.sum(), portable.sum(), nil
}

// writePortable writes the part of a package's own digest no build context enters: its path alone for a package keyed
// so, else its identity, module, files with their contents, import map by ID and extra inputs, everything but the
// context and its dependencies' keys. An extra build (linux, windows, freebsd) whose package has the same portable
// part as the host's read the same files under the same import map (#b2t9xmf).
//
// Mutates: parts is the digest being written, which takes each part in turn
func (keyer *keyer) writePortable(parts *partHash, keyed *packages.Package, importPaths []string, extra []string) {
	if pathOnly(keyed) {
		parts.text("path only")
		parts.text(keyed.PkgPath)
		return
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
	parts.texts(extra)
}
