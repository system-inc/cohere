package cache

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// The chain fixture's packages: top imports middle, which imports bottom, and top has in-package and external
// tests. The plain top is analyzed as its test variant, so no package of the load imports it.
const (
	bottomId      = "example.com/chain/bottom"
	middleId      = "example.com/chain/middle"
	topId         = "example.com/chain/top [example.com/chain/top.test]"
	topExternalId = "example.com/chain/top_test [example.com/chain/top.test]"
)

// fixtureEnvironment is what the chain is loaded under: off the repository's go.work, and with cgo off, so the
// load is the same on every machine.
var fixtureEnvironment = []string{"GOWORK=off", "CGO_ENABLED=0", "GOFLAGS="}

// fixtureContext is the context every test keys under unless it is the context being changed.
var fixtureContext = Context{
	FormatVersion:  "1",
	EngineIdentity: "engine",
	Build:          []string{"GOOS=linux", "GOARCH=amd64", "loader=source"},
	RuleSet:        []string{"first", `second {"limit":2}`},
}

// printKeysVariable, set, makes the test binary print the chain's keys and exit, so a test can compare them with
// another process's.
const printKeysVariable = "COHERE_CACHE_PRINT_KEYS"

// fixture is the chain loaded once per test binary, in place in testdata, and its keys by ID: each load
// type-checks the standard library from source, seconds of work every test only reads.
var fixture struct {
	once    sync.Once
	loaded  *load.Loaded
	keys    map[string]Key
	failure error
}

// TestMain prints the chain's keys instead of running the tests when printKeysVariable is set.
func TestMain(tests *testing.M) {
	if os.Getenv(printKeysVariable) != "" {
		if printError := printKeys(); printError != nil {
			// Ignored: the exit code already says the keys were not printed, and standard error is the last
			// place to say why.
			_, _ = fmt.Fprintln(os.Stderr, printError)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(tests.Run())
}

// printKeys loads the chain and prints each package's ID and key, a line each.
func printKeys() error {
	loaded, loadError := loadChain(chainDirectory())
	if loadError != nil {
		return loadError
	}
	keyed, keysError := Keys(loaded.Packages, fixtureContext, nil, FileDigests{}, nil)
	if keysError != nil {
		return keysError
	}
	var lines strings.Builder
	for id, key := range keysById(keyed.Keys) {
		lines.WriteString(id + "\t" + key.String() + "\n")
	}
	if _, writeError := os.Stdout.WriteString(lines.String()); writeError != nil {
		return fmt.Errorf("printing the keys: %w", writeError)
	}
	return nil
}

// chainDirectory is the chain fixture in testdata. A test runs in its package's directory.
func chainDirectory() string {
	directory, absoluteError := filepath.Abs(filepath.Join("testdata", "chain"))
	if absoluteError != nil {
		// Invariant: the working directory of a test binary always resolves.
		panic(absoluteError)
	}
	return directory
}

// loadChain loads the chain module at directory.
func loadChain(directory string) (*load.Loaded, error) {
	loaded, loadError := load.Packages(directory, []string{"./..."}, fixtureEnvironment, nil)
	if loadError != nil {
		return nil, fmt.Errorf("loading the chain at %s: %w", directory, loadError)
	}
	if len(loaded.Diagnostics) > 0 {
		return nil, fmt.Errorf("loading the chain at %s: %+v", directory, loaded.Diagnostics)
	}
	return loaded, nil
}

// loadedFixture loads and keys the chain on first use.
func loadedFixture(test *testing.T) (*load.Loaded, map[string]Key) {
	test.Helper()

	fixture.once.Do(func() {
		fixture.loaded, fixture.failure = loadChain(chainDirectory())
		if fixture.failure != nil {
			return
		}
		keyed, keysError := Keys(fixture.loaded.Packages, fixtureContext, nil, FileDigests{}, nil)
		if keysError != nil {
			fixture.failure = keysError
			return
		}
		fixture.keys = keysById(keyed.Keys)
	})
	if fixture.failure != nil {
		test.Fatalf("keying the chain: %v", fixture.failure)
	}
	return fixture.loaded, fixture.keys
}

// keysById is keys by each package's ID, which is unique within one load.
func keysById(keys map[*packages.Package]Key) map[string]Key {
	byId := make(map[string]Key, len(keys))
	for keyed, key := range keys {
		byId[keyed.ID] = key
	}
	return byId
}

// cloneGraph copies every package the given ones reach, so a test can change one without touching the shared
// fixture: each clone's file lists, import map and module are its own, and its imports are clones. It returns
// the clones of the given packages, and every clone by ID.
func cloneGraph(given []*packages.Package) ([]*packages.Package, map[string]*packages.Package) {
	clones := map[*packages.Package]*packages.Package{}
	var cloneOf func(*packages.Package) *packages.Package
	cloneOf = func(original *packages.Package) *packages.Package {
		if earlier, cloned := clones[original]; cloned {
			return earlier
		}
		clone := &packages.Package{}
		*clone = *original
		clones[original] = clone
		clone.GoFiles = slices.Clone(original.GoFiles)
		clone.CompiledGoFiles = slices.Clone(original.CompiledGoFiles)
		clone.OtherFiles = slices.Clone(original.OtherFiles)
		clone.EmbedFiles = slices.Clone(original.EmbedFiles)
		clone.IgnoredFiles = slices.Clone(original.IgnoredFiles)
		if original.Module != nil {
			module := *original.Module
			clone.Module = &module
		}
		clone.Imports = make(map[string]*packages.Package, len(original.Imports))
		for importPath, imported := range original.Imports {
			clone.Imports[importPath] = cloneOf(imported)
		}
		return clone
	}
	roots := make([]*packages.Package, 0, len(given))
	for _, root := range given {
		roots = append(roots, cloneOf(root))
	}
	byId := make(map[string]*packages.Package, len(clones))
	for _, clone := range clones {
		byId[clone.ID] = clone
	}
	return roots, byId
}

// changedIds are the IDs whose keys differ between before and after, sorted. A package in only one of them
// counts as changed.
func changedIds(before, after map[string]Key) []string {
	var changed []string
	for id, key := range after {
		if earlier, known := before[id]; !known || earlier != key {
			changed = append(changed, id)
		}
	}
	for id := range before {
		if _, known := after[id]; !known {
			changed = append(changed, id)
		}
	}
	slices.Sort(changed)
	return changed
}

// expectChanged fails the test unless exactly the packages want names changed key between before and after.
func expectChanged(test *testing.T, what string, before, after map[string]Key, want ...string) {
	test.Helper()

	// Sorted as a copy: the parallel subtests share one slice of every package, and sorting it in place let one
	// subtest reorder another's expectation while it was being read (#xja4mef).
	sortedWant := slices.Sorted(slices.Values(want))
	if changed := changedIds(before, after); !slices.Equal(changed, sortedWant) {
		test.Errorf("%s: want the keys of\n%q\nchanged, and only those, got\n%q", what, sortedWant, changed)
	}
}

// unchanged is the change for a test that keys the clone as loaded, changing the context or the extra inputs
// instead.
func unchanged(map[string]*packages.Package) {
	// Nothing: the graph is keyed as it was loaded.
}

// keyClone keys a clone of the fixture after change has changed it, and returns the keys by ID.
func keyClone(
	test *testing.T,
	shared Context,
	extraInputs func(*packages.Package) ([]string, error),
	change func(map[string]*packages.Package),
) map[string]Key {
	test.Helper()

	loaded, _ := loadedFixture(test)
	roots, byId := cloneGraph(loaded.Packages)
	change(byId)
	keyed, keysError := Keys(roots, shared, extraInputs, FileDigests{}, nil)
	if keysError != nil {
		test.Fatalf("keying a changed clone of the chain: %v", keysError)
	}
	return keysById(keyed.Keys)
}

// Every package the analyzed ones reach is keyed, the standard library included, each differently, and keying the
// same graph twice gives the same keys.
func TestKeysCoverEveryPackageReachedAndAreTheSameAcrossCalls(test *testing.T) {
	test.Parallel()

	loaded, keys := loadedFixture(test)
	reached := 0
	packages.Visit(loaded.Packages, nil, func(*packages.Package) { reached++ })
	if len(keys) != reached {
		test.Errorf("want a key for each of the %d packages reached, got %d", reached, len(keys))
	}
	for _, id := range []string{bottomId, middleId, topId, topExternalId, "testing", "unsafe"} {
		if _, keyed := keys[id]; !keyed {
			test.Errorf("%s has no key", id)
		}
	}
	distinct := map[Key]string{}
	for id, key := range keys {
		if other, taken := distinct[key]; taken {
			test.Errorf("%s and %s share the key %s", id, other, key)
		}
		distinct[key] = id
	}

	again, keysError := Keys(loaded.Packages, fixtureContext, nil, FileDigests{}, nil)
	if keysError != nil {
		test.Fatalf("keying the chain again: %v", keysError)
	}
	expectChanged(test, "keying twice", keys, keysById(again.Keys))
}

// Another process keying the same files under the same context gives the same keys: nothing in a key depends on
// the process, such as map order or a pointer.
func TestKeysAreTheSameInAnotherProcess(test *testing.T) {
	test.Parallel()

	_, keys := loadedFixture(test)
	command := exec.Command(os.Args[0], "-test.run=^$")
	command.Env = append(os.Environ(), printKeysVariable+"=1")
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		test.Fatalf("printing the keys in another process: %v\n%s", runError, standardError.String())
	}
	printed := map[string]Key{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		id, hexKey, cut := strings.Cut(scanner.Text(), "\t")
		if !cut {
			test.Fatalf("the other process printed %q, want an ID and a key", scanner.Text())
		}
		decoded, decodeError := hex.DecodeString(hexKey)
		if decodeError != nil || len(decoded) != len(Key{}) {
			test.Fatalf("the other process printed the key %q for %s: %v", hexKey, id, decodeError)
		}
		printed[id] = Key(decoded)
	}
	if len(printed) == 0 {
		test.Fatal("the other process printed no keys")
	}
	expectChanged(test, "another process", keys, printed)
}

// copyChain copies the chain fixture to a directory of the test's own, so the test can edit its files.
func copyChain(test *testing.T) string {
	test.Helper()

	source := chainDirectory()
	destination := test.TempDir()
	walkError := filepath.WalkDir(source, func(path string, entry fs.DirEntry, entryError error) error {
		if entryError != nil {
			return entryError
		}
		relative, relativeError := filepath.Rel(source, path)
		if relativeError != nil {
			return relativeError
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(destination, relative), 0o755)
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		return os.WriteFile(filepath.Join(destination, relative), contents, 0o644)
	})
	if walkError != nil {
		test.Fatalf("copying the chain: %v", walkError)
	}
	return destination
}

// A byte changed in any file a package holds, a Go file, an other file, an embedded file or an ignored file,
// changes the key of that package and of every package that imports it, transitively, and of no other. A change
// in the leaf of the chain leaves its dependencies' keys alone.
func TestAByteOfAFileChangesItsPackageAndItsImportersOnly(test *testing.T) {
	test.Parallel()

	directory := copyChain(test)
	loaded, loadError := loadChain(directory)
	if loadError != nil {
		test.Fatal(loadError)
	}
	var bottom *packages.Package
	packages.Visit(loaded.Packages, nil, func(visited *packages.Package) {
		if visited.ID == bottomId {
			bottom = visited
		}
	})
	if bottom == nil {
		test.Fatalf("the chain loaded without %s", bottomId)
	}
	// The load from source lists no embedded files; go/packages does with NeedEmbedFiles, and a key must hold them
	// when it does.
	bottom.EmbedFiles = []string{filepath.Join(directory, "bottom", "data.txt")}
	if !slices.Contains(bottom.OtherFiles, filepath.Join(directory, "bottom", "notes.s")) ||
		!slices.Contains(bottom.IgnoredFiles, filepath.Join(directory, "bottom", "ignored.go")) {
		test.Fatalf("bottom loaded without its other and ignored files: %q, %q", bottom.OtherFiles, bottom.IgnoredFiles)
	}

	keyed, keysError := Keys(loaded.Packages, fixtureContext, nil, FileDigests{}, nil)
	if keysError != nil {
		test.Fatal(keysError)
	}
	before, ownBefore, portableBefore := keysById(keyed.Keys), keysById(keyed.Own), keysById(keyed.Portable)
	// A file's own package changes its own digest and its portable digest, and only its: an importer's key changes,
	// its own digests do not.
	for _, step := range []struct {
		file    string
		want    []string
		wantOwn []string
	}{
		{"bottom/bottom.go", []string{bottomId, middleId, topId, topExternalId}, []string{bottomId}},
		{"bottom/notes.s", []string{bottomId, middleId, topId, topExternalId}, []string{bottomId}},
		{"bottom/data.txt", []string{bottomId, middleId, topId, topExternalId}, []string{bottomId}},
		{"bottom/ignored.go", []string{bottomId, middleId, topId, topExternalId}, []string{bottomId}},
		{"middle/middle.go", []string{middleId, topId, topExternalId}, []string{middleId}},
		{"top/top.go", []string{topId, topExternalId}, []string{topId}},
		{"top/top_external_test.go", []string{topExternalId}, []string{topExternalId}},
	} {
		path := filepath.Join(directory, filepath.FromSlash(step.file))
		contents, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		// One byte more: a trailing newline changes no meaning, so only the bytes can tell.
		if writeError := os.WriteFile(path, append(contents, '\n'), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		keyed, keysError = Keys(loaded.Packages, fixtureContext, nil, FileDigests{}, nil)
		if keysError != nil {
			test.Fatal(keysError)
		}
		after, ownAfter, portableAfter := keysById(keyed.Keys), keysById(keyed.Own), keysById(keyed.Portable)
		expectChanged(test, "a byte of "+step.file, before, after, step.want...)
		expectChanged(test, "the own digests on a byte of "+step.file, ownBefore, ownAfter, step.wantOwn...)
		expectChanged(test, "the portable digests on a byte of "+step.file, portableBefore, portableAfter,
			step.wantOwn...)
		before, ownBefore, portableBefore = after, ownAfter, portableAfter
	}
}

// A package another build reads the same files of, under the same import map, has the same portable digest there,
// while its own digest differs by the build's context: the chain's packages hold no file of one target, so each is
// portable between linux and windows, and runtime, whose files differ by target, is not (#b2t9xmf).
func TestThePortableDigestIsTheSameInAnotherBuildOfTheSameFiles(test *testing.T) {
	test.Parallel()

	builds := map[string]map[string]Key{}
	owns := map[string]map[string]Key{}
	for _, goos := range []string{"linux", "windows"} {
		loaded, loadError := load.Packages(chainDirectory(), []string{"./..."},
			append(slices.Clone(fixtureEnvironment), "GOOS="+goos, "GOARCH=amd64"), nil)
		if loadError != nil {
			test.Fatalf("loading the chain for %s: %v", goos, loadError)
		}
		if len(loaded.Diagnostics) > 0 {
			test.Fatalf("loading the chain for %s: %+v", goos, loaded.Diagnostics)
		}
		built := fixtureContext
		built.Build = []string{"GOOS=" + goos, "GOARCH=amd64", "loader=source"}
		keyed, keysError := Keys(loaded.Packages, built, nil, FileDigests{}, nil)
		if keysError != nil {
			test.Fatalf("keying the chain for %s: %v", goos, keysError)
		}
		builds[goos], owns[goos] = keysById(keyed.Portable), keysById(keyed.Own)
	}
	for _, id := range []string{bottomId, middleId, topId, topExternalId, "unsafe"} {
		if builds["linux"][id] != builds["windows"][id] {
			test.Errorf("%s's portable digest differs between linux and windows, though its files do not", id)
		}
		if owns["linux"][id] == owns["windows"][id] {
			test.Errorf("%s's own digest is the same in linux and windows, though their contexts differ", id)
		}
	}
	if builds["linux"]["runtime"] == builds["windows"]["runtime"] {
		test.Error("runtime's portable digest is the same in linux and windows, though its files differ")
	}
}

// A file added to a package, or one taken out of it, changes the key of that package and its importers.
func TestAFileAddedOrRemovedChangesItsPackageAndItsImporters(test *testing.T) {
	test.Parallel()

	_, keys := loadedFixture(test)
	added := filepath.Join(test.TempDir(), "added.go")
	if writeError := os.WriteFile(added, []byte("package middle\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	withAdded := keyClone(test, fixtureContext, nil, func(byId map[string]*packages.Package) {
		byId[middleId].GoFiles = append(byId[middleId].GoFiles, added)
		byId[middleId].CompiledGoFiles = append(byId[middleId].CompiledGoFiles, added)
	})
	expectChanged(test, "a file added", keys, withAdded, middleId, topId, topExternalId)

	withRemoved := keyClone(test, fixtureContext, nil, func(byId map[string]*packages.Package) {
		byId[bottomId].IgnoredFiles = nil
	})
	expectChanged(test, "a file removed", keys, withRemoved, bottomId, middleId, topId, topExternalId)

	// The same file in another of the lists is another package: a file the build constraints now leave out.
	moved := keyClone(test, fixtureContext, nil, func(byId map[string]*packages.Package) {
		byId[bottomId].GoFiles = append(byId[bottomId].GoFiles, byId[bottomId].IgnoredFiles...)
		byId[bottomId].IgnoredFiles = nil
	})
	expectChanged(test, "a file moved between lists", keys, moved, bottomId, middleId, topId, topExternalId)
}

// Each field of the context changes every key: a new field the key forgets fails here, since the fields are
// found by reflection.
func TestEachContextFieldChangesEveryKey(test *testing.T) {
	test.Parallel()

	_, keys := loadedFixture(test)
	every := make([]string, 0, len(keys))
	for id := range keys {
		every = append(every, id)
	}
	fields := reflect.TypeFor[Context]()
	for index := range fields.NumField() {
		field := fields.Field(index)
		test.Run(field.Name, func(test *testing.T) {
			test.Parallel()

			changed := fixtureContext
			changed.Build = slices.Clone(fixtureContext.Build)
			changed.RuleSet = slices.Clone(fixtureContext.RuleSet)
			value := reflect.ValueOf(&changed).Elem().Field(index)
			switch value.Kind() {
			case reflect.String:
				value.SetString(value.String() + "+")
			case reflect.Slice:
				value.Set(reflect.Append(value, reflect.ValueOf("another")))
			default:
				test.Fatalf("Context.%s is a %s, which this test does not know how to change", field.Name, value.Kind())
			}
			after := keyClone(test, changed, nil, unchanged)
			expectChanged(test, "Context."+field.Name, keys, after, every...)
		})
	}
}

// The import map changes the key of its package and its importers, even when the path added reaches a package
// already imported, so the dependencies' keys alone do not change.
func TestTheImportMapChangesItsPackageAndItsImporters(test *testing.T) {
	test.Parallel()

	_, keys := loadedFixture(test)
	aliased := keyClone(test, fixtureContext, nil, func(byId map[string]*packages.Package) {
		byId[middleId].Imports["example.com/chain/alias"] = byId[bottomId]
	})
	expectChanged(test, "an import path added", keys, aliased, middleId, topId, topExternalId)

}

// Each part of a package's module changes the key of its package and its importers.
func TestTheModuleChangesItsPackageAndItsImporters(test *testing.T) {
	test.Parallel()

	_, keys := loadedFixture(test)
	for name, change := range map[string]func(*packages.Module){
		"path":       func(module *packages.Module) { module.Path = "example.com/other" },
		"version":    func(module *packages.Module) { module.Version = "v1.0.0" },
		"go version": func(module *packages.Module) { module.GoVersion = "1.22" },
		"replace":    func(module *packages.Module) { module.Replace = &packages.Module{Path: "../elsewhere"} },
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			after := keyClone(test, fixtureContext, nil, func(byId map[string]*packages.Package) {
				change(byId[middleId].Module)
			})
			expectChanged(test, "the module's "+name, keys, after, middleId, topId, topExternalId)
		})
	}
}

// A package's extra inputs change the key of that package and its importers, and an error reading them is Keys'
// error.
func TestExtraInputsChangeItsPackageAndItsImporters(test *testing.T) {
	test.Parallel()

	_, keys := loadedFixture(test)
	inputsOfMiddle := func(keyed *packages.Package) ([]string, error) {
		if keyed.ID == middleId {
			return []string{"policy.json 1f2e"}, nil
		}
		return nil, nil
	}
	after := keyClone(test, fixtureContext, inputsOfMiddle, unchanged)
	expectChanged(test, "an extra input", keys, after, middleId, topId, topExternalId)

	loaded, _ := loadedFixture(test)
	_, keysError := Keys(loaded.Packages, fixtureContext, func(*packages.Package) ([]string, error) {
		return nil, fs.ErrPermission
	}, FileDigests{}, nil)
	if keysError == nil || !strings.Contains(keysError.Error(), "extra inputs") {
		test.Errorf("want the extra inputs' error, got %v", keysError)
	}
}

// Two contexts whose parts run together alike, "ab" then "c" against "a" then "bc", key apart: each part goes in
// with its length.
func TestPartsThatRunTogetherAlikeKeyApart(test *testing.T) {
	test.Parallel()

	_, keys := loadedFixture(test)
	for name, contexts := range map[string][2]Context{
		"the format version and the engine": {
			{FormatVersion: "ab", EngineIdentity: "c"},
			{FormatVersion: "a", EngineIdentity: "bc"},
		},
		"two rules": {
			{RuleSet: []string{"ab", "c"}},
			{RuleSet: []string{"a", "bc"}},
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			first := keyClone(test, contexts[0], nil, unchanged)
			second := keyClone(test, contexts[1], nil, unchanged)
			if first[bottomId] == second[bottomId] {
				test.Errorf("%+v and %+v gave bottom the same key", contexts[0], contexts[1])
			}
			if len(first) != len(keys) {
				test.Errorf("want %d keys, got %d", len(keys), len(first))
			}
		})
	}
}

// A package that imports itself through a cycle, which Go never builds, is an error, not a hang.
func TestAnImportCycleIsAnError(test *testing.T) {
	test.Parallel()

	loaded, _ := loadedFixture(test)
	roots, byId := cloneGraph(loaded.Packages)
	byId[bottomId].Imports["example.com/chain/top"] = byId[topId]
	_, keysError := Keys(roots, fixtureContext, nil, FileDigests{}, nil)
	if keysError == nil || !strings.Contains(keysError.Error(), "import cycle") {
		test.Errorf("want an import cycle's error, got %v", keysError)
	}
}

// unsafe is keyed by its path alone: its types are go/types' own, so its file is never read.
func TestUnsafeIsKeyedByItsPathAlone(test *testing.T) {
	test.Parallel()

	_, keys := loadedFixture(test)
	after := keyClone(test, fixtureContext, nil, func(byId map[string]*packages.Package) {
		byId["unsafe"].GoFiles = []string{filepath.Join(test.TempDir(), "missing.go")}
		byId["unsafe"].Module = &packages.Module{Path: "example.com/unsafe"}
	})
	expectChanged(test, "unsafe's files and module", keys, after)
}

// A file a package holds that cannot be read is an error naming it: a key without its bytes would vouch for
// nothing.
func TestAnUnreadableFileIsAnError(test *testing.T) {
	test.Parallel()

	loaded, _ := loadedFixture(test)
	roots, byId := cloneGraph(loaded.Packages)
	missing := filepath.Join(test.TempDir(), "missing.go")
	byId[middleId].GoFiles = append(byId[middleId].GoFiles, missing)
	_, keysError := Keys(roots, fixtureContext, nil, FileDigests{}, nil)
	if keysError == nil || !strings.Contains(keysError.Error(), missing) {
		test.Errorf("want an error naming %s, got %v", missing, keysError)
	}
}

// A key reads as lowercase hex, as its entry's file is named.
func TestAKeyReadsAsLowercaseHex(test *testing.T) {
	test.Parallel()

	var key Key
	key[0], key[31] = 0xAB, 0x0F
	want := "ab" + strings.Repeat("00", 30) + "0f"
	if key.String() != want {
		test.Errorf("want %s, got %s", want, key.String())
	}
}

// A key is of the bytes the load parsed: a file held in place of the disk's, as the editor's save holds its buffer, is
// hashed from the held bytes, so its package and every importer key apart from the disk's, and held bytes that are
// the disk's key as the disk does. Hashed from disk instead, a save's entry would be written under the key of the file
// on disk, and the next run over that file would restore what the buffer held.
func TestHeldBytesAreKeyedInPlaceOfTheDisks(test *testing.T) {
	test.Parallel()

	loaded, keys := loadedFixture(test)
	middle := filepath.Join(chainDirectory(), "middle", "middle.go")
	onDisk, readError := os.ReadFile(middle)
	if readError != nil {
		test.Fatal(readError)
	}
	keyedWith := func(contents []byte) map[string]Key {
		test.Helper()
		keyed, keysError := Keys(loaded.Packages, fixtureContext, nil, FileDigests{},
			func(file string) ([]byte, bool) {
				if file == middle {
					return contents, true
				}
				return nil, false
			})
		if keysError != nil {
			test.Fatal(keysError)
		}
		return keysById(keyed.Keys)
	}
	expectChanged(test, "the disk's bytes held", keys, keyedWith(onDisk))
	expectChanged(test, "a buffer held for middle.go", keys, keyedWith(append(onDisk, '\n')),
		middleId, topId, topExternalId)
}
