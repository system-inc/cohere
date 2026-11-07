package cache

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"
)

// Context is what every package's key shares in one run.
type Context struct {
	// FormatVersion is the cache's own format version, so an entry written in another format is never read.
	FormatVersion string

	// EngineIdentity is ExecutableIdentity: the running executable covers every rule's code and embedded policy.
	EngineIdentity string

	// Build is the build context, "GOOS=darwin" and the rest, in a fixed order, the loader included as
	// "loader=source".
	Build []string

	// RuleSet is each enabled rule as "name" or "name options-json", sorted.
	RuleSet []string
}

// Keyed is every package's key, and the part of each that is the package's own.
type Keyed struct {
	Keys map[*packages.Package]Key

	// Own is each package's own digest: its key but for its dependencies' keys, which the findings cache's second
	// key joins with what its dependencies' analyses came out as rather than with what went into them.
	Own map[*packages.Package]Key
}

// Keys computes every package's key, for the packages given and everything they import, reading each file once.
// extraInputs, when non-nil, returns more inputs for one package (a rule's reads outside the pass), each hashed in.
//
// A key is a sha256 over two parts, each length-prefixed: the package's own digest, then the keys of the packages it
// imports, sorted by import path. The own digest is a sha256 over, in this order and each part length-prefixed: the
// context (its format version, engine identity, rule set and build, hashed once for every package); the package's
// ID, PkgPath and Name; its Module's path, version and go version, and its replacement's; each of its GoFiles,
// CompiledGoFiles, OtherFiles, EmbedFiles and IgnoredFiles as its path and the sha256 of its bytes; its import map,
// path to ID, sorted by path; and its extra inputs. Since a package's key holds its dependencies' keys, a change
// anywhere below it changes it, and a change in it leaves them alone.
func Keys(
	given []*packages.Package,
	shared Context,
	extraInputs func(*packages.Package) ([]string, error),
) (*Keyed, error) {
	reachable, reachableError := reachableFrom(given)
	if reachableError != nil {
		return nil, reachableError
	}
	digests, digestsError := fileDigests(reachable)
	if digestsError != nil {
		return nil, digestsError
	}
	computing := &keyer{
		context:     contextDigest(shared),
		extraInputs: extraInputs,
		fileDigests: digests,
		keys:        make(map[*packages.Package]Key, len(reachable)),
		own:         make(map[*packages.Package]Key, len(reachable)),
		visiting:    map[*packages.Package]bool{},
	}
	for _, root := range given {
		if _, keyError := computing.key(root); keyError != nil {
			return nil, keyError
		}
	}
	return &Keyed{Keys: computing.keys, Own: computing.own}, nil
}

// contextDigest is the run's Context hashed once, in the key's order: format version, engine identity, rule set,
// build.
func contextDigest(shared Context) Key {
	parts := newPartHash()
	parts.text(shared.FormatVersion)
	parts.text(shared.EngineIdentity)
	parts.texts(shared.RuleSet)
	parts.texts(shared.Build)
	return parts.sum()
}

// hashModule writes a package's module, or that it has none: its path, version and go version, then its
// replacement's the same way. A replacement in another version has other files, but its version says so first.
func hashModule(parts *partHash, module *packages.Module) {
	if module == nil {
		parts.count(0)
		return
	}
	parts.count(1)
	parts.texts([]string{module.Path, module.Version, module.GoVersion})
	hashModule(parts, module.Replace)
}

// pathOnly reports whether a package is keyed by its path alone: unsafe, whose types go/types makes rather than
// reads from its file, and a package with no files, which has nothing else to key on.
func pathOnly(keyed *packages.Package) bool {
	if keyed.PkgPath == "unsafe" {
		return true
	}
	for _, files := range fileLists(keyed) {
		if len(files) > 0 {
			return false
		}
	}
	return true
}

// fileLists are a package's files, list by list, in the key's order.
func fileLists(keyed *packages.Package) [][]string {
	return [][]string{keyed.GoFiles, keyed.CompiledGoFiles, keyed.OtherFiles, keyed.EmbedFiles, keyed.IgnoredFiles}
}

// sortedImportPaths are a package's import paths, sorted.
func sortedImportPaths(keyed *packages.Package) []string {
	importPaths := make([]string, 0, len(keyed.Imports))
	for importPath := range keyed.Imports {
		importPaths = append(importPaths, importPath)
	}
	slices.Sort(importPaths)
	return importPaths
}

// reachableFrom is the given packages and every package they import, each once. An import map holding no package
// for a path is an error: a key would have nothing to hash for it.
func reachableFrom(given []*packages.Package) ([]*packages.Package, error) {
	seen := map[*packages.Package]bool{}
	var reachable []*packages.Package
	waiting := slices.Clone(given)
	for len(waiting) > 0 {
		next := waiting[len(waiting)-1]
		waiting = waiting[:len(waiting)-1]
		if next == nil {
			return nil, fmt.Errorf("keying packages: a nil package was given")
		}
		if seen[next] {
			continue
		}
		seen[next] = true
		reachable = append(reachable, next)
		for importPath, imported := range next.Imports {
			if imported == nil {
				return nil, fmt.Errorf("keying %s: its import %s holds no package", next.ID, importPath)
			}
			waiting = append(waiting, imported)
		}
	}
	return reachable, nil
}

// fileDigests hashes every file of the packages that are not keyed by path alone, each once, across the cores:
// reading and hashing the files is nearly all of Keys' work. A file that cannot be read is an error, since a key
// without its bytes would say nothing about it.
func fileDigests(reachable []*packages.Package) (map[string]fileDigest, error) {
	seen := map[string]bool{}
	var files []string
	for _, reached := range reachable {
		if pathOnly(reached) {
			continue
		}
		for _, list := range fileLists(reached) {
			for _, file := range list {
				if !seen[file] {
					seen[file] = true
					files = append(files, file)
				}
			}
		}
	}
	// Sorted so that of several unreadable files the same one is reported every time.
	slices.Sort(files)

	digests := make([]fileDigest, len(files))
	readErrors := make([]error, len(files))
	indexes := make(chan int)
	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), max(len(files), 1)) {
		workers.Go(func() {
			for index := range indexes {
				digests[index], readErrors[index] = digestOf(files[index])
			}
		})
	}
	for index := range files {
		indexes <- index
	}
	close(indexes)
	workers.Wait()

	byPath := make(map[string]fileDigest, len(files))
	for index, file := range files {
		if readErrors[index] != nil {
			return nil, readErrors[index]
		}
		byPath[file] = digests[index]
	}
	return byPath, nil
}

// digestOf is the sha256 of one file's bytes, streamed rather than read whole.
func digestOf(file string) (fileDigest, error) {
	opened, openError := os.Open(file)
	if openError != nil {
		return fileDigest{}, fmt.Errorf("hashing %s: %w", file, openError)
	}
	defer func() {
		// Ignored: the file was only read, and every byte of it hashed or the copy failed.
		_ = opened.Close()
	}()
	digester := sha256.New()
	if _, copyError := io.Copy(digester, opened); copyError != nil {
		return fileDigest{}, fmt.Errorf("hashing %s: %w", file, copyError)
	}
	var digest fileDigest
	copy(digest[:], digester.Sum(nil))
	return digest, nil
}
