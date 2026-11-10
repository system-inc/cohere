package load

import (
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// buildReadsFiles are a module whose package user reads another package, base, every way the type check records: a
// call of os.Getpid, a field of base reached through a field base embeds and user never names, so only the selection's
// path holds it, and an instance of base's generic. user's own struct and its field are its own, never a read.
var buildReadsFiles = map[string]string{
	"go.mod": "module example.com/reads\n\ngo 1.21\n",
	"base/base.go": "package base\n\ntype inner struct{ Value int }\n\n// Outer embeds inner.\n" +
		"type Outer struct{ inner }\n\n// Identity is its argument.\nfunc Identity[Type any](value Type) Type {\n" +
		"\treturn value\n}\n",
	"user/user.go": "package user\n\nimport (\n\t\"os\"\n\n\t\"example.com/reads/base\"\n)\n\n" +
		"type own struct{ Count int }\n\n// Run reads base and os.\nfunc Run(outer base.Outer) int {\n" +
		"\tvar mine own\n\treturn os.Getpid() + outer.Value + mine.Count + base.Identity(1)\n}\n",
}

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

// objectNames are objects as their package's path and their name, in order.
func objectNames(objects []types.Object) []string {
	names := make([]string, 0, len(objects))
	for _, object := range objects {
		names = append(names, object.Pkg().Path()+"."+object.Name())
	}
	return names
}

// A package's check records every object of another package it reached, the field it reached only through a
// selection's path among them, and the type arguments of the instance of another package's generic, and never an
// object of its own or of the universe.
func TestAPackagesTypeReadsAreEveryForeignObjectItReached(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeListingFiles(test, directory, buildReadsFiles)
	loaded, loadError := Packages(directory, []string{"./user"}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	if len(loaded.Diagnostics) > 0 {
		test.Fatalf("the fixture loaded with diagnostics: %+v", loaded.Diagnostics)
	}
	reads := readsOf(test, loaded, "example.com/reads/user")
	if never := reads.Never(); never != "" {
		test.Fatalf("user is marked never: %s", never)
	}
	want := []string{
		"example.com/reads/base.Identity",
		"example.com/reads/base.Outer",
		"example.com/reads/base.Value",
		"example.com/reads/base.inner",
		"os.Getpid",
	}
	if got := objectNames(reads.Objects()); !slices.Equal(got, want) {
		test.Fatalf("want user's reads\n%q\ngot\n%q", want, got)
	}
	instances := reads.Instances()
	if len(instances) != 1 || instances[0].Generic.Name() != "Identity" || instances[0].TypeArguments.Len() != 1 ||
		instances[0].TypeArguments.At(0) != types.Typ[types.Int] {
		test.Fatalf("want the one instance base.Identity[int], got %+v", instances)
	}
}

// A dependency checked for its types alone, with no type maps, has nothing to record its reads from, so it can never
// be restored; a root of the same load, and the same dependency checked whole, can.
func TestADependencyCheckedForItsTypesAloneIsNeverRestored(test *testing.T) {
	test.Parallel()

	lean, whole, packagesOf := rootSyntaxLoads(test)
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
	loaded, loadError := Packages(directory, []string{"./user"}, cgoEnvironment, nil)
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
