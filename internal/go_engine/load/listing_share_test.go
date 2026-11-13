package load

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The run's one table of go/build's HasSubdir and IsDir answers gives the answer go/build's own default does, so
// ImportDir reads every directory as it would without it, package for package, error for error (#5jpss95): a link to a
// file read as a Go file and a link to a directory named like one left out, a directory reached through a link, the
// toolchain's source reached through a link either way, a module path below `GOPATH` named through one, a file and a
// path that does not exist read as no directory. Each is asked twice, the second answered from the table.
func TestSharedHooksImportEveryDirectoryAsGoBuildsOwn(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeListingFiles(test, root, map[string]string{
		"tree/package/plain.go":                        "package linked\n",
		"tree/elsewhere/real.go":                       "package linked\n\nimport _ \"errors\"\n",
		"tree/package/directory/inner.go":              "package inner\n",
		"path/src/example.com/thing/thing.go":          "package thing\n",
		"path/src/example.com/thing/testdata/fixed.go": "package fixed\n",
	})
	// The toolchain's root as go env reads it, since a test binary built with -trimpath leaves go/build's own empty.
	toolchainRoot := goEnvironmentOf(test, root, nil).value("GOROOT")
	if toolchainRoot == "" {
		test.Fatal("go env named no GOROOT")
	}
	toolchainSource := filepath.Join(toolchainRoot, "src")
	links := map[string]string{
		// A link to a file among a package's entries, which go/build reads as a Go file.
		"tree/package/linked.go": filepath.Join(root, "tree", "elsewhere", "real.go"),
		// A link to a directory named like a Go file, which go/build leaves out.
		"tree/package/directory.go": filepath.Join(root, "tree", "package", "directory"),
		// The tree, the toolchain's source and a `GOPATH` as reached through links.
		"view":      filepath.Join(root, "tree"),
		"errors":    filepath.Join(toolchainSource, "errors"),
		"toolchain": toolchainRoot,
		"gopath":    filepath.Join(root, "path"),
	}
	for name, target := range links {
		if linkError := os.Symlink(target, filepath.Join(root, filepath.FromSlash(name))); linkError != nil {
			test.Fatal(linkError)
		}
	}
	directories := []string{
		filepath.Join(root, "tree", "package"),
		filepath.Join(root, "view", "package"),
		filepath.Join(root, "errors"),
		filepath.Join(toolchainSource, "errors"),
		filepath.Join(root, "toolchain", "src", "errors"),
		filepath.Join(root, "path", "src", "example.com", "thing"),
		filepath.Join(root, "gopath", "src", "example.com", "thing"),
		filepath.Join(root, "path", "src", "example.com", "thing", "testdata"),
		filepath.Join(root, "tree", "package", "plain.go"),
		filepath.Join(root, "tree", "missing"),
	}
	for _, givenRoot := range []string{toolchainRoot, filepath.Join(root, "toolchain")} {
		for _, goPath := range []string{filepath.Join(root, "path"), filepath.Join(root, "gopath")} {
			plain := build.Default
			plain.GOROOT, plain.GOPATH = givenRoot, goPath
			shared := NewSharedReads()
			hooked := plain
			hooked.HasSubdir, hooked.IsDir = shared.hasSubdirectory, shared.isDirectory
			for _, directory := range directories {
				wanted, wantedError := plain.ImportDir(directory, 0)
				for ask := range 2 {
					imported, importError := hooked.ImportDir(directory, 0)
					if fmt.Sprint(importError) != fmt.Sprint(wantedError) {
						test.Errorf("GOROOT %s, GOPATH %s, %s, ask %d: imported with %v, go/build's own with %v",
							givenRoot, goPath, directory, ask, importError, wantedError)
					}
					if !reflect.DeepEqual(imported, wanted) {
						test.Errorf("GOROOT %s, GOPATH %s, %s, ask %d: imported %+v, go/build's own %+v",
							givenRoot, goPath, directory, ask, imported, wanted)
					}
				}
			}
		}
	}
	// What go/build's own reads of the links, so each case above proves what it is there for.
	plain := build.Default
	plain.GOROOT, plain.GOPATH = filepath.Join(root, "toolchain"), filepath.Join(root, "gopath")
	if read, importError := plain.ImportDir(filepath.Join(root, "tree", "package"), 0); importError != nil ||
		!reflect.DeepEqual(read.GoFiles, []string{"linked.go", "plain.go"}) {
		test.Errorf("go/build read %v of the package with %v, want the linked file and the plain one", read.GoFiles,
			importError)
	}
	if read, importError := plain.ImportDir(filepath.Join(root, "errors"), 0); importError != nil || !read.Goroot ||
		read.ImportPath != "errors" {
		test.Errorf("go/build read the linked errors as %s, standard %v, with %v, want the toolchain's errors",
			read.ImportPath, read.Goroot, importError)
	}
	if read, importError := plain.ImportDir(filepath.Join(root, "path", "src", "example.com", "thing"),
		0); importError != nil || read.ImportPath != "example.com/thing" {
		test.Errorf("go/build read the GOPATH package as %q with %v, want example.com/thing", read.ImportPath,
			importError)
	}
}

// Every extra build's listing is the one it makes with nothing shared, its kept answer and that answer's fingerprint
// included, as the engine lists them (#5jpss95): the host's through the run's reads alone, then each extra build's
// through the reads and the builds' fingerprints, a first time, keeping its answer, and a second, reading it kept. A
// fingerprint read once for every build is each build's own, since a directory's fingerprint is of its entries and
// headers, which no build changes. It holds on a module of targeted files and on the engine's cgo fixture.
func TestEveryBuildsListingIsItsOwnWithTheRunsTablesShared(test *testing.T) {
	test.Parallel()
	targeted := test.TempDir()
	writeListingFiles(test, targeted, targetedModuleFiles)
	cgoFixture, absoluteError := filepath.Abs(filepath.Join("..", "testdata", "cgo"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	for _, directory := range []string{targeted, cgoFixture} {
		shared := NewSharedReads()
		fingerprints := NewFingerprints()
		ownListings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
		sharedListings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
		patterns := []string{"./..."}
		hostEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, sharedListingTargets[0]...))
		if _, _, listError := cachedList(hostEnvironment, patterns, sharedListings, nil, shared, nil,
			nil); listError != nil {
			test.Fatalf("%s: the host's listing: %v", directory, listError)
		}
		for round := range 2 {
			for _, target := range sharedListingTargets[1:] {
				goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, target...))
				alone, aloneKept, aloneError := cachedList(goEnvironment, patterns, ownListings, nil, nil, nil, nil)
				through, throughKept, throughError := cachedList(goEnvironment, patterns, sharedListings, nil, shared,
					fingerprints, nil)
				if aloneError != nil || throughError != nil {
					test.Fatalf("%s %v round %d: listed alone with %v, shared with %v", directory, target, round,
						aloneError, throughError)
				}
				if throughKept != aloneKept || throughKept != (round == 1) {
					test.Errorf("%s %v round %d: kept shared %v, alone %v, want both %v", directory, target, round,
						throughKept, aloneKept, round == 1)
				}
				if !reflect.DeepEqual(through, alone) {
					test.Errorf("%s %v round %d: the listing shared differs from its own:\n%s", directory, target,
						round, listingDifferences(through, alone))
				}
				key := listingKey(goEnvironment, patterns)
				ownKept, sharedKept := decodedKeptListing(test, ownListings, key), decodedKeptListing(test,
					sharedListings, key)
				if sharedKept.Fingerprint != ownKept.Fingerprint {
					test.Errorf("%s %v round %d: the kept fingerprint shared is %x, its own %x", directory, target,
						round, sharedKept.Fingerprint, ownKept.Fingerprint)
				}
				if !reflect.DeepEqual(sharedKept, ownKept) {
					test.Errorf("%s %v round %d: the kept listing shared differs from its own", directory, target,
						round)
				}
			}
		}
	}
}

// Each directory is fingerprinted once a run however many builds list it (#5jpss95): five extra builds, each keeping
// its listing and then reading it kept, ask for every directory's fingerprint ten times, and each is hashed once.
func TestEachDirectoryIsFingerprintedOnceARun(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, targetedModuleFiles)
	fingerprints := NewFingerprints()
	listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
	for range 2 {
		for _, target := range sharedListingTargets[1:] {
			goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, target...))
			if _, _, listError := cachedList(goEnvironment, []string{"./..."}, listings, nil, NewSharedReads(),
				fingerprints, nil); listError != nil {
				test.Fatalf("%v: %v", target, listError)
			}
		}
	}
	// Every directory the pattern walks: the module's own, command, which holds no package, and command/tool.
	if len(fingerprints.directories) != 3 {
		test.Errorf("the builds fingerprinted %d directories, want the module's 3", len(fingerprints.directories))
	}
	if computed := fingerprints.computed.Load(); computed != int64(len(fingerprints.directories)) {
		test.Errorf("%d directories were fingerprinted %d times, want once each", len(fingerprints.directories),
			computed)
	}
}

// decodedKeptListing is the listing listings keeps under key, failing the test when it keeps none.
func decodedKeptListing(test *testing.T, listings *listingMap, key [sha256.Size]byte) keptListing {
	test.Helper()
	contents, stored := listings.Kept(key)
	if !stored {
		test.Fatalf("no listing is kept under %x", key)
	}
	var kept keptListing
	if decodeError := gob.NewDecoder(bytes.NewReader(contents)).Decode(&kept); decodeError != nil {
		test.Fatal(decodeError)
	}
	return kept
}
