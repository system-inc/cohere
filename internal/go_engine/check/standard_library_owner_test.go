package check

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// A read of a standard-library package's types is named by that package's own key, never by another's under the same
// path: a run over the standard library itself loads errors beside its in-package test variant, both with the path
// errors, and the standard library's keys hold both, each under its own key. A read of either one's types names that
// one, as the session finds it by path and then by types (standardLibraryOwnerOf).
func TestAReadOfATestVariantIsNamedByTheVariantsOwnKey(test *testing.T) {
	test.Parallel()

	goRoot := test.TempDir()
	files := map[string]string{
		"src/errors/errors.go":      "package errors\n",
		"src/errors/errors_test.go": "package errors\n",
	}
	for relative, contents := range files {
		path := filepath.Join(goRoot, filepath.FromSlash(relative))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	source := filepath.Join(goRoot, "src", "errors", "errors.go")
	testSource := filepath.Join(goRoot, "src", "errors", "errors_test.go")
	plain := &packages.Package{ID: "errors", PkgPath: "errors", Name: "errors", GoFiles: []string{source},
		CompiledGoFiles: []string{source}, Imports: map[string]*packages.Package{},
		Types: types.NewPackage("errors", "errors")}
	variant := &packages.Package{ID: "errors [errors.test]", PkgPath: "errors", Name: "errors",
		GoFiles: []string{source, testSource}, CompiledGoFiles: []string{source, testSource},
		Imports: map[string]*packages.Package{}, Types: types.NewPackage("errors", "errors")}
	keys, keysError := cache.StandardLibraryKeys([]*packages.Package{plain, variant},
		standardLibraryContext("check tests", goRoot), cache.FileDigests{})
	if keysError != nil {
		test.Fatal(keysError)
	}
	if _, keyed := keys[variant]; !keyed || keys[plain] == keys[variant] {
		test.Fatalf("want the variant keyed apart from the plain package, got %v", keys)
	}
	session := NewCacheSession(&Caching{}, nil)
	session.standardLibraryKeys = keys
	session.standardLibraryByPath = standardLibraryPackagesByPath(keys)
	for _, read := range []*packages.Package{plain, variant} {
		owner, key, keyed := session.ownerKeyOf(read.Types)
		if !keyed || owner != read || key != keys[read] {
			named := "nothing"
			if owner != nil {
				named = owner.ID
			}
			test.Errorf("a read of %s's types is named by %s under %s (named %t), want %s's own key %s", read.ID,
				named, key, keyed, read.ID, keys[read])
		}
	}
}
