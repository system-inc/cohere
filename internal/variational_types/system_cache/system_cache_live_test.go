package system_cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/go-types/canonical"
	"github.com/system-inc/go-types/gcimporter"
	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/variational_types/system_cache/export_data"
)

// The live tests read this machine's system cache, as the engine binary liveEngineVariable names wrote it, against
// what go list compiles for the same builds. They are opt-in, since they read what no test made: the system cache of
// whatever engine last ran. Each extra build of a run keeps a pack of the records it read there, by key, which says
// neither the build nor each package's path, so a pack's records are matched to a build's go list by each package's
// name and imports (packedKeys), and the host's build, which keeps no pack, reads a darwin extra build's: the engine
// keys a build's standard library by its go flags, but a build tag of the root's no standard-library file reads.

// liveEngineVariable names the cohere binary whose system cache entries the live tests read, spelled as the
// environment spells it.
const liveEngineVariable = "COHERE_SYSTEM_CACHE_ENGINE"

// liveBuild is one build the live tests read: its target and cgo, as go env spells them.
type liveBuild struct {
	goos string
	cgo  string
}

// environment is the build's go environment over the process's, with no workspace, flags or toolchain switch.
func (build liveBuild) environment() []string {
	return append(withoutVariables(os.Environ(), "GOWORK", "GOFLAGS", "GOOS", "GOARCH", "CGO_ENABLED"),
		"GOOS="+build.goos, "GOARCH=arm64", "CGO_ENABLED="+build.cgo, "GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=")
}

// The builds the live tests read: the host's, with cgo, and windows/arm64, without.
var (
	hostBuild    = liveBuild{goos: "darwin", cgo: "1"}
	windowsBuild = liveBuild{goos: "windows", cgo: "0"}
)

// withoutVariables is environment without the entries of names.
func withoutVariables(environment []string, names ...string) []string {
	var kept []string
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(names, name) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// listedPackage is what go list says of one package.
type listedPackage struct {
	ImportPath string
	Name       string
	Imports    []string
	Export     string
	Standard   bool
}

// goList is what go list says, in directory under build, with arguments after `go list -json`.
func goList(test *testing.T, build liveBuild, directory string, arguments ...string) []listedPackage {
	test.Helper()
	command := exec.Command("go", append([]string{"list", "-e", "-json"}, arguments...)...)
	command.Dir = directory
	command.Env = build.environment()
	output, listError := command.Output()
	if listError != nil {
		var exitError *exec.ExitError
		if errors.As(listError, &exitError) {
			test.Fatalf("go list %v for %s: %v\n%s", arguments, build.goos, listError, exitError.Stderr)
		}
		test.Fatalf("go list %v for %s: %v", arguments, build.goos, listError)
	}
	var listed []listedPackage
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var one listedPackage
		if decodeError := decoder.Decode(&one); errors.Is(decodeError, io.EOF) {
			return listed
		} else if decodeError != nil {
			test.Fatal(decodeError)
		}
		listed = append(listed, one)
	}
}

// exportsOf is each package of listed by path, its compiler export data's file, for one with export data.
func exportsOf(listed []listedPackage) map[string]string {
	exports := map[string]string{}
	for _, one := range listed {
		if one.Export != "" {
			exports[one.ImportPath] = one.Export
		}
	}
	return exports
}

// packRecord is what packedKeys reads of a record: its format, its body, inline or by the key of the record that
// holds it, and its export data's digest.
type packRecord struct {
	Version    string
	Inline     []byte
	BodyAt     []byte
	ExportData []byte
}

// packBody is what packedKeys reads of an entry's body: the paths of the packages its types import.
type packBody struct {
	Imports []string
}

// signature is a package as packedKeys matches it: its name, then its imports sorted, "C" left out, which a check
// imports as a package of its own and go list does not.
func signature(name string, imports []string) string {
	kept := slices.DeleteFunc(slices.Clone(imports), func(path string) bool { return path == "C" })
	slices.Sort(kept)
	return name + " " + strings.Join(kept, " ")
}

// liveStore is the system cache's store as the engine finds it, and the identity of the engine binary
// liveEngineVariable names, or a skip saying how to run the live tests.
func liveStore(test *testing.T) (Store, string) {
	test.Helper()
	engine := os.Getenv(liveEngineVariable)
	if engine == "" {
		test.Skipf("reads this machine's system cache: set $%s to the cohere binary whose run over the root wrote "+
			"it, as <root>/.cache/cohere/bin/cohere-darwin-arm64-<hash>", liveEngineVariable)
	}
	contents, readError := os.ReadFile(engine)
	if readError != nil {
		test.Fatalf("reading the engine at $%s: %v", liveEngineVariable, readError)
	}
	identity := sha256.Sum256(contents)
	root, notKept := cache.SystemCacheRoot()
	if notKept != nil {
		test.Skipf("no system cache: %s", notKept.Reason())
	}
	return Store{Directory: filepath.Join(root, "go", "store")}, hex.EncodeToString(identity[:])
}

// packedKeys is, for each pack the engine of identity kept in store, the key of every package of its records that
// matches exactly one package of listed by its name and imports, by path, and how many of its records match none or
// more than one, the best-matched pack first. A record's body is its own or the record's at BodyAt, which an extra
// build writing a package through from the host's run names.
func packedKeys(test *testing.T, store Store, identity string, listed []listedPackage) ([]map[string]cache.Key, []int) {
	test.Helper()
	bySignature := map[string][]string{}
	for _, one := range listed {
		bySignature[signature(one.Name, one.Imports)] = append(bySignature[signature(one.Name, one.Imports)],
			one.ImportPath)
	}
	directory := filepath.Join(store.Directory, "packs", cache.PackIdentity(identity).String())
	names, listError := os.ReadDir(directory)
	if listError != nil {
		test.Skipf("the system cache holds no packs of engine %s: %v", identity, listError)
	}
	reader := cache.OpenAt(store.Directory, nil)
	type matched struct {
		keys      map[string]cache.Key
		unmatched int
	}
	var packs []matched
	for _, name := range names {
		var place cache.PackPlace
		place.Identity = cache.PackIdentity(identity)
		decoded, decodeError := hex.DecodeString(name.Name())
		if decodeError != nil || len(decoded) != len(place.Name) {
			continue
		}
		copy(place.Name[:], decoded)
		pack, refused := reader.ReadPack(place)
		if refused != "" {
			test.Logf("pack %s refused: %s", name.Name(), refused)
			continue
		}
		one := matched{keys: map[string]cache.Key{}}
		for asked, entry := range pack.Entries {
			path, found := matchedPath(store, entry.Entry, bySignature)
			if !found {
				one.unmatched++
				continue
			}
			one.keys[path] = asked
		}
		packs = append(packs, one)
	}
	slices.SortStableFunc(packs, func(left matched, right matched) int {
		return (len(right.keys) - right.unmatched) - (len(left.keys) - left.unmatched)
	})
	keys := make([]map[string]cache.Key, len(packs))
	unmatched := make([]int, len(packs))
	for index, pack := range packs {
		keys[index], unmatched[index] = pack.keys, pack.unmatched
	}
	return keys, unmatched
}

// matchedPath is the path of the one package of bySignature whose name and imports are those of the package whose
// record is contents, false for a record that does not read or matches none or more than one.
func matchedPath(store Store, contents []byte, bySignature map[string][]string) (string, bool) {
	var record packRecord
	if gob.NewDecoder(bytes.NewReader(contents)).Decode(&record) != nil || record.ExportData == nil {
		return "", false
	}
	body := record.Inline
	if body == nil && len(record.BodyAt) == len(cache.Key{}) {
		holder, held := store.Get(cache.Key(record.BodyAt))
		var holding packRecord
		if !held || gob.NewDecoder(bytes.NewReader(holder)).Decode(&holding) != nil {
			return "", false
		}
		body = holding.Inline
	}
	var decodedBody packBody
	if body == nil || gob.NewDecoder(bytes.NewReader(body)).Decode(&decodedBody) != nil {
		return "", false
	}
	data, stored := store.Get(exportDataKey(record.ExportData))
	if !stored {
		return "", false
	}
	var name string
	_, manifestError := export_data.IImportShallow(token.NewFileSet(), func(items []export_data.GetPackagesItem) error {
		name = items[0].Name
		return errorManifestRead
	}, data, "", func(string, ...any) {
		// Nothing past the manifest is read, so nothing can be reported.
	})
	if !errors.Is(manifestError, errorManifestRead) {
		return "", false
	}
	paths := bySignature[signature(name, decodedBody.Imports)]
	if len(paths) != 1 {
		return "", false
	}
	return paths[0], true
}

// spelledObjects is each exported package-level object of typed, by name, as its kind and its type spelled
// canonically, its constant value, and for a declared type its underlying type and each method's signature, sorted by
// method name.
func spelledObjects(typed *types.Package) map[string]string {
	spelled := map[string]string{}
	for _, name := range typed.Scope().Names() {
		if !token.IsExported(name) {
			continue
		}
		object := typed.Scope().Lookup(name)
		var described strings.Builder
		fmt.Fprintf(&described, "%T %s", object, canonical.Spell(object.Type()))
		if constantObject, isConstant := object.(*types.Const); isConstant {
			fmt.Fprintf(&described, " = %s", constantObject.Val().ExactString())
		}
		if typeName, isTypeName := object.(*types.TypeName); isTypeName && !typeName.IsAlias() {
			if named, isNamed := typeName.Type().(*types.Named); isNamed {
				fmt.Fprintf(&described, " underlying %s", canonical.Spell(named.Underlying()))
				methods := make([]string, 0, named.NumMethods())
				for index := range named.NumMethods() {
					methods = append(methods, named.Method(index).Name()+" "+
						canonical.Spell(named.Method(index).Type()))
				}
				slices.Sort(methods)
				fmt.Fprintf(&described, " methods [%s]", strings.Join(methods, "; "))
			}
		}
		spelled[name] = described.String()
	}
	return spelled
}

// compiledPackage is path's package read through the fork's lazy gcimporter from exports, compiler export data by
// path, into universe.
func compiledPackage(fileSet *token.FileSet, universe *gcimporter.Universe, exports map[string]string,
	path string) (*types.Package, error) {
	return gcimporter.Import(fileSet, universe, path, "", func(asked string) (io.ReadCloser, error) {
		file, compiled := exports[asked]
		if !compiled {
			return nil, fmt.Errorf("no export data for %q", asked)
		}
		return os.Open(file)
	})
}

// os and syscall read from the system cache for the host's build and windows/arm64 hold every exported package-level
// object as go list's compiler export data does for the same build, read through the fork's gcimporter: the same
// names, each of the same kind, type, value, underlying type and methods, spelled canonically.
func TestTheSystemCacheReadsAsTheCompilersExportData(test *testing.T) {
	test.Parallel()

	store, identity := liveStore(test)
	for _, build := range []liveBuild{hostBuild, windowsBuild} {
		test.Run(build.goos, func(test *testing.T) {
			test.Parallel()

			listed := goList(test, build, test.TempDir(), "std")
			keys, unmatched := packedKeys(test, store, identity, listed)
			if len(keys) == 0 {
				test.Skipf("engine %s kept no pack in the system cache", identity)
			}
			test.Logf("best pack of %d: %d packages matched, %d records unmatched", len(keys), len(keys[0]),
				unmatched[0])
			cached := NewBuild(store, keysOf(keys[0]), token.NewFileSet(), map[string]*types.Package{})
			exports := exportsOf(goList(test, build, test.TempDir(), "-export", "-deps", "os", "syscall"))
			fileSet := token.NewFileSet()
			universe := gcimporter.NewUniverse()
			for _, path := range []string{"os", "syscall"} {
				present, readError := cached.Read(path)
				if readError != nil {
					test.Fatalf("reading %s from the system cache: %v", path, readError)
				}
				if !present {
					test.Skipf("the system cache's best pack for %s has no %s", build.goos, path)
				}
				compiled, compiledError := compiledPackage(fileSet, universe, exports, path)
				if compiledError != nil {
					test.Fatalf("reading %s's compiler export data: %v", path, compiledError)
				}
				fromCache, fromCompiler := spelledObjects(cached.Packages[path]), spelledObjects(compiled)
				for name, spelled := range fromCompiler {
					if fromCache[name] != spelled {
						test.Errorf("%s.%s: the system cache has %q, the compiler %q", path, name, fromCache[name],
							spelled)
					}
				}
				for name, spelled := range fromCache {
					if _, compiledHas := fromCompiler[name]; !compiledHas {
						test.Errorf("%s.%s: the system cache has %q, the compiler nothing", path, name, spelled)
					}
				}
				test.Logf("%s: %d exported objects compared", path, len(fromCompiler))
			}
		})
	}
}

// The time to read every standard-library package the root's host build needs from the system cache, each whole,
// against reading the same packages' compiler export data through the fork's lazy gcimporter, imported alone and then
// with every exported object looked up. Packages outside the standard library are not in the system cache at all:
// the project's store keeps them.
//
// Not parallel: it measures, and a test running beside it would be in its times.
func TestTheSystemCacheDecodeTime(test *testing.T) {
	store, identity := liveStore(test)
	root, rootError := filepath.Abs(filepath.Join("..", "..", ".."))
	if rootError != nil {
		test.Fatal(rootError)
	}
	var needed []string
	for _, one := range goList(test, hostBuild, root, "-deps", "./...") {
		if one.Standard {
			needed = append(needed, one.ImportPath)
		}
	}
	keys, _ := packedKeys(test, store, identity, goList(test, hostBuild, test.TempDir(), "std"))
	if len(keys) == 0 {
		test.Skipf("engine %s kept no pack in the system cache", identity)
	}
	exports := exportsOf(goList(test, hostBuild, test.TempDir(), append([]string{"-export", "-deps"}, needed...)...))

	started := time.Now()
	for _, path := range needed {
		if key, keyed := keys[0][path]; keyed {
			store.ExportData(key)
		}
	}
	storeTime := time.Since(started)

	cached := NewBuild(store, keysOf(keys[0]), token.NewFileSet(), map[string]*types.Package{})
	started = time.Now()
	var absent []string
	for _, path := range needed {
		present, readError := cached.Read(path)
		if readError != nil {
			test.Fatalf("reading %s from the system cache: %v", path, readError)
		}
		if !present {
			absent = append(absent, path)
		}
	}
	cachedTime := time.Since(started)

	fileSet := token.NewFileSet()
	universe := gcimporter.NewUniverse()
	started = time.Now()
	var compiled []*types.Package
	for _, path := range needed {
		if path == "unsafe" || slices.Contains(absent, path) {
			continue
		}
		typed, compiledError := compiledPackage(fileSet, universe, exports, path)
		if compiledError != nil {
			test.Fatalf("reading %s's compiler export data: %v", path, compiledError)
		}
		compiled = append(compiled, typed)
	}
	importTime := time.Since(started)
	started = time.Now()
	for _, typed := range compiled {
		for _, name := range typed.Scope().Names() {
			if token.IsExported(name) {
				typed.Scope().Lookup(name)
			}
		}
	}
	lookupTime := time.Since(started)
	// A package absent here may be one the system cache holds whose record matched no package of go list's alone, or
	// one that names such a package: packedKeys finds keys by name and imports, which two packages can share.
	test.Logf("%d standard-library packages the host's build needs, %d absent from the pack's keys %v", len(needed),
		len(absent), absent)
	test.Logf("system cache, each read whole: %v, after %v reading its records and export data alone", cachedTime,
		storeTime)
	test.Logf("compiler export data, lazy gcimporter: %v imported, %v more to look up every exported object, %v",
		importTime, lookupTime, importTime+lookupTime)
}
