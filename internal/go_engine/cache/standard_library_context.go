package cache

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// rootMarker stands for the Go installation's root in the path a standard-library file is keyed by, so the key names
// where the file is in the installation and never where the installation is.
const rootMarker = "$GOROOT/"

// StandardLibraryContext is what every standard-library package's key shares in one run. It names nothing of the
// machine but what can change a standard-library entry, its installation's root among them, so a key computed on one
// machine equals the key on any other with the same engine, Go version, target, fact analyzers and `GOROOT`.
type StandardLibraryContext struct {
	// FormatVersion is the cache's own format version, so an entry written in another format is never read.
	FormatVersion string

	// EngineIdentity names the engine whose entries these are, as Context's EngineIdentity does: ExecutableIdentity
	// for a run reading and writing the system cache, so an engine reads only what it wrote itself, or a stamp a test
	// gives in its place. StandardLibraryKeys refuses an empty one.
	EngineIdentity string

	// Build is the target, cgo, the toolchain, its experiments and the go flags narrowed for the standard library,
	// "GOOS=linux" and the rest, in a fixed order.
	Build []string

	// Facts is each fact analyzer as its name, then its flags with their values as one JSON object, sorted.
	Facts []string

	// Loader is which loader ran, load's LoaderSource or LoaderCgoTranslated. Only a package with cgo files is
	// checked differently under the two, so only its own digest holds it, and a package importing one holds it
	// through its dependency's key: a run of a project with cgo of its own misses no package that never reaches cgo.
	Loader string

	// CgoToolchain is the C toolchain go env reports, "CC=clang" and the rest, in load.CgoToolchainNames' order. Under
	// the cgo-translated loader a package with cgo files is checked from what the cgo tool made with it, so its own
	// digest holds the toolchain beside the loader, and only then: under the source loader the cgo tool never runs.
	CgoToolchain []string

	// Goroot is the Go installation's root, `GOROOT` as go env reports it, absolute. It is hashed into every key: the
	// system cache's store holds each entry with this machine's paths in it, its files, its withheld errors and its
	// export data's positions, so an installation moved, or another beside it, keys apart (#syscache). Each file is
	// keyed by its path below it besides.
	Goroot string
}

// StandardLibraryKeys computes the key of every standard-library package among reachable and the packages they
// import, reading each file once, and none that known, the digests Keys hashed earlier in the run, already holds: a
// cached run hashes every file for Keys first, and a run under `--no-cache`, which computes no Keys, gives none and
// hashes only the standard library's files. A package is the standard library's when it has no module, every file of
// every list it holds is below the context's Goroot, and every package it imports is the standard library's too. Any
// other package gets no key, which is no error: it is analyzed as it always is. unsafe and a package with no files
// are keyed by their path alone, as Keys keys them.
//
// A key is a sha256 over two parts, each length-prefixed: the package's own digest, then the keys of the packages it
// imports, sorted by import path, as Keys' are. The own digest is a sha256 over, in this order and each part
// length-prefixed: the context (its format version, engine identity, fact analyzers, build and Goroot, hashed once
// for every package); the package's ID, PkgPath and Name; each of its GoFiles, CompiledGoFiles, OtherFiles,
// EmbedFiles and IgnoredFiles as its path below Goroot, written `$GOROOT/` and then slash-separated, and the sha256 of
// its bytes; and, for a package with cgo files alone, the loader, then the C toolchain when the loader is
// cgo-translated. Nothing of the process goes in: only what the context names and the files' bytes.
func StandardLibraryKeys(
	reachable []*packages.Package,
	shared StandardLibraryContext,
	known FileDigests,
) (map[*packages.Package]Key, error) {
	if shared.EngineIdentity == "" {
		return nil, fmt.Errorf("keying the standard library: no engine identity was given")
	}
	if !filepath.IsAbs(shared.Goroot) {
		return nil, fmt.Errorf("keying the standard library: GOROOT %q is not absolute", shared.Goroot)
	}
	computing := &standardLibraryKeyer{
		context:      standardLibraryContextDigest(shared),
		loader:       shared.Loader,
		cgoToolchain: shared.CgoToolchain,
		goroot:       shared.Goroot,
		rootRelative: map[string]string{},
		standard:     map[*packages.Package]bool{},
		keys:         map[*packages.Package]Key{},
		visiting:     map[*packages.Package]bool{},
	}
	for _, candidate := range reachable {
		if candidate == nil {
			return nil, fmt.Errorf("keying the standard library: a nil package was given")
		}
		if _, judgeError := computing.isStandard(candidate); judgeError != nil {
			return nil, judgeError
		}
	}
	// Every package judged the standard library's, those only imported among them, since each one's imports were
	// judged before it.
	var standard []*packages.Package
	for judged, isStandard := range computing.standard {
		if isStandard {
			standard = append(standard, judged)
		}
	}
	// No run holds a standard-library file, so each is read from disk.
	digests, digestsError := fileDigests(standard, known, nil)
	if digestsError != nil {
		return nil, digestsError
	}
	computing.fileDigests = digests
	for _, keyed := range standard {
		computing.key(keyed)
	}
	return computing.keys, nil
}

// standardLibraryContextDigest is the run's StandardLibraryContext hashed once, in the key's order: a marker that sets
// these keys apart from Keys', the format version, the engine identity, the fact analyzers, the build and Goroot.
// The loader and the C toolchain are not in it, since neither belongs in every package's key.
func standardLibraryContextDigest(shared StandardLibraryContext) Key {
	parts := newPartHash()
	parts.text("standard library")
	parts.text(shared.FormatVersion)
	parts.text(shared.EngineIdentity)
	parts.texts(shared.Facts)
	parts.texts(shared.Build)
	parts.text(shared.Goroot)
	return parts.sum()
}

// belowRoot is file as a standard-library key names it, `$GOROOT/` then its path below goroot slash-separated, and
// false for a file that is not absolute or not below goroot. The paths are compared as written, symbolic links
// unresolved, as go list names both from the same `GOROOT`.
func belowRoot(goroot, file string) (string, bool) {
	if !filepath.IsAbs(file) {
		return "", false
	}
	relative, relativeError := filepath.Rel(goroot, file)
	if relativeError != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rootMarker + filepath.ToSlash(relative), true
}

// hasCgoFiles reports whether a package has cgo files, which packages.Package holds among its GoFiles without saying
// which. The load, from either lister, gives a package with cgo files the imports the cgo tool adds (see load's
// newNode), runtime/cgo among them whenever go list says the package depends on it, which go says of every package
// with cgo files but runtime/cgo itself, the one package cgo leaves the import out of so it does not import itself.
// A package that imports runtime/cgo by hand without cgo files reads as having some, which only puts the loader in
// its key.
func hasCgoFiles(keyed *packages.Package) bool {
	_, importsCgoRuntime := keyed.Imports["runtime/cgo"]
	return importsCgoRuntime || keyed.PkgPath == "runtime/cgo"
}
