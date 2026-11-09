package intermediate_representation_test

// An instance's name is its type arguments unaliased (unalias.go, #6pt3bt8).

import (
	"go/types"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// unaliasSource declares, for each type form, a variable spelled through PP, an alias of an alias of Point, and
// its twin spelled with Point itself.
const unaliasSource = `package aliases

type Point struct{ X int }

type P = Point

type PP = P

type Box[T any] struct{ value T }

func Use[T any](value T) {}

var (
	aliasedPointer   *PP
	plainPointer     *Point
	aliasedSlice     []PP
	plainSlice       []Point
	aliasedArray     [2]PP
	plainArray       [2]Point
	aliasedMap       map[PP]PP
	plainMap         map[Point]Point
	aliasedChannel   chan<- PP
	plainChannel     chan<- Point
	aliasedSignature func(PP, ...PP) (PP, error)
	plainSignature   func(Point, ...Point) (Point, error)
	aliasedStruct    struct{ field PP "tag" }
	plainStruct      struct{ field Point "tag" }
	aliasedNamed     Box[*PP]
	plainNamed       Box[*Point]
	aliasedAlias     PP
	plainAlias       Point
)

func first() { Use[*PP](nil) }

func second() { Use[*Point](nil) }
`

// TestUnaliasDeeplyReachesEveryForm holds each type form spelled through the alias of an alias to its twin's
// spelling once unaliased.
func TestUnaliasDeeplyReachesEveryForm(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "aliases", unaliasSource)
	scope := unit.types.Scope()
	for _, form := range []string{"Pointer", "Slice", "Array", "Map", "Channel", "Signature", "Struct", "Named",
		"Alias"} {
		aliased := scope.Lookup("aliased" + form).Type()
		plain := scope.Lookup("plain" + form).Type()
		if types.TypeString(aliased, nil) == types.TypeString(plain, nil) {
			test.Errorf("%s: the aliased spelling prints as the plain one, %s, so the case checks nothing", form,
				types.TypeString(aliased, nil))
		}
		got := types.TypeString(intermediate_representation.UnaliasDeeplyForTest(aliased), nil)
		if want := types.TypeString(plain, nil); got != want {
			test.Errorf("%s: %s unaliases to %s, want %s", form, types.TypeString(aliased, nil), got, want)
		}
	}
}

// TestAnInstanceIsNamedTheSameInEitherOrder builds first, which calls Use through the alias, and second, which
// calls it plainly, in both orders, and holds the one instance they share to one name.
func TestAnInstanceIsNamedTheSameInEitherOrder(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "aliases", unaliasSource)
	for _, order := range [][]string{{"first", "second"}, {"second", "first"}} {
		built := differentialCreateOurs(unit)
		for _, name := range order {
			built.Function(name).Build()
		}
		for _, name := range order {
			callee := ""
			for _, call := range callsOf(built.Function(name)) {
				callee = call.StaticCallee().RelativeString(nil)
			}
			if want := "aliases.Use[*aliases.Point]"; callee != want {
				test.Errorf("built %v, %s calls %s, want %s", order, name, callee, want)
			}
		}
	}
}
