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
	"slices"
	"sync"
	"testing"
)

// pickedListingFiles are a module whose listing on linux names a directory its listing on freebsd does not, and
// nothing else: both builds import base, in a module beside it the project replaces by path, so both kept listings
// name that module's files, and only linux's file imports lib, in the same module, so lib's directory is in linux's
// kept listing alone.
var pickedListingFiles = map[string]string{
	"project/go.mod": "module example.com/picked\n\ngo 1.24\n\nrequire example.com/outside v0.0.0\n\n" +
		"replace example.com/outside => ../outside\n",
	"project/picked.go": "package picked\n\nimport _ \"example.com/outside/base\"\n\n// Picked is one.\n" +
		"func Picked() int { return 1 }\n",
	"project/picked_linux.go": "package picked\n\nimport _ \"example.com/outside/lib\"\n",
	"outside/go.mod":          "module example.com/outside\n\ngo 1.24\n",
	"outside/base/base.go":    "package base\n\n// Base is one.\nfunc Base() int { return 1 }\n",
	"outside/lib/lib.go":      "package lib\n\n// Lib is two.\nfunc Lib() int { return 2 }\n",
}

// twoModuleWorkspaceFiles are a go.work root of two modules, the root's own and second, which the go.work uses beside
// it: the root's package imports second's, and second holds a directory with no package, which only second's own walk
// reads.
var twoModuleWorkspaceFiles = map[string]string{
	"go.work":               "go 1.27\n\nuse (\n\t.\n\t./second\n)\n",
	"go.mod":                "module example.com/first\n\ngo 1.27\n",
	"first.go":              "package first\n\nimport _ \"example.com/second\"\n",
	"second/go.mod":         "module example.com/second\n\ngo 1.27\n",
	"second/second.go":      "package second\n",
	"second/notes/notes.md": "Notes, in a directory with no package.\n",
}

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
// included, as cohere-go lists them (#5jpss95): the host's through the run's reads alone, then each extra build's
// through the reads, the builds' fingerprints and the builds' walks (#tb538na), a first time, keeping its answer, and
// in the next run, under fingerprints and walks of its own as each run has, a second, reading it kept: under the
// first run's, each would be answered from what that run remembers (#32594zm), and nothing would be read kept. A
// fingerprint read once for every build is each build's own, since a directory's
// fingerprint is of its entries and headers, which no build changes, and so is a walk, which reads only entries. It
// holds on a module of targeted files, on cohere-go's cgo fixture, on a module whose builds' kept listings name other
// directories, and on a go.work root of two modules, whose kept listings name two roots and two go.mod files, each
// root's walk and each module file its own (#qqyq61s).
func TestEveryBuildsListingIsItsOwnWithTheRunsTablesShared(test *testing.T) {
	test.Parallel()
	targeted := test.TempDir()
	writeListingFiles(test, targeted, targetedModuleFiles)
	cgoFixture, absoluteError := filepath.Abs(filepath.Join("..", "testdata", "cgo"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	picked := test.TempDir()
	writeListingFiles(test, picked, pickedListingFiles)
	twoModules := test.TempDir()
	writeListingFiles(test, twoModules, twoModuleWorkspaceFiles)
	for _, fixture := range []struct {
		directory   string
		environment []string
		patterns    []string
		roots       int
	}{
		{directory: targeted, environment: []string{"GOWORK=off"}, patterns: []string{"./..."}, roots: 1},
		{directory: cgoFixture, environment: []string{"GOWORK=off"}, patterns: []string{"./..."}, roots: 1},
		{
			directory:   filepath.Join(picked, "project"),
			environment: []string{"GOWORK=off"},
			patterns:    []string{"./..."},
			roots:       1,
		},
		{directory: twoModules, patterns: []string{"./...", "example.com/second/..."}, roots: 2},
	} {
		directory := fixture.directory
		shared := NewSharedReads()
		ownListings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
		sharedListings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
		patterns := fixture.patterns
		hostEnvironment := goEnvironmentOf(test, directory, append(slices.Clone(fixture.environment),
			sharedListingTargets[0]...))
		if _, _, listError := cachedList(hostEnvironment, patterns, sharedListings, nil, shared, nil, nil,
			nil); listError != nil {
			test.Fatalf("%s: the host's listing: %v", directory, listError)
		}
		for round := range 2 {
			fingerprints := NewFingerprints()
			walks := NewPackageWalks()
			for _, target := range sharedListingTargets[1:] {
				goEnvironment := goEnvironmentOf(test, directory, append(slices.Clone(fixture.environment), target...))
				alone, aloneKept, aloneError := cachedList(goEnvironment, patterns, ownListings, nil, nil, nil, nil,
					nil)
				through, throughKept, throughError := cachedList(goEnvironment, patterns, sharedListings, nil, shared,
					fingerprints, walks, nil)
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
				if len(ownKept.Roots) != fixture.roots {
					test.Fatalf("%s %v round %d: the kept listing names roots %v, want %d", directory, target, round,
						ownKept.Roots, fixture.roots)
				}
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

// Each directory is fingerprinted once a run however many builds list it (#5jpss95): five extra builds keep their
// listings in one run, and in the next, under fingerprints of its own, as each run has, five read them kept, asking
// for every directory's fingerprint five times, and each is hashed once.
func TestEachDirectoryIsFingerprintedOnceARun(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, targetedModuleFiles)
	var fingerprints *Fingerprints
	listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
	for range 2 {
		fingerprints = NewFingerprints()
		for _, target := range sharedListingTargets[1:] {
			goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, target...))
			if _, _, listError := cachedList(goEnvironment, []string{"./..."}, listings, nil, NewSharedReads(),
				fingerprints, nil, nil); listError != nil {
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

// Each pattern's walk of package directories is walked once a run however many builds list it (#tb538na): five extra
// builds, keeping no listing, so each lists again, list a local pattern and a module pattern, ten walks asked, and each
// pattern's is walked once. Each build's listing is the one it makes walking for itself.
func TestEachPatternIsWalkedOnceARun(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, targetedModuleFiles)
	walks := NewPackageWalks()
	for _, pattern := range []string{"./...", "example.com/targeted/..."} {
		for _, target := range sharedListingTargets[1:] {
			goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, target...))
			alone, aloneError := list(goEnvironment, []string{pattern}, nil, nil, nil, nil)
			through, throughError := list(goEnvironment, []string{pattern}, nil, NewSharedReads(), walks, nil)
			if aloneError != nil || throughError != nil {
				test.Fatalf("%s %v: listed alone with %v, shared with %v", pattern, target, aloneError, throughError)
			}
			if !through.inProcess {
				test.Fatalf("%s %v: go list answered, since %s, so nothing was walked", pattern, target,
					through.goListReason)
			}
			if !reflect.DeepEqual(through, alone) {
				test.Errorf("%s %v: the listing through the walks differs from its own:\n%s", pattern, target,
					listingDifferences(through, alone))
			}
		}
	}
	if len(walks.walks) != 2 {
		test.Errorf("the builds walked %d patterns, want the local one and the module one", len(walks.walks))
	}
	if computed := walks.computed.Load(); computed != int64(len(walks.walks)) {
		test.Errorf("%d patterns were walked %d times, want once each", len(walks.walks), computed)
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

// A kept listing's steady fingerprint is read once a run for every build whose kept listing names the same inputs
// (#qqyq61s): five extra builds side by side keep their listings in one run, and in the next, under fingerprints of
// its own, as each run has, five read them kept, and it is read once, its root walked once and each steady module
// file read once. Each build reads its listing kept the second time.
func TestAListingFingerprintIsReadOnceARunForEachSetOfInputs(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, targetedModuleFiles)
	var fingerprints *Fingerprints
	listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
	targets := sharedListingTargets[1:]
	environments := make([]*GoEnvironment, len(targets))
	for index, target := range targets {
		environments[index] = goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, target...))
	}
	for round := range 2 {
		fingerprints = NewFingerprints()
		var group sync.WaitGroup
		for index, goEnvironment := range environments {
			group.Go(func() {
				_, kept, listError := cachedList(goEnvironment, []string{"./..."}, listings, nil, NewSharedReads(),
					fingerprints, nil, nil)
				if listError != nil || kept != (round == 1) {
					test.Errorf("%v round %d: kept %v with %v, want kept %v", targets[index], round, kept, listError,
						round == 1)
				}
			})
		}
		group.Wait()
	}
	if len(fingerprints.listings) != 1 {
		test.Errorf("the builds' kept listings named %d sets of inputs, want the module's one",
			len(fingerprints.listings))
	}
	if computed := fingerprints.listingsComputed.Load(); computed != 1 {
		test.Errorf("one set of inputs was fingerprinted %d times, want once", computed)
	}
	if walked := fingerprints.rootsWalked.Load(); len(fingerprints.roots) != 1 || walked != 1 {
		test.Errorf("%d roots were walked %d times, want the module's one, once", len(fingerprints.roots), walked)
	}
	if read := fingerprints.moduleFilesRead.Load(); read != int64(len(fingerprints.moduleFiles)) {
		test.Errorf("%d module files were read %d times, want once each", len(fingerprints.moduleFiles), read)
	}
}

// A kept listing's fingerprint is keyed by the directories it names (#qqyq61s): of two builds of one run, whose kept
// listings name the same roots and module files, linux's names lib's directory and freebsd's does not, so linux's
// reads a fingerprint of its own rather than freebsd's, read first, and its listing is kept while nothing moved. A file
// added in lib, the directory linux's alone names, asks go list again for linux and not for freebsd, and linux's
// listing is then the one it makes with nothing kept. The two sets share their root's walk and their module files.
func TestAListingFingerprintIsKeyedByTheDirectoriesItNames(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeListingFiles(test, root, pickedListingFiles)
	project := filepath.Join(root, "project")
	freebsd := goEnvironmentOf(test, project, append([]string{"GOWORK=off"}, sharedListingTargets[3]...))
	linux := goEnvironmentOf(test, project, append([]string{"GOWORK=off"}, sharedListingTargets[1]...))
	listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
	// runBuilds lists freebsd's build and then linux's under fingerprints of their own, as one run's extra builds do,
	// and reports whether each was kept and linux's listing.
	runBuilds := func(run string) (bool, bool, *listing, *Fingerprints) {
		fingerprints := NewFingerprints()
		_, freebsdKept, freebsdError := cachedList(freebsd, []string{"./..."}, listings, nil, NewSharedReads(),
			fingerprints, nil, nil)
		linuxListed, linuxKept, linuxError := cachedList(linux, []string{"./..."}, listings, nil, NewSharedReads(),
			fingerprints, nil, nil)
		if freebsdError != nil || linuxError != nil {
			test.Fatalf("%s: listed freebsd with %v, linux with %v", run, freebsdError, linuxError)
		}
		return freebsdKept, linuxKept, linuxListed, fingerprints
	}
	runBuilds("the first run")
	libDirectory := filepath.Join(root, "outside", "lib")
	linuxListing := decodedKeptListing(test, listings, listingKey(linux, []string{"./..."}))
	if !slices.Contains(linuxListing.Directories, libDirectory) {
		test.Fatalf("linux's kept listing names %v, without lib's directory", linuxListing.Directories)
	}
	freebsdListing := decodedKeptListing(test, listings, listingKey(freebsd, []string{"./..."}))
	if slices.Contains(freebsdListing.Directories, libDirectory) {
		test.Fatalf("freebsd's kept listing names lib's directory: %v", freebsdListing.Directories)
	}
	if !slices.Equal(linuxListing.Roots, freebsdListing.Roots) ||
		!slices.Equal(linuxListing.ModuleFiles, freebsdListing.ModuleFiles) {
		test.Fatalf("linux's kept listing names roots %v and module files %v, freebsd's %v and %v, want the same",
			linuxListing.Roots, linuxListing.ModuleFiles, freebsdListing.Roots, freebsdListing.ModuleFiles)
	}
	freebsdKept, linuxKept, _, fingerprints := runBuilds("the second run")
	if !freebsdKept || !linuxKept {
		test.Errorf("with nothing moved, freebsd's listing kept %v and linux's %v, want both kept", freebsdKept,
			linuxKept)
	}
	if computed := fingerprints.listingsComputed.Load(); len(fingerprints.listings) != 2 || computed != 2 {
		test.Errorf("%d sets of inputs were fingerprinted %d times, want two, once each", len(fingerprints.listings),
			computed)
	}
	if walked := fingerprints.rootsWalked.Load(); len(fingerprints.roots) != 1 || walked != 1 {
		test.Errorf("%d roots were walked %d times, want the project's one, once", len(fingerprints.roots), walked)
	}
	if read := fingerprints.moduleFilesRead.Load(); read != int64(len(fingerprints.moduleFiles)) {
		test.Errorf("%d module files were read %d times, want once each", len(fingerprints.moduleFiles), read)
	}
	writeListingFiles(test, root, map[string]string{
		"outside/lib/more.go": "package lib\n\n// More is three.\nfunc More() int { return 3 }\n",
	})
	freebsdKept, linuxKept, linuxListed, _ := runBuilds("the run after lib moved")
	if !freebsdKept || linuxKept {
		test.Errorf("after lib moved, freebsd's listing kept %v and linux's %v, want freebsd's alone kept",
			freebsdKept, linuxKept)
	}
	fresh, freshError := list(linux, []string{"./..."}, nil, nil, nil, nil)
	if freshError != nil {
		test.Fatal(freshError)
	}
	if !reflect.DeepEqual(linuxListed, fresh) {
		test.Errorf("after lib moved, linux's listing differs from the one it makes with nothing kept:\n%s",
			listingDifferences(linuxListed, fresh))
	}
}

// A run's fingerprints are never carried into the next (#qqyq61s): a build keeps its listing under one run's
// fingerprints, an import is added to a file's header, and the next run's build, under fingerprints of its own, reads
// the edit and asks go list again. It guards that NewFingerprints carries nothing from one to the next; that one is
// made for each run, in newPipeline, cohere-go's TestEachRunsPipelineHoldsFingerprintsOfItsOwn pins.
func TestAListingFingerprintIsReadAgainEachRun(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, targetedModuleFiles)
	goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, sharedListingTargets[1]...))
	listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
	for run, wanted := range []bool{false, true} {
		_, kept, listError := cachedList(goEnvironment, []string{"./..."}, listings, nil, NewSharedReads(),
			NewFingerprints(), nil, nil)
		if listError != nil || kept != wanted {
			test.Fatalf("run %d: kept %v with %v, want kept %v", run, kept, listError, wanted)
		}
	}
	writeListingFiles(test, directory, map[string]string{
		"targeted.go": "package targeted\n\nimport (\n\t\"os\"\n\t\"strings\"\n)\n\n// Name is the program's name.\n" +
			"func Name() string { return strings.TrimSpace(os.Args[0]) }\n",
	})
	edited, kept, listError := cachedList(goEnvironment, []string{"./..."}, listings, nil, NewSharedReads(),
		NewFingerprints(), nil, nil)
	if listError != nil || kept {
		test.Fatalf("after the import was added: kept %v with %v, want go list asked again", kept, listError)
	}
	targeted := edited.byPath["example.com/targeted"]
	if targeted == nil || !slices.Contains(targeted.Imports, "strings") {
		test.Errorf("after the import was added, the package listed is %+v, without strings among its imports",
			targeted)
	}
}

// rewrittenModuleFile is a module file a go command of a run can rewrite while its builds read it, in a module of
// targetedModuleFiles with files beside it, its builds' environment after each target's, and the bytes it is rewritten
// to.
type rewrittenModuleFile struct {
	name        string
	files       map[string]string
	environment []string
	file        string
	contents    string
}

// rewrittenModuleFiles are the module files a build's go list can rewrite: go.work.sum in a go.work root, when the
// workspace's guard runs it in the tree. go.mod and go.sum it never writes, a user's -mod=mod included, since the
// load runs its go commands at -mod=readonly then (#ycf4240).
var rewrittenModuleFiles = []rewrittenModuleFile{
	{
		name:     "go.work.sum",
		files:    map[string]string{"go.work": "go 1.27\n\nuse .\n", "go.work.sum": ""},
		file:     "go.work.sum",
		contents: "golang.org/x/text v0.3.0/go.mod h1:NqM8EUOU14njkJ3fqMW+pc6Ldnwhi/IjpwHt7yyuwOQ=\n",
	},
}

// A module file a go command of the run can rewrite is read again at every ask, never from what the run's fingerprints
// hold (#qqyq61s). Two builds keep listings over one set of inputs. In the next run linux's reads its listing kept, the
// file is rewritten, standing in for linux's go list, and freebsd's, asking through the same fingerprints, reads the
// new bytes, asks go list again and keeps its listing against them: the run after reads freebsd's listing kept and not
// linux's, kept against the bytes before.
func TestAModuleFileTheRunsGoCanRewriteIsReadAgainAtEveryAsk(test *testing.T) {
	test.Parallel()
	for _, rewritten := range rewrittenModuleFiles {
		test.Run(rewritten.name, func(test *testing.T) {
			test.Parallel()
			directory := test.TempDir()
			writeListingFiles(test, directory, targetedModuleFiles)
			writeListingFiles(test, directory, rewritten.files)
			linux := goEnvironmentOf(test, directory, append(slices.Clone(sharedListingTargets[1]),
				rewritten.environment...))
			freebsd := goEnvironmentOf(test, directory, append(slices.Clone(sharedListingTargets[3]),
				rewritten.environment...))
			listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
			// ask lists build's patterns through fingerprints and reports whether its listing was kept.
			ask := func(build *GoEnvironment, fingerprints *Fingerprints) bool {
				_, kept, listError := cachedList(build, []string{"./..."}, listings, nil, NewSharedReads(),
					fingerprints, nil, nil)
				if listError != nil {
					test.Fatal(listError)
				}
				return kept
			}
			first := NewFingerprints()
			ask(linux, first)
			ask(freebsd, first)
			linuxListing := decodedKeptListing(test, listings, listingKey(linux, []string{"./..."}))
			freebsdListing := decodedKeptListing(test, listings, listingKey(freebsd, []string{"./..."}))
			if !slices.Equal(linuxListing.Roots, freebsdListing.Roots) ||
				!slices.Equal(linuxListing.Directories, freebsdListing.Directories) ||
				!slices.Equal(linuxListing.ModuleFiles, freebsdListing.ModuleFiles) {
				test.Fatalf("linux's and freebsd's kept listings name other inputs: %+v and %+v", linuxListing,
					freebsdListing)
			}
			if !slices.Contains(linuxListing.ModuleFiles, filepath.Join(directory, rewritten.file)) {
				test.Fatalf("the kept listings name %v, without %s", linuxListing.ModuleFiles, rewritten.file)
			}
			second := NewFingerprints()
			if !ask(linux, second) {
				test.Fatal("with nothing moved, linux's listing was not kept")
			}
			writeListingFiles(test, directory, map[string]string{rewritten.file: rewritten.contents})
			if ask(freebsd, second) {
				test.Errorf("after %s was rewritten, freebsd's listing was kept against the bytes before",
					rewritten.file)
			}
			third := NewFingerprints()
			if freebsdKept, linuxKept := ask(freebsd, third), ask(linux, third); !freebsdKept || linuxKept {
				test.Errorf("the run after %s was rewritten kept freebsd's listing %v and linux's %v, want freebsd's "+
					"alone", rewritten.file, freebsdKept, linuxKept)
			}
		})
	}
}

// A steady fingerprint is keyed by each list it is read from alone (#qqyq61s): a set that differs from one read first
// in its roots alone, its directories alone, its module files alone, or by a part moved from one list into the next,
// reads its own fingerprint, the one read with nothing shared. No two kept listings differ in their module files
// alone, since keepListing names a module's files only beside a directory of its packages, so the sets are asked here
// directly.
func TestASteadyFingerprintIsKeyedByEachListAlone(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeListingFiles(test, root, map[string]string{
		"first/first.go":   "package first\n",
		"second/second.go": "package second\n",
		"go.mod":           "module example.com/keyed\n\ngo 1.27\n",
		"go.sum":           "",
	})
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	moduleFile, sumFile := filepath.Join(root, "go.mod"), filepath.Join(root, "go.sum")
	readFirst := [3][]string{{first}, {second}, {moduleFile}}
	readFirstAlone, _ := steadyFingerprint(readFirst[0], readFirst[1], readFirst[2], nil, nil)
	for name, other := range map[string][3][]string{
		"its roots":                         {{second}, {second}, {moduleFile}},
		"its directories":                   {{first}, {first}, {moduleFile}},
		"its module files":                  {{first}, {second}, {sumFile}},
		"a directory moved among its roots": {{first, second}, nil, {moduleFile}},
	} {
		fingerprints := NewFingerprints()
		fingerprints.steady(readFirst[0], readFirst[1], readFirst[2], nil)
		shared, sharedCertain := fingerprints.steady(other[0], other[1], other[2], nil)
		alone, aloneCertain := steadyFingerprint(other[0], other[1], other[2], nil, nil)
		if alone == readFirstAlone {
			test.Fatalf("a set that differs in %s reads the fingerprint of the set read first", name)
		}
		if shared != alone || !sharedCertain || !aloneCertain {
			test.Errorf("a set that differs in %s read %x through the run's fingerprints, certain %v, and %x alone, "+
				"certain %v", name, shared, sharedCertain, alone, aloneCertain)
		}
	}
}

// A doubt the run's fingerprints hold is a doubt for every build that asks after it (#qqyq61s). With go.sum a
// directory, a module file that cannot be read and is not absent, two builds through one run's fingerprints keep no
// listing, and neither reads one kept at its second ask; and a root that cannot be walked, one that is not there, reads
// as a doubt at its first ask and its second.
func TestADoubtIsADoubtForEveryBuildThatAsks(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, targetedModuleFiles)
	if makeError := os.Mkdir(filepath.Join(directory, "go.sum"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	fingerprints := NewFingerprints()
	listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
	for ask := range 2 {
		for _, target := range [][]string{sharedListingTargets[1], sharedListingTargets[3]} {
			goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, target...))
			_, kept, listError := cachedList(goEnvironment, []string{"./..."}, listings, nil, NewSharedReads(),
				fingerprints, nil, nil)
			if listError != nil || kept {
				test.Errorf("%v ask %d: kept %v with %v, want go list asked", target, ask, kept, listError)
			}
		}
	}
	if len(listings.listings) != 0 || len(fingerprints.listings) != 1 {
		test.Errorf("with go.sum unreadable the builds kept %d listings over %d sets of inputs, want none over one",
			len(listings.listings), len(fingerprints.listings))
	}
	missing := filepath.Join(directory, "missing")
	for ask := range 2 {
		if _, certain := listingFingerprint([]string{missing}, nil, nil, nil, fingerprints); certain {
			test.Errorf("ask %d: a root that is not there read as walked whole", ask)
		}
	}
}

// A module file a go command of the run can rewrite that cannot be read is a doubt too, though it is read at every
// ask and never through the run's fingerprints (#qqyq61s): with a go.work root's go.work.sum a directory, its listing's
// fingerprint is a doubt at its first ask and its second, read through one run's fingerprints or with none, and a
// listing that names it is never kept.
func TestAnUnreadableRewritableModuleFileIsADoubt(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, targetedModuleFiles)
	writeListingFiles(test, directory, map[string]string{"go.work": "go 1.27\n\nuse .\n"})
	if makeError := os.Mkdir(filepath.Join(directory, "go.work.sum"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	moduleFiles := []string{filepath.Join(directory, "go.mod"), filepath.Join(directory, "go.work.sum")}
	fingerprints := NewFingerprints()
	for ask := range 2 {
		for name, through := range map[string]*Fingerprints{"the run's fingerprints": fingerprints, "none": nil} {
			if _, certain := listingFingerprint([]string{directory}, nil, moduleFiles, nil, through); certain {
				test.Errorf("ask %d through %s: a listing naming an unreadable go.work.sum read as certain", ask, name)
			}
		}
	}
	listed := &listing{byPath: map[string]*listedPackage{}, roots: map[string]bool{}, testOnly: map[string]bool{}}
	if kept, keepable := keepListing(directory, "", "", listed, nil, NewFingerprints()); keepable {
		test.Errorf("a listing naming an unreadable go.work.sum was kept, over module files %v", kept.ModuleFiles)
	}
}
