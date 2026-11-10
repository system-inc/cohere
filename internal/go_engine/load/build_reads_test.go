package load

import (
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// readsOf is the reads loaded holds of the package whose import path is path, failing the test when it holds none.
func readsOf(test *testing.T, loaded *Loaded, path string) *build_reads.Reads {
	test.Helper()

	for recorded, reads := range loaded.Reads {
		if recorded.PkgPath == path && recorded.ForTest == "" {
			return reads
		}
	}
	test.Fatalf("the load holds no reads of %s", path)
	return nil
}

// A dependency checked for its types alone, with no type maps, has nothing to record its reads from, so it can never
// be restored; a root of the same load, and the same dependency checked whole, can.
func TestADependencyCheckedForItsTypesAloneIsNeverRestored(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeListingFiles(test, directory, rootSyntaxFiles)
	lean, leanError := loadWorkspace(directory, []string{"./root"}, []string{"GOWORK=off"}, nil, true,
		Caching{RecordReads: true, rootSyntaxOnly: true}, nil)
	whole, wholeError := loadWorkspace(directory, []string{"./root"}, []string{"GOWORK=off"}, nil, true,
		Caching{RecordReads: true}, nil)
	if leanError != nil || wholeError != nil {
		test.Fatalf("loading the fixture: %v, %v", leanError, wholeError)
	}
	// rootSyntaxFiles' one root, and the dependency it imports.
	packagesOf := func(loaded *Loaded) (*packages.Package, *packages.Package) {
		if len(loaded.Roots) != 1 {
			test.Fatalf("want the one root, got %d", len(loaded.Roots))
		}
		return loaded.Roots[0], loaded.Roots[0].Imports["example.com/rootsyntax/dependency"]
	}
	root, dependency := packagesOf(lean)
	if never := lean.Reads[dependency].Never(); never != "checked for its types only" {
		test.Fatalf("want the types-only dependency marked never, got %q", never)
	}
	if never := lean.Reads[root].Never(); never != "" {
		test.Fatalf("the root is marked never: %s", never)
	}
	_, wholeDependency := packagesOf(whole)
	if never := whole.Reads[wholeDependency].Never(); never != "" {
		test.Fatalf("the dependency checked whole is marked never: %s", never)
	}
}

// A package whose types import runtime/cgo is one vet's cgocall type-checks again from its raw files, and a package
// with cgo files, runtime/cgo itself here, is checked again the same way, neither check recorded, so neither can ever
// be restored.
func TestAPackageWithCgoIsNeverRestored(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeListingFiles(test, directory, map[string]string{
		"go.mod":       "module example.com/cgoreads\n\ngo 1.21\n",
		"user/user.go": "package user\n\nimport _ \"runtime/cgo\"\n",
	})
	loaded, loadError := PackagesCached(directory, []string{"./user"}, cgoEnvironment, nil, Caching{RecordReads: true})
	if loadError != nil {
		test.Fatal(loadError)
	}
	var runtimeCgo *packages.Package
	packages.Visit(loaded.Roots, nil, func(visited *packages.Package) {
		if visited.PkgPath == "runtime/cgo" {
			runtimeCgo = visited
		}
	})
	if runtimeCgo == nil {
		test.Fatal("the load reached no runtime/cgo to check")
	}
	want := "it imports runtime/cgo, so vet's cgocall type-checks its cgo files again, unrecorded"
	if never := readsOf(test, loaded, "example.com/cgoreads/user").Never(); never != want {
		test.Fatalf("want user marked never\n%s\ngot\n%s", want, never)
	}
	want = "it has cgo files, which vet's cgocall type-checks again, unrecorded"
	if never := loaded.Reads[runtimeCgo].Never(); never != want {
		test.Fatalf("want runtime/cgo marked never\n%s\ngot\n%s", want, never)
	}
}
