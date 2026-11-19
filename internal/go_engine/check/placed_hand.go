package check

import (
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// placedHand is the hand a build puts on its resolver's answers (fact_resolver.Held, HoldAnswers) to answer a question
// about a function of a package it checks from source as the host's run answered it (#k7k3wwc): from the placed
// answers the host's entry of the package holds (HostRun.placedOf, storedPlacedAt), by the function's place in the
// build's own syntax, for each function a caller names as standing, whose answers still hold in the build. A question
// it holds no answer for, about a function not standing, one the host's run never answered, or an object of a package
// it was not handed, it leaves to the resolver, which computes it as it would with no hand. It is filled before the
// run asks anything and only read while it runs, from every pass at once.
type placedHand struct {
	packages map[*types.Package]placedPackage
}

// placedPackage is one package as placedHand answers of it: the place of each function it declares and each one's
// parameters, by object, in the build's syntax, and the answers of the functions standing, by kind and place.
type placedPackage struct {
	places  map[types.Object]fact_resolver.Place
	answers map[placedAt][]byte
}

// newPlacedHand is a hand handed no package, which answers nothing.
func newPlacedHand() *placedHand {
	return &placedHand{packages: map[*types.Package]placedPackage{}}
}

// hold has the hand answer of held, a package the build loaded from source, its syntax and types its own, with the
// answers of placed, the host's entry's of it, about each function standing names by its place, Parameter -1, and its
// parameters. standing is the caller's word on which of held's functions the host's answers still hold for; until a
// build hands one, only a test does.
func (hand *placedHand) hold(
	held *packages.Package,
	placed []fact_resolver.PlacedAnswer,
	standing func(function fact_resolver.Place) bool,
) {
	answers := map[placedAt][]byte{}
	for _, answer := range placed {
		if !standing(fact_resolver.Place{File: answer.File, Offset: answer.Offset, Parameter: -1}) {
			continue
		}
		answers[placedAt{kind: answer.Kind, file: answer.File, offset: answer.Offset,
			parameter: answer.Parameter}] = answer.Encoded
	}
	hand.packages[held.Types] = placedPackage{
		places:  fact_resolver.DeclarationPlaces(held.Fset, held.Syntax, held.TypesInfo),
		answers: answers,
	}
}

// Answer is the host's answer of the kind named kindName about object, when the hand holds one at object's place.
func (hand *placedHand) Answer(kindName string, object types.Object) ([]byte, bool) {
	held, isHeld := hand.packages[object.Pkg()]
	if !isHeld {
		return nil, false
	}
	place, isPlaced := held.places[object]
	if !isPlaced {
		return nil, false
	}
	encoded, answered := held.answers[placedAt{kind: kindName, file: place.File, offset: place.Offset,
		parameter: place.Parameter}]
	return encoded, answered
}

// Computed hears an answer the resolver computed, which the hand, answering only from the host's, has no use for.
func (*placedHand) Computed(string, types.Object, []byte) {}
