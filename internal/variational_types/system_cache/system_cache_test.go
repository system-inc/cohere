package system_cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/go-types/canonical"
	"github.com/system-inc/go-types/gcimporter"
	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	engine_export_data "github.com/system-inc/cohere/internal/go_engine/export_data"
)

// The packages the store below holds, shaped as the standard library's are: dependency declares constants, a defined
// integer, a struct with methods, one variadic, an interface, a generic type and function, an alias and a variable,
// and user names them across the import, embedding dependency's struct and interface and instantiating its generic
// type, so user's export data names dependency, and a read of user reads dependency first.
const (
	modulePath     = "example.com"
	dependencyPath = modulePath + "/dependency"
	userPath       = modulePath + "/user"

	dependencySource = `package dependency

const Limit = 1 << 10

const Name = "dependency"

type Mode uint32

const (
	ModeRead Mode = 1 << iota
	ModeWrite
)

type Thing struct {
	Size int
	Tags []string
	mode Mode
}

func (Thing) Measure() int { return 0 }

func (thing *Thing) Set(values ...int) { thing.Size = len(values) }

type Reader interface {
	Read(buffer []byte) (int, error)
}

type Pair[Key comparable, Value any] struct {
	Key   Key
	Value Value
}

func Make[Key comparable, Value any](key Key, value Value) Pair[Key, Value] {
	return Pair[Key, Value]{Key: key, Value: value}
}

type Alias = Thing

var Default = Thing{Size: 1}

func Join(separator string, parts ...string) string { return separator }
`
	userSource = `package user

import "example.com/dependency"

type File struct {
	dependency.Thing
	name string
}

type ReadCloser interface {
	dependency.Reader
	Close() error
}

var Value dependency.Thing

var Pairs []dependency.Pair[string, int]

func Open(name string, modes ...dependency.Mode) (*File, error) { return &File{name: name}, nil }

const Size = dependency.Limit * 2
`
)

// sources are the packages' sources by path, dependency first.
var sources = []struct{ path, source string }{{dependencyPath, dependencySource}, {userPath, userSource}}

// importerFunction is a function as a go/types importer, which the engine's check uses.
type importerFunction func(path string) (*gotypes.Package, error)

// Import is the function's answer for path.
func (importer importerFunction) Import(path string) (*gotypes.Package, error) {
	return importer(path)
}

// putWhole keeps entry under key in the store at directory as the engine's store keeps it: the sha256 of entry, then
// entry, in the shard directory of the key's first byte.
func putWhole(test *testing.T, directory string, key cache.Key, entry []byte) {
	test.Helper()
	shard := filepath.Join(directory, key.String()[:2])
	if makeError := os.MkdirAll(shard, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	sum := sha256.Sum256(entry)
	if writeError := os.WriteFile(
		filepath.Join(shard, key.String()),
		append(sum[:], entry...),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
}

// putPackage keeps typed in the store at directory under key as the engine keeps a package: its export data, written
// shallow by the engine's own writer, under the digest of its bytes, and a record of version naming it under key.
func putPackage(test *testing.T, directory string, key cache.Key, fileSet *token.FileSet, typed *gotypes.Package,
	version string) {
	test.Helper()
	written, writeError := engine_export_data.Write(fileSet, typed)
	if writeError != nil {
		test.Fatalf("writing the export data of %s: %v", typed.Path(), writeError)
	}
	digest := sha256.Sum256(written)
	putWhole(test, directory, exportDataKey(digest[:]), written)
	var record bytes.Buffer
	if encodeError := gob.NewEncoder(&record).Encode(entryRecord{Version: version,
		ExportData: digest[:]}); encodeError != nil {
		test.Fatal(encodeError)
	}
	putWhole(test, directory, key, record.Bytes())
}

// keyOfPath is a package's key in the store below, made of its path alone.
func keyOfPath(path string) cache.Key {
	return sha256.Sum256([]byte("key of " + path))
}

// storeOfSources is a store in a directory of the test's holding every package of sources, checked from source with
// go/types as the engine checks one, and the key it holds each under, by path.
func storeOfSources(test *testing.T) (Store, map[string]cache.Key) {
	test.Helper()
	fileSet := token.NewFileSet()
	checked := map[string]*gotypes.Package{}
	importer := importerFunction(func(path string) (*gotypes.Package, error) {
		if typed, held := checked[path]; held {
			return typed, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})
	store := Store{Directory: test.TempDir()}
	keys := map[string]cache.Key{}
	for _, each := range sources {
		parsed, parseError := parser.ParseFile(fileSet, filepath.Base(each.path)+".go", each.source,
			parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", each.path, parseError)
		}
		typed, checkError := (&gotypes.Config{Importer: importer}).Check(each.path, fileSet, []*ast.File{parsed}, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s: %v", each.path, checkError)
		}
		checked[each.path] = typed
		keys[each.path] = keyOfPath(each.path)
		putPackage(test, store.Directory, keys[each.path], fileSet, typed, check.CacheFormatVersion)
	}
	return store, keys
}

// keysOf is keys as a build's KeyOf.
func keysOf(keys map[string]cache.Key) func(path string) (cache.Key, bool) {
	return func(path string) (cache.Key, bool) {
		key, keyed := keys[path]
		return key, keyed
	}
}

// A read of user reads dependency first, into the same packages, so user's Value is of dependency's own Thing: one
// universe. A second read reads nothing again.
func TestABuildReadsEachPackageItNamesFirst(test *testing.T) {
	test.Parallel()

	store, keys := storeOfSources(test)
	build := NewBuild(store, keysOf(keys), token.NewFileSet(), map[string]*types.Package{})
	present, readError := build.Read(userPath)
	if readError != nil || !present {
		test.Fatalf("want %s read, got present %t, %v", userPath, present, readError)
	}
	dependency, user := build.Packages[dependencyPath], build.Packages[userPath]
	if dependency == nil || user == nil {
		test.Fatalf("want both packages read, got %v", build.Packages)
	}
	valueType, isNamed := user.Scope().Lookup("Value").Type().(*types.Named)
	if !isNamed || valueType.Obj() != dependency.Scope().Lookup("Thing") {
		test.Errorf("want Value of dependency's own Thing, got %v", user.Scope().Lookup("Value").Type())
	}
	if again, againError := build.Read(userPath); !again || againError != nil || build.Packages[userPath] != user {
		test.Errorf("want a second read to keep the first's package, got present %t, %v", again, againError)
	}
	if unsafePresent, unsafeError := build.Read("unsafe"); !unsafePresent || unsafeError != nil ||
		build.Packages["unsafe"] != types.Unsafe {
		test.Errorf("want unsafe the fork's own, got present %t, %v", unsafePresent, unsafeError)
	}
}

// A package the build keys nothing under, a package whose dependency it keys nothing under, a record in another
// format, and a record whose file is not whole are each absent, with no error and nothing gained. The file that is
// not whole is left where it is: the store is never written.
func TestABuildReadsAPackageItDoesNotHaveAsAbsent(test *testing.T) {
	test.Parallel()

	store, keys := storeOfSources(test)
	absent := func(name string, keyOf func(path string) (cache.Key, bool)) {
		test.Helper()
		build := NewBuild(store, keyOf, token.NewFileSet(), map[string]*types.Package{})
		present, readError := build.Read(userPath)
		if present || readError != nil {
			test.Errorf("%s: want %s absent, got present %t, %v", name, userPath, present, readError)
		}
		if _, held := build.Packages[userPath]; held {
			test.Errorf("%s: want %s left out of the packages", name, userPath)
		}
	}
	absent("unkeyed", keysOf(map[string]cache.Key{}))
	absent("its dependency unkeyed", keysOf(map[string]cache.Key{userPath: keys[userPath]}))

	otherFormat := keyOfPath("another format")
	putPackage(test, store.Directory, otherFormat, token.NewFileSet(), gotypes.NewPackage("example.com/other",
		"other"), "go-engine-cache-0")
	absent("another format", keysOf(map[string]cache.Key{userPath: otherFormat,
		dependencyPath: keys[dependencyPath]}))

	cut := filepath.Join(store.Directory, keys[userPath].String()[:2], keys[userPath].String())
	contents, readError := os.ReadFile(cut)
	if readError != nil {
		test.Fatal(readError)
	}
	if writeError := os.WriteFile(cut, contents[:len(contents)-1], 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	absent("not whole", keysOf(keys))
	if _, statError := os.Stat(cut); statError != nil {
		test.Errorf("want the file that is not whole left in the store, got %v", statError)
	}
}

// compiledExports is the compiler's export data of every package of sources, by path, as go list compiles a module
// the test lays out in a directory of its own, with a build cache of its own and no go env file, workspace, flags or
// network: the sources import nothing outside the module, so nothing else is compiled.
func compiledExports(test *testing.T) map[string]string {
	test.Helper()
	module := test.TempDir()
	files := map[string]string{"go.mod": "module " + modulePath + "\n\ngo 1.27\n"}
	for _, each := range sources {
		relative := strings.TrimPrefix(each.path, modulePath+"/")
		files[filepath.Join(relative, relative+".go")] = each.source
	}
	for name, contents := range files {
		if makeError := os.MkdirAll(filepath.Dir(filepath.Join(module, name)), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(filepath.Join(module, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	command := exec.Command("go", "list", "-export", "-deps", "-json=ImportPath,Export", "./...")
	command.Dir = module
	command.Env = append(withoutVariables(os.Environ(), "GOWORK", "GOFLAGS", "GOENV", "GOCACHE", "GOOS", "GOARCH",
		"CGO_ENABLED", "GOTOOLCHAIN", "GOPROXY"), "GOWORK=off", "GOFLAGS=", "GOENV=off",
		"GOCACHE="+test.TempDir(), "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOPROXY=off")
	output, listError := command.Output()
	if listError != nil {
		var exitError *exec.ExitError
		if errors.As(listError, &exitError) {
			test.Fatalf("go list: %v\n%s", listError, exitError.Stderr)
		}
		test.Fatalf("go list: %v", listError)
	}
	exports := map[string]string{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var listed struct{ ImportPath, Export string }
		if decodeError := decoder.Decode(&listed); errors.Is(decodeError, io.EOF) {
			return exports
		} else if decodeError != nil {
			test.Fatal(decodeError)
		}
		exports[listed.ImportPath] = listed.Export
	}
}

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

// Each package read from the store, as the engine's writer wrote it, holds every exported package-level object as the
// compiler's export data of the same source does, read through the fork's gcimporter: the same names, each of the
// same kind, type, value, underlying type and methods, spelled canonically.
func TestAStoredPackageReadsAsTheCompilersExportData(test *testing.T) {
	test.Parallel()

	store, keys := storeOfSources(test)
	exports := compiledExports(test)
	cached := NewBuild(store, keysOf(keys), token.NewFileSet(), map[string]*types.Package{})
	universe := gcimporter.NewUniverse()
	fileSet := token.NewFileSet()
	for _, each := range sources {
		present, readError := cached.Read(each.path)
		if readError != nil || !present {
			test.Fatalf("want %s read from the store, got present %t, %v", each.path, present, readError)
		}
		compiled, compiledError := gcimporter.Import(fileSet, universe, each.path, "",
			func(asked string) (io.ReadCloser, error) {
				file, held := exports[asked]
				if !held || file == "" {
					return nil, fmt.Errorf("no export data for %q", asked)
				}
				return os.Open(file)
			})
		if compiledError != nil {
			test.Fatalf("reading the compiler's export data of %s: %v", each.path, compiledError)
		}
		fromStore, fromCompiler := spelledObjects(cached.Packages[each.path]), spelledObjects(compiled)
		for name, spelled := range fromCompiler {
			if fromStore[name] != spelled {
				test.Errorf("%s.%s: the store has %q, the compiler %q", each.path, name, fromStore[name], spelled)
			}
		}
		for name, spelled := range fromStore {
			if _, compiledHas := fromCompiler[name]; !compiledHas {
				test.Errorf("%s.%s: the store has %q, the compiler nothing", each.path, name, spelled)
			}
		}
		if len(fromCompiler) == 0 {
			test.Errorf("%s: no exported object compared", each.path)
		}
	}
}
