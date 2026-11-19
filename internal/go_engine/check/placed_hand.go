package check

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// placedHand is the hand a build puts on its resolver's answers (fact_resolver.Held, HoldAnswers) to answer a question
// about a function of a package it checks from source as the host's run answered it (#k7k3wwc): from the placed
// answers the host's entry of the package holds (HostRun.placedOf, storedPlacedAt), by the function's place in the
// build's own syntax, for each function a caller names as standing, whose answers still hold in the build, and only
// where the build parsed the very bytes the host did: an answer is found by its file's digest as well as its place,
// so a file whose bytes differ, where an offset that still lines up may name another function, as a cgo file
// generated for another build can, answers nothing. A question it holds no answer for, about a function not standing,
// one the host's run never answered, one in a file either side holds no digest of, or an object of a package it was
// not handed, it leaves to the resolver, which computes it as it would with no hand. It is filled before the
// run asks anything and only read while it runs, from every pass at once.
type placedHand struct {
	packages map[*types.Package]placedPackage
}

// placedPackage is one package as placedHand answers of it: the place of each function it declares and each one's
// parameters, by object, in the build's syntax, the file set those are in, the digest of each file the build parsed,
// by the name it parsed it under, and the answers of the functions standing, by kind, file digest and place.
type placedPackage struct {
	places  map[types.Object]fact_resolver.Place
	fileSet *token.FileSet
	digests map[string]string
	answers map[placedAt][]byte
}

// newPlacedHand is a hand handed no package, which answers nothing.
func newPlacedHand() *placedHand {
	return &placedHand{packages: map[*types.Package]placedPackage{}}
}

// hold has the hand answer of held, a package the build loaded from source, its syntax and types its own, with the
// answers of placed, the host's entry's of it, about each function standing names by its place, Parameter -1, and its
// parameters. standing is the caller's word on which of held's functions the host's answers still hold for, and a
// sound caller names a function only when all three hold: nothing its answers rest on was reached, so no function in
// its callee closure differs and a caller of a reached unit is never standing; the build parsed the same bytes the
// host did, which the digests check here too; and its place is its own, which DeclarationPlaces holds by placing no
// place two functions share. Unit B's comparison of the host's and the build's units is what makes the first true;
// until a build hands a predicate built on it, only a test does, and TestOnlyATestHoldsTheAnswers keeps every hand,
// this one included, out of an engine run. digests is the sha256 of each file's bytes as the build parsed it, by the
// name it parsed it under, as digestsOf reads them from the build's keys; an answer whose file the host held no digest
// of is never taken.
func (hand *placedHand) hold(
	held *packages.Package,
	placed []fact_resolver.PlacedAnswer,
	standing func(function fact_resolver.Place) bool,
	digests func(parsed string) ([]byte, bool),
) {
	answers := map[placedAt][]byte{}
	for _, answer := range placed {
		if len(answer.FileDigest) == 0 ||
			!standing(fact_resolver.Place{File: answer.File, Offset: answer.Offset, Parameter: -1}) {
			continue
		}
		answers[placedAt{kind: answer.Kind, file: answer.File, digest: string(answer.FileDigest), offset: answer.Offset,
			parameter: answer.Parameter}] = answer.Encoded
	}
	parsedDigests := map[string]string{}
	for _, file := range held.Syntax {
		if tokenFile := held.Fset.File(file.FileStart); tokenFile != nil {
			if digest, hashed := digests(tokenFile.Name()); hashed && len(digest) > 0 {
				parsedDigests[tokenFile.Name()] = string(digest)
			}
		}
	}
	hand.packages[held.Types] = placedPackage{
		places:  fact_resolver.DeclarationPlaces(held.Fset, held.Syntax, held.TypesInfo),
		fileSet: held.Fset,
		digests: parsedDigests,
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
	tokenFile := held.fileSet.File(object.Pos())
	if !isPlaced || tokenFile == nil {
		return nil, false
	}
	digest, hashed := held.digests[tokenFile.Name()]
	if !hashed {
		return nil, false
	}
	encoded, answered := held.answers[placedAt{kind: kindName, file: place.File, digest: digest, offset: place.Offset,
		parameter: place.Parameter}]
	return encoded, answered
}

// Computed hears an answer the resolver computed, which the hand, answering only from the host's, has no use for.
func (*placedHand) Computed(string, types.Object, []byte) {
	// Answers only from the host's, so an answer the resolver computed is nothing to keep.
}

// storedPlacedAt is the placed answers of the entry store holds under key, and whether it holds a whole one whose
// placed answers read.
func storedPlacedAt(store *cache.Store, key cache.Key) ([]fact_resolver.PlacedAnswer, bool) {
	entry, _, stored := storedEntryAt(store, key)
	if !stored {
		return nil, false
	}
	placed, placedError := entry.Placed.placed()
	if placedError != nil {
		return nil, false
	}
	return placed, true
}
