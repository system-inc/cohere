package system_cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/variational_types/system_cache/export_data"
)

// The packages the store below holds: user's API names dependency's Thing, so user's export data names dependency, and
// a read of user reads dependency first.
const (
	dependencyPath = "example.com/dependency"
	userPath       = "example.com/user"

	dependencySource = "package dependency\n\ntype Thing struct{ Size int }\n\nfunc (Thing) Measure() int { return 0 }\n"
	userSource       = "package user\n\nimport \"example.com/dependency\"\n\nvar Value dependency.Thing\n\n" +
		"func Make() dependency.Thing { return dependency.Thing{} }\n"
)

// importerFunction is a function as a types.Importer.
type importerFunction func(path string) (*types.Package, error)

// Import is the function's answer for path.
func (importer importerFunction) Import(path string) (*types.Package, error) {
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
// shallow, under the digest of its bytes, and a record of version naming it under key.
func putPackage(test *testing.T, directory string, key cache.Key, fileSet *token.FileSet, typed *types.Package,
	version string) {
	test.Helper()
	written, writeError := export_data.Write(fileSet, typed)
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

// storeOfTwo is a store in a directory of the test's holding dependency and user, checked from source, and every key
// it holds them under, by path.
func storeOfTwo(test *testing.T) (Store, map[string]cache.Key) {
	test.Helper()
	fileSet := token.NewFileSet()
	checked := func(path string, source string, importer types.Importer) *types.Package {
		parsed, parseError := parser.ParseFile(fileSet, path+".go", source, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", path, parseError)
		}
		typed, checkError := (&types.Config{Importer: importer}).Check(path, fileSet, []*ast.File{parsed}, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s: %v", path, checkError)
		}
		return typed
	}
	dependency := checked(dependencyPath, dependencySource, nil)
	user := checked(userPath, userSource, importerFunction(func(path string) (*types.Package, error) {
		if path != dependencyPath {
			return nil, fmt.Errorf("no package %s", path)
		}
		return dependency, nil
	}))
	store := Store{Directory: test.TempDir()}
	keys := map[string]cache.Key{dependencyPath: keyOfPath(dependencyPath), userPath: keyOfPath(userPath)}
	putPackage(test, store.Directory, keys[dependencyPath], fileSet, dependency, check.CacheFormatVersion)
	putPackage(test, store.Directory, keys[userPath], fileSet, user, check.CacheFormatVersion)
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

	store, keys := storeOfTwo(test)
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

	store, keys := storeOfTwo(test)
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
	fileSet := token.NewFileSet()
	putPackage(test, store.Directory, otherFormat, fileSet, types.NewPackage("example.com/other", "other"),
		"go-engine-cache-0")
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
