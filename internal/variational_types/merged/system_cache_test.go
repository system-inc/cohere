package merged_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	engine_export_data "github.com/system-inc/cohere/internal/go_engine/export_data"
	"github.com/system-inc/cohere/internal/variational_types/merged"
	"github.com/system-inc/cohere/internal/variational_types/system_cache"
)

// The packages each build's store holds: dependency alike in both, and user, which names it, declaring N and File
// as each build has them.
const (
	cachedDependencyPath = "example.com/dependency"
	cachedUserPath       = "example.com/user"

	cachedDependencySource = `package dependency

type Thing struct{ Size int }

func (Thing) Measure() int { return 0 }
`
	cachedDarwinUserSource = `package user

import "example.com/dependency"

const N = 1

type File struct {
	dependency.Thing
	fd int
}

var Value dependency.Thing
`
	cachedLinuxUserSource = `package user

import "example.com/dependency"

const N = 2

type File struct {
	dependency.Thing
	handle uintptr
	name   string
}

var Value dependency.Thing
`
)

// cachedEntryRecord is the record the engine's store keeps under a package's key, as far as the system cache reads
// it: the format it was written in and the sha256 of the package's export data.
type cachedEntryRecord struct {
	Version    string
	ExportData []byte
}

// putCachedEntry keeps entry under key in the store at directory as the engine's store keeps it: the sha256 of entry,
// then entry, in the shard directory of the key's first byte.
func putCachedEntry(test *testing.T, directory string, key cache.Key, entry []byte) {
	test.Helper()
	shard := filepath.Join(directory, key.String()[:2])
	if makeError := os.MkdirAll(shard, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	sum := sha256.Sum256(entry)
	if writeError := os.WriteFile(filepath.Join(shard, key.String()), append(sum[:], entry...),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
}

// putCachedPackage keeps typed in the store at directory under key as the engine keeps a package: its shallow export
// data, written by the engine's own writer, under the digest of its bytes, and a record naming it under key.
func putCachedPackage(test *testing.T, directory string, key cache.Key, fileSet *token.FileSet,
	typed *gotypes.Package) {
	test.Helper()
	written, writeError := engine_export_data.Write(fileSet, typed)
	if writeError != nil {
		test.Fatalf("writing the export data of %s: %v", typed.Path(), writeError)
	}
	digest := sha256.Sum256(written)
	putCachedEntry(test, directory, sha256.Sum256(append([]byte("export data by content "), digest[:]...)), written)
	var record bytes.Buffer
	if encodeError := gob.NewEncoder(&record).Encode(cachedEntryRecord{Version: check.CacheFormatVersion,
		ExportData: digest[:]}); encodeError != nil {
		test.Fatal(encodeError)
	}
	putCachedEntry(test, directory, key, record.Bytes())
}

// goTypesImporter is a function as a go/types importer.
type goTypesImporter func(path string) (*gotypes.Package, error)

func (importer goTypesImporter) Import(path string) (*gotypes.Package, error) { return importer(path) }

// cachedBuild writes a store holding dependency and user, from userSource, each checked from source with go/types as
// the engine checks one, and is a system cache build over it, its keys made of the build's name and the path.
func cachedBuild(test *testing.T, buildName string, userSource string, fileSet *token.FileSet) *system_cache.Build {
	test.Helper()
	directory := test.TempDir()
	keys := map[string]cache.Key{}
	checked := map[string]*gotypes.Package{}
	importer := goTypesImporter(func(path string) (*gotypes.Package, error) {
		if typed, held := checked[path]; held {
			return typed, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})
	for _, each := range []struct{ path, source string }{
		{cachedDependencyPath, cachedDependencySource},
		{cachedUserPath, userSource},
	} {
		parsed, parseError := parser.ParseFile(fileSet, buildName+"/"+filepath.Base(each.path)+".go", each.source,
			parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", each.path, parseError)
		}
		typed, checkError := (&gotypes.Config{Importer: importer}).Check(each.path, fileSet, []*ast.File{parsed}, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s: %v", each.path, checkError)
		}
		checked[each.path] = typed
		keys[each.path] = sha256.Sum256([]byte("key of " + buildName + " " + each.path))
		putCachedPackage(test, directory, keys[each.path], fileSet, typed)
	}
	keyOf := func(path string) (cache.Key, bool) {
		key, keyed := keys[path]
		return key, keyed
	}
	return system_cache.NewBuild(system_cache.Store{Directory: directory}, keyOf, fileSet, map[string]*types.Package{})
}

// Not parallel: the fork holds a variational check's unit state in package variables, which a check writes and
// every merge and lookup reads.
func TestMergedImporterMergesBuildsReadFromTheSystemCache(test *testing.T) {
	fileSet := token.NewFileSet()
	sources := []merged.Source{
		merged.SystemCache(cachedBuild(test, "darwin", cachedDarwinUserSource, fileSet)),
		merged.SystemCache(cachedBuild(test, "linux", cachedLinuxUserSource, fileSet)),
	}
	importer := merged.New(fileSet, sources, buildDarwin|buildLinux)
	userPackage, importError := importer.Import(cachedUserPath)
	if importError != nil {
		test.Fatal(importError)
	}
	values := map[types.Mask]string{}
	for _, variant := range types.VariantsOf(userPackage.Scope().Lookup("N")) {
		values[variant.Builds] = variant.Value.ExactString()
	}
	if values[buildDarwin] != "1" || values[buildLinux] != "2" || len(values) != 2 {
		test.Errorf("user.N answers %v by build, and wants 1 on darwin and 2 on linux", values)
	}
	file := lookupNamed(test, userPackage, "File")
	if variants := types.UnderlyingVariantsOf(file); len(variants) != 2 {
		test.Errorf("user.File has underlying variants %v, and its fields differ by build", variants)
	}
	value := userPackage.Scope().Lookup("Value")
	if value == nil {
		test.Fatal("user.Value looked up nil")
	}
	thing, isNamed := value.Type().(*types.Named)
	if !isNamed || thing.Obj().Pkg().Path() != cachedDependencyPath || thing.Obj().Name() != "Thing" {
		test.Fatalf("user.Value is of %v, and wants dependency.Thing", value.Type())
	}
	if types.VariantsOf(value) != nil {
		test.Errorf("user.Value's type differs by build, %v, and both builds read it as the one dependency.Thing",
			types.VariantsOf(value))
	}
	dependencyPackage, dependencyError := importer.Import(cachedDependencyPath)
	if dependencyError != nil {
		test.Fatal(dependencyError)
	}
	if lookupNamed(test, dependencyPackage, "Thing") != thing {
		test.Error("dependency.Thing is two named types, one reached from user and one looked up")
	}
	if thing.NumMethods() != 1 || thing.Method(0).Name() != "Measure" {
		test.Errorf("dependency.Thing has %d methods, and wants Measure", thing.NumMethods())
	}
	if len(importer.Failures()) > 0 {
		test.Errorf("the merge met failures reading the system cache: %v", importer.Failures())
	}
}
