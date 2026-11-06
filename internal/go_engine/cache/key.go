// Package cache is the Go engine's per-package findings cache, its keys and its store.
//
// This file and store.go stand in for hand A's package, built in parallel to the same interface, so the checker's
// side can be built and tested before it lands. Both are replaced by hand A's files when the two branches merge.
package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"
)

// Key is one package's cache key.
type Key [32]byte

// String is the key in lowercase hex.
func (key Key) String() string {
	return hex.EncodeToString(key[:])
}

// Context is what every package's key shares in one run.
type Context struct {
	FormatVersion  string
	EngineIdentity string
	Build          []string
	RuleSet        []string
}

// Keys computes every package's key, for the packages given and everything they import. extraInputs, when non-nil,
// returns more inputs for one package, each hashed in.
func Keys(
	given []*packages.Package,
	context Context,
	extraInputs func(*packages.Package) ([]string, error),
) (map[*packages.Package]Key, error) {
	keys := map[*packages.Package]Key{}
	var keyOf func(keyed *packages.Package) (Key, error)
	keyOf = func(keyed *packages.Package) (Key, error) {
		if known, done := keys[keyed]; done {
			return known, nil
		}
		digest := sha256.New()
		writePart(digest, context.FormatVersion)
		writePart(digest, context.EngineIdentity)
		writeParts(digest, context.RuleSet)
		writeParts(digest, context.Build)
		writePart(digest, keyed.ID)
		writePart(digest, keyed.PkgPath)
		writePart(digest, keyed.Name)
		if keyed.Module != nil {
			writeParts(digest, []string{keyed.Module.Path, keyed.Module.Version, keyed.Module.GoVersion})
		}
		for _, files := range [][]string{keyed.GoFiles, keyed.CompiledGoFiles, keyed.OtherFiles, keyed.EmbedFiles,
			keyed.IgnoredFiles} {
			writePart(digest, fmt.Sprint(len(files)))
			for _, file := range files {
				contents, readError := os.ReadFile(file)
				if readError != nil {
					return Key{}, fmt.Errorf("keying %s: %w", keyed.ID, readError)
				}
				fileDigest := sha256.Sum256(contents)
				writeParts(digest, []string{file, string(fileDigest[:])})
			}
		}
		paths := make([]string, 0, len(keyed.Imports))
		for path := range keyed.Imports {
			paths = append(paths, path)
		}
		slices.Sort(paths)
		for _, path := range paths {
			imported := keyed.Imports[path]
			importedKey, importError := keyOf(imported)
			if importError != nil {
				return Key{}, importError
			}
			writeParts(digest, []string{path, imported.ID, string(importedKey[:])})
		}
		if extraInputs != nil {
			extra, extraError := extraInputs(keyed)
			if extraError != nil {
				return Key{}, fmt.Errorf("keying %s: %w", keyed.ID, extraError)
			}
			writeParts(digest, extra)
		}
		var key Key
		copy(key[:], digest.Sum(nil))
		keys[keyed] = key
		return key, nil
	}
	for _, each := range given {
		if _, keyError := keyOf(each); keyError != nil {
			return nil, keyError
		}
	}
	return keys, nil
}

// writePart hashes one part, prefixed by its length, so no two lists of parts hash alike.
func writePart(digest hash.Hash, part string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(part)))
	// Ignored: a hash.Hash never returns an error from Write.
	_, _ = digest.Write(length[:])
	// Ignored: as above.
	_, _ = io.WriteString(digest, part)
}

// writeParts hashes a list of parts, prefixed by how many there are.
func writeParts(digest hash.Hash, parts []string) {
	writePart(digest, fmt.Sprint(len(parts)))
	for _, part := range parts {
		writePart(digest, part)
	}
}

// executable is the running executable's sha256, read once for the process.
var executable struct {
	once     sync.Once
	identity string
	failure  error
}

// ExecutableIdentity is the sha256 of the running executable, memoized for the process.
func ExecutableIdentity() (string, error) {
	executable.once.Do(func() {
		path, pathError := os.Executable()
		if pathError != nil {
			executable.failure = fmt.Errorf("finding the running executable: %w", pathError)
			return
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			executable.failure = fmt.Errorf("reading the running executable: %w", readError)
			return
		}
		digest := sha256.Sum256(contents)
		executable.identity = hex.EncodeToString(digest[:])
	})
	return executable.identity, executable.failure
}
