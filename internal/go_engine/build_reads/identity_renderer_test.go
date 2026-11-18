package build_reads

import (
	"go/types"
	"testing"

	"golang.org/x/tools/go/types/objectpath"
)

// methodBuilds are two builds of one package: b.go declares T's method Second alike in both, and the second build's
// a.go declares a method Zero before First, which moves Second one place on among T's methods.
var methodBuilds = [][]fixtureFile{
	{
		{"p", "a.go", "package p\n\ntype T struct{}\n\nfunc (T) First() {}\n"},
		{"p", "b.go", "package p\n\nfunc (T) Second() {}\n"},
	},
	{
		{"p", "a.go", "package p\n\ntype T struct{}\n\nfunc (T) Zero() {}\n\nfunc (T) First() {}\n"},
		{"p", "b.go", "package p\n\nfunc (T) Second() {}\n"},
	},
}

// A method's identity is its type's and its own name, so a method declared alike in a file alike is the same in two
// builds whose other files move it among its type's methods, which its objectpath's index follows.
func TestAMethodsIdentityHoldsWhereItsIndexShifts(test *testing.T) {
	test.Parallel()

	var identities []Identity
	var paths []objectpath.Path
	for _, build := range methodBuilds {
		fileSet, packagesChecked := checkedFixture(test, build)
		named, isNamed := packagesChecked[0].own.Scope().Lookup("T").Type().(*types.Named)
		if !isNamed {
			test.Fatal("want T a named type")
		}
		var second types.Object
		for index := range named.NumMethods() {
			if named.Method(index).Name() == "Second" {
				second = named.Method(index)
			}
		}
		if second == nil {
			test.Fatal("want T's method Second")
		}
		path, pathError := (&objectpath.Encoder{}).For(second)
		if pathError != nil {
			test.Fatal(pathError)
		}
		paths = append(paths, path)
		identities = append(identities, NewIdentityRenderer(fileSet, nil).Object(second))
	}
	if paths[0] == paths[1] {
		test.Fatalf("want Second's objectpath to differ between the builds, got %s in both", paths[0])
	}
	if identities[0] != identities[1] || identities[0].Kind != IdentityMethod {
		test.Errorf("want Second's identity a method's, the same in both builds, got %s and %s", identities[0],
			identities[1])
	}
	if want := "(p.T).Second (b.go)"; identities[0].String() != want {
		test.Errorf("want Second's identity written %s, got %s", want, identities[0])
	}
}
