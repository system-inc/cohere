package build_dependence

import (
	"go/token"
	"go/types"
	"testing"
)

// IdentifiedObjects fails wide where it cannot name an object to one package (#w72zxtq): an object of a package
// neither the graph holds nor the identifiers name answers both bits set, and so does an object of a held package no
// unit declares whose type reads such an object, though no bit of the build is set. The methods fixture's graph is
// judged against a build that changes nothing, so every bit a known object reads is unset: a local of an int, or of
// the fixture's own Count, answers none.
func TestAnObjectIdentifiedObjectsCannotNameAnswersBothBits(test *testing.T) {
	test.Parallel()

	host := loadFixture(test, "methods", hostBuild)
	held, isHeld := host["example.com/methods"]
	if !isHeld {
		test.Fatal("the methods fixture has no package example.com/methods")
	}
	identified := NewIdentifiedObjects(graphOf(host, nil).Against(Difference{}),
		map[*types.Package]string{held.Types: held.ID})
	elsewhere := types.NewPackage("example.com/elsewhere", "elsewhere")
	thing := types.NewTypeName(token.NoPos, elsewhere, "Thing", nil)
	types.NewNamed(thing, types.Typ[types.Int], nil)
	count := held.Types.Scope().Lookup("Count")
	if count == nil {
		test.Fatal("the methods fixture declares no Count")
	}
	for _, each := range []struct {
		name   string
		object types.Object
		want   Bits
	}{
		{"a type of a package neither held nor identified", thing, Bits{Name: true, Underlying: true}},
		{"a held package's local of that type", types.NewVar(token.NoPos, held.Types, "local", thing.Type()),
			Bits{Name: true, Underlying: true}},
		{"a held package's local of an int", types.NewVar(token.NoPos, held.Types, "local", types.Typ[types.Int]),
			Bits{}},
		{"a held package's local of its own Count", types.NewVar(token.NoPos, held.Types, "local", count.Type()),
			Bits{}},
	} {
		if got := identified.Bits(each.object); got != each.want {
			test.Errorf("%s answers %+v, want %+v", each.name, got, each.want)
		}
	}
}
