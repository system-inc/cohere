package build_reads

import (
	"testing"

	"github.com/system-inc/go-types/types"
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
	if !identities[0].Equal(identities[1]) || identities[0].Kind != IdentityMethod {
		test.Errorf("want Second's identity a method's, the same in both builds, got %s and %s", identities[0],
			identities[1])
	}
	if want := "(p.T).Second (b.go)"; identities[0].String() != want {
		test.Errorf("want Second's identity written %s, got %s", want, identities[0])
	}
}

// An unknown identity equals none, itself and another unknown included, while a known one equals itself, and a used
// entry holding an unknown one equals none either.
func TestAnUnknownIdentityEqualsNone(test *testing.T) {
	test.Parallel()

	fileSet, packagesChecked := checkedFixture(test, unknownSources)
	configuration := packagesChecked[0].own.Scope().Lookup("cfg")
	structure, isStruct := configuration.Type().(*types.Struct)
	if !isStruct {
		test.Fatal("want cfg of a struct type")
	}
	renderer := NewIdentityRenderer(fileSet, nil)
	unknown, known := renderer.Object(structure.Field(0)), renderer.Object(configuration)
	if unknown.Kind != IdentityUnknown || known.Kind != IdentityObject {
		test.Fatalf("want cfg's field unknown and cfg known, got %s and %s", unknown, known)
	}
	if unknown.Equal(unknown) || unknown.Equal(renderer.Object(structure.Field(0))) || unknown.Equal(known) ||
		known.Equal(unknown) {
		test.Error("want an unknown identity equal to none")
	}
	if !known.Equal(renderer.Object(configuration)) {
		test.Error("want a known identity equal to itself")
	}
	selected := UsedIdentity{Object: unknown, Index: "0,"}
	if selected.Equal(selected) {
		test.Error("want a used entry holding an unknown identity equal to none")
	}
	if named := (UsedIdentity{Object: known}); !named.Equal(UsedIdentity{Object: known}) {
		test.Error("want a used entry of a known identity equal to itself")
	}
}
