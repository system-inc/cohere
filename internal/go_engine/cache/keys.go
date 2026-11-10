package cache

import (
	"fmt"
	"slices"

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

	// Portable is each package's own digest but for the context: its files with their contents, module, import map by
	// ID and extra inputs. The context holds `GOOS`, cgo and the flags, so an own digest never matches across builds,
	// while a package of an extra build whose portable digest is the host's read the same inputs there (#b2t9xmf).
	Portable map[*packages.Package]Key

	// Files are the digests of every file Keys keyed, those it was given included, which StandardLibraryKeys reads
	// rather than hashing the standard library's files again in the same run, and an extra build's Keys under
	// `--other-builds` reads rather than hashing again the files the host's build shares with it.
	Files FileDigests
}

// Keys computes every package's key, for the packages given and everything they import, reading each file once.
// extraInputs, when non-nil, returns more inputs for one package (a rule's reads outside the pass), each hashed in.
// known are digests the run already holds, read rather than hashed again, and the caller vouches that nothing wrote
// any of those files since: the zero value holds none. held is the bytes the load parsed in place of a file's on disk,
// hashed in their place, so a key is of what the load read; nil hashes every file from disk.
//
// A key is a sha256 over two parts, each length-prefixed: the package's own digest, then the keys of the packages it
// imports, sorted by import path. The own digest is a sha256 over, in this order and each part length-prefixed: the
// context (its format version, engine identity, rule set and build, hashed once for every package); the package's
// ID, PkgPath and Name; its Module's path, version and go version, and its replacement's; each of its GoFiles,
// CompiledGoFiles, OtherFiles, EmbedFiles and IgnoredFiles as its path and the sha256 of its bytes; its import map,
// path to ID, sorted by path; and its extra inputs. Since a package's key holds its dependencies' keys, a change
// anywhere below it changes it, and a change in it leaves them alone. The portable digest is a sha256 over the own
// digest's parts after the context, alone.
func Keys(
	given []*packages.Package,
	shared Context,
	extraInputs func(*packages.Package) ([]string, error),
	known FileDigests,
	held func(file string) ([]byte, bool),
) (*Keyed, error) {
	reachable, reachableError := reachableFrom(given)
	if reachableError != nil {
		return nil, reachableError
	}
	digests, digestsError := fileDigests(reachable, known, held)
	if digestsError != nil {
		return nil, digestsError
	}
	computing := &keyer{
		context:     contextDigest(shared),
		extraInputs: extraInputs,
		fileDigests: digests,
		keys:        make(map[*packages.Package]Key, len(reachable)),
		own:         make(map[*packages.Package]Key, len(reachable)),
		portable:    make(map[*packages.Package]Key, len(reachable)),
		visiting:    map[*packages.Package]bool{},
	}
	for _, root := range given {
		if _, keyError := computing.key(root); keyError != nil {
			return nil, keyError
		}
	}
	return &Keyed{Keys: computing.keys, Own: computing.own, Portable: computing.portable,
		Files: FileDigests{byPath: digests}}, nil
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
