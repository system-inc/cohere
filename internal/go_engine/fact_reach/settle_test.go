package fact_reach_test

import (
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// fixture is a package's host side as a test writes it: each object's users, each body's recorded questions, and the
// functions declared at each body.
type fixture struct {
	users    map[fact_reach.Object][]fact_reach.Place
	recorded map[fact_reach.Place][]fact_reach.Question
	declared map[fact_reach.Place][]fact_reach.Object

	// parameters are how many parameters each declared function takes, by its key: none for a key it lacks.
	parameters map[fact_reach.Object]int
}

func (source fixture) Users(object fact_reach.Object) []fact_reach.Place { return source.users[object] }

func (source fixture) Recorded(place fact_reach.Place) []fact_reach.Question {
	return source.recorded[place]
}

func (source fixture) Declared(place fact_reach.Place) []fact_reach.Object {
	return source.declared[place]
}

func (source fixture) Parameters(key fact_reach.Object) int { return source.parameters[key] }

// The bodies of the reviewer's example (#w72zxtq, must 6): `size` names `platformSize`, which a build's file declares
// otherwise, so the reach lints it; `Warm` names only `size`, and `Outer` only `Warm`.
var (
	size  = fact_reach.Place{File: "shared.go", Offset: 10}
	warm  = fact_reach.Place{File: "shared.go", Offset: 60}
	outer = fact_reach.Place{File: "shared.go", Offset: 90}

	purityOfSize = fact_reach.Question{Key: "function_purity size", Kind: "function_purity", About: "size",
		Parameter: -1}
	purityOfWarm = fact_reach.Question{Key: "function_purity Warm", Kind: "function_purity", About: "Warm",
		Parameter: -1}
)

// warmFixture is that package: Warm uses size, Outer uses Warm, and the host recorded the purity of size and of Warm.
func warmFixture() fixture {
	return fixture{
		users: map[fact_reach.Object][]fact_reach.Place{"size": {warm}, "Warm": {outer}},
		recorded: map[fact_reach.Place][]fact_reach.Question{
			size: {purityOfSize},
			warm: {purityOfWarm},
		},
		declared: map[fact_reach.Place][]fact_reach.Object{size: {"size"}, warm: {"Warm"}, outer: {"Outer"}},
	}
}

// answering is a compare that reads each question's answer from answers, and the count of how often each was asked,
// which the compare keeps.
func answering(
	answers map[string]fact_reach.Comparison,
) (func(fact_reach.Question) fact_reach.Comparison, map[string]int) {
	asked := map[string]int{}
	return func(question fact_reach.Question) fact_reach.Comparison {
		asked[question.Key]++
		return answers[question.Key]
	}, asked
}

// A callee the build lints whose purity differs takes its unreached caller out of the standing set, the finding the
// reach alone would lose, and the walk stops where the caller's own answer reads equal. The set handed in is only
// read: what still stands is the settlement's own.
func TestACalleesDifferingAnswerTakesItsCallerOut(test *testing.T) {
	test.Parallel()
	standing := map[fact_reach.Place]bool{warm: true, outer: true}
	compare, _ := answering(
		map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Differs, purityOfWarm.Key: fact_reach.Equal})
	settlement := fact_reach.Settle(standing, []fact_reach.Place{size}, nil, nil, warmFixture(), compare)
	wantRemoved := []fact_reach.Removal{{Place: warm, Object: "size", Reason: "answer function_purity"}}
	if !reflect.DeepEqual(settlement.Removed, wantRemoved) {
		test.Errorf("removed %v, want %v", settlement.Removed, wantRemoved)
	}
	if settlement.Standing[warm] || !settlement.Standing[outer] {
		test.Errorf("standing is %v, want Outer alone: Warm's own answer read equal, so nothing above it moves",
			settlement.Standing)
	}
	if !standing[warm] || len(standing) != 2 {
		test.Errorf("the set handed in is now %v, want it as it was handed, Warm and Outer", standing)
	}
	if settlement.Asked != 2 || settlement.Differing["function_purity"] != 1 {
		test.Errorf("asked %d with %v differing, want 2 asked and one function_purity differing", settlement.Asked,
			settlement.Differing)
	}
}

// An answer that reads equal cuts the walk off at once: the caller stands, and its own answer is never asked.
func TestAnEqualAnswerMovesNothing(test *testing.T) {
	test.Parallel()
	compare, asked := answering(map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Equal})
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{warm: true, outer: true}, []fact_reach.Place{size}, nil,
		nil, warmFixture(), compare)
	if len(settlement.Removed) != 0 || !settlement.Standing[warm] || !settlement.Standing[outer] {
		test.Errorf("removed %v leaving %v, want nothing removed", settlement.Removed, settlement.Standing)
	}
	if asked[purityOfWarm.Key] != 0 {
		test.Errorf("Warm's purity was asked %d times, want never: Warm stands", asked[purityOfWarm.Key])
	}
}

// An answer the build cannot give is taken as differing, and each differing answer carries the walk one caller up.
func TestAnUnknownAnswerJoinsAndADifferenceCarriesUp(test *testing.T) {
	test.Parallel()
	// Warm's answer is absent from the answers, so it reads as the zero Comparison, Unknown.
	compare, _ := answering(map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Differs})
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{warm: true, outer: true}, []fact_reach.Place{size}, nil,
		nil, warmFixture(), compare)
	wantRemoved := []fact_reach.Removal{
		{Place: warm, Object: "size", Reason: "answer function_purity"},
		{Place: outer, Object: "Warm", Reason: "unknown function_purity"},
	}
	if !reflect.DeepEqual(settlement.Removed, wantRemoved) {
		test.Errorf("removed %v, want %v", settlement.Removed, wantRemoved)
	}
	if len(settlement.Standing) != 0 {
		test.Errorf("standing is %v, want nothing", settlement.Standing)
	}
	if settlement.Unknown["function_purity"] != 1 {
		test.Errorf("unknown answers %v, want one of function_purity", settlement.Unknown)
	}
}

// Every comparison but Equal and Differs reads unknown (#e3q4cds): Unknown itself, and a value of the type none of the
// three names, which no compare gives today and one that did would mean nothing the build could trust. Each names size
// as an unknown answer of its kind, and takes Warm, its user, out, so nothing the build cannot read stands; Warm's own
// purity, asked as it leaves, reads equal.
func TestEveryComparisonButEqualAndDiffersNamesItsObjectUnknown(test *testing.T) {
	test.Parallel()
	for _, comparison := range []fact_reach.Comparison{fact_reach.Unknown, fact_reach.Comparison(3),
		fact_reach.Comparison(-1)} {
		compare, _ := answering(
			map[string]fact_reach.Comparison{purityOfSize.Key: comparison, purityOfWarm.Key: fact_reach.Equal})
		settlement := fact_reach.Settle(map[fact_reach.Place]bool{warm: true}, []fact_reach.Place{size}, nil, nil,
			warmFixture(), compare)
		wantRemoved := []fact_reach.Removal{{Place: warm, Object: "size", Reason: "unknown function_purity"}}
		if !reflect.DeepEqual(settlement.Removed, wantRemoved) || settlement.Unknown["function_purity"] != 1 {
			test.Errorf("comparing %d, removed %v with unknown answers %v, want %v and one function_purity unknown",
				comparison, settlement.Removed, settlement.Unknown, wantRemoved)
		}
	}
}

// An object named before any answer is asked takes its users out with its own reason, and their recorded answers are
// asked as any body's that left.
func TestANamedObjectTakesItsUsersOut(test *testing.T) {
	test.Parallel()
	source := warmFixture()
	source.users["os.Exit"] = []fact_reach.Place{size}
	compare, asked := answering(map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Equal})
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{size: true, warm: true, outer: true}, nil,
		[]fact_reach.Named{{Object: "os.Exit", Reason: "no-return"}}, nil, source, compare)
	wantRemoved := []fact_reach.Removal{{Place: size, Object: "os.Exit", Reason: "no-return"}}
	if !reflect.DeepEqual(settlement.Removed, wantRemoved) {
		test.Errorf("removed %v, want %v", settlement.Removed, wantRemoved)
	}
	if asked[purityOfSize.Key] != 1 {
		test.Errorf("size's purity was asked %d times, want once, as it left", asked[purityOfSize.Key])
	}
}

// A question recorded under two bodies is asked once, and a user the build lints already is not counted as removed.
func TestAQuestionIsAskedOnceAndAReachedUserIsNotRemoved(test *testing.T) {
	test.Parallel()
	source := warmFixture()
	// The answer about size also names Warm as a function its judgment entered, so it is recorded under both.
	source.recorded[warm] = append(source.recorded[warm], purityOfSize)
	compare, asked := answering(
		map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Differs, purityOfWarm.Key: fact_reach.Equal})
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{outer: true}, []fact_reach.Place{size, warm}, nil, nil,
		source, compare)
	if asked[purityOfSize.Key] != 1 {
		test.Errorf("size's purity was asked %d times, want once", asked[purityOfSize.Key])
	}
	if len(settlement.Removed) != 0 || !settlement.Standing[outer] {
		test.Errorf("removed %v leaving %v, want none: Warm, size's one user, was reached before", settlement.Removed,
			settlement.Standing)
	}
}

// A question recorded under two bodies is asked once and is a record at both (#e3q4cds): Warm's purity names size as
// a function its judgment entered, so the host recorded it under size too, and both bodies are linted. size reaches
// Warm's record first and asks it; Warm, reached after, holds it all the same, so Warm is named for no missing record
// and Outer, its user, stands. With Warm's record gone from both, Warm holds none, and Outer leaves.
func TestAQuestionRecordedUnderTwoBodiesIsARecordAtEach(test *testing.T) {
	test.Parallel()
	kinds := []fact_reach.Kind{{Name: "function_purity"}}
	compare, asked := answering(
		map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Equal, purityOfWarm.Key: fact_reach.Equal})
	source := warmFixture()
	source.recorded[size] = append(source.recorded[size], purityOfWarm)
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{outer: true}, []fact_reach.Place{size, warm}, nil,
		kinds, source, compare)
	if !settlement.Standing[outer] || len(settlement.Unrecorded) != 0 || asked[purityOfWarm.Key] != 1 {
		test.Errorf("removed %v with %v unrecorded, Warm's purity asked %d times, want Outer standing, nothing "+
			"unrecorded and Warm's purity asked once", settlement.Removed, settlement.Unrecorded,
			asked[purityOfWarm.Key])
	}
	source = warmFixture()
	delete(source.recorded, warm)
	settlement = fact_reach.Settle(map[fact_reach.Place]bool{outer: true}, []fact_reach.Place{size, warm}, nil, kinds,
		source, compare)
	wantRemoved := []fact_reach.Removal{{Place: outer, Object: "Warm", Reason: "unrecorded function_purity"}}
	if !reflect.DeepEqual(settlement.Removed, wantRemoved) {
		test.Errorf("with no record of Warm's purity, removed %v, want %v", settlement.Removed, wantRemoved)
	}
}

// With nothing reached and nothing named, nothing is asked and everything stands, and a set never handed still gives
// the settlement one of its own.
func TestNothingReachedAsksNothing(test *testing.T) {
	test.Parallel()
	compare, asked := answering(nil)
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{size: true, warm: true, outer: true}, nil, nil, nil,
		warmFixture(), compare)
	if settlement.Asked != 0 || len(asked) != 0 || len(settlement.Standing) != 3 {
		test.Errorf("asked %d, leaving %v, want nothing asked and all three standing", settlement.Asked,
			settlement.Standing)
	}
	none := fact_reach.Settle(nil, nil, nil, nil, warmFixture(), compare)
	if none.Standing == nil || len(none.Standing) != 0 {
		test.Errorf("a nil set left %v standing, want an empty set of the settlement's own", none.Standing)
	}
}

// A body that left with no record of a kind reads unknown for it (#5vbs1np, must 1): a kind's judgment may have read
// its answer from the kind's state for another function's, as size's purity reads its callees', and left no record.
// size holds a record of purity alone, so the build names it for nil-ness, and Warm, its user, leaves for that; Warm,
// holding no record of either kind, names itself for both, the first taking Outer out, which names itself for both in
// turn. A kind a body holds a record of is never named for, whatever the record reads.
func TestABodyThatLeftWithNoRecordOfAKindReadsUnknownForIt(test *testing.T) {
	test.Parallel()
	source := warmFixture()
	delete(source.recorded, warm)
	compare, _ := answering(map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Equal})
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{warm: true, outer: true}, []fact_reach.Place{size}, nil,
		[]fact_reach.Kind{{Name: "function_purity"}, {Name: "return_nil_ness"}}, source, compare)
	wantRemoved := []fact_reach.Removal{
		{Place: warm, Object: "size", Reason: "unrecorded return_nil_ness"},
		{Place: outer, Object: "Warm", Reason: "unrecorded function_purity"},
	}
	if !reflect.DeepEqual(settlement.Removed, wantRemoved) {
		test.Errorf("removed %v, want %v", settlement.Removed, wantRemoved)
	}
	wantUnrecorded := map[string]int{"function_purity": 2, "return_nil_ness": 3}
	if !reflect.DeepEqual(settlement.Unrecorded, wantUnrecorded) || len(settlement.Unknown) != 0 {
		test.Errorf("unrecorded %v and unknown %v, want %v and none", settlement.Unrecorded, settlement.Unknown,
			wantUnrecorded)
	}
}

// With every kind recorded where a body left, the rule names nothing: the walk is the one of no kinds.
func TestABodyRecordedInEveryKindIsNamedForNone(test *testing.T) {
	test.Parallel()
	compare, _ := answering(map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Equal})
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{warm: true, outer: true}, []fact_reach.Place{size}, nil,
		[]fact_reach.Kind{{Name: "function_purity"}}, warmFixture(), compare)
	if len(settlement.Removed) != 0 || len(settlement.Unrecorded) != 0 || len(settlement.Standing) != 2 {
		test.Errorf("removed %v with %v unrecorded, leaving %v, want nothing removed", settlement.Removed,
			settlement.Unrecorded, settlement.Standing)
	}
}

// A kind that answers about each parameter is recorded by the parameter (round 2's N1): size takes two, and the host
// recorded odd slice's answer about the first alone, so size reads unknown for the kind and Warm leaves; with both
// recorded, Warm stands. A place two functions share is no record of anything for either: size's, declaring sizeToo
// beside it, names both for purity, whose one answer it holds is size's.
func TestAKindRecordedByParameterNeedsEachParameterRecorded(test *testing.T) {
	test.Parallel()
	odd := fact_reach.Kind{Name: "odd_slice_argument", Parameters: true}
	oddOf := func(index int) fact_reach.Question {
		return fact_reach.Question{Key: "odd size " + string(rune('0'+index)), Kind: odd.Name, About: "size",
			Parameter: index}
	}
	settle := func(source fixture, kinds ...fact_reach.Kind) *fact_reach.Settlement {
		compare := func(fact_reach.Question) fact_reach.Comparison { return fact_reach.Equal }
		return fact_reach.Settle(map[fact_reach.Place]bool{warm: true, outer: true}, []fact_reach.Place{size}, nil,
			kinds, source, compare)
	}
	source := warmFixture()
	source.parameters = map[fact_reach.Object]int{"size": 2}
	source.recorded[size] = []fact_reach.Question{oddOf(0)}
	wantRemoved := []fact_reach.Removal{{Place: warm, Object: "size", Reason: "unrecorded odd_slice_argument"}}
	if settlement := settle(source, odd); !reflect.DeepEqual(settlement.Removed[:1], wantRemoved) {
		test.Errorf("with one of two parameters recorded, removed %v, want %v first", settlement.Removed, wantRemoved)
	}
	source.recorded[size] = []fact_reach.Question{oddOf(0), oddOf(1)}
	if settlement := settle(source, odd); len(settlement.Removed) != 0 {
		test.Errorf("with both parameters recorded, removed %v, want nothing", settlement.Removed)
	}
	shared := warmFixture()
	shared.declared[size] = []fact_reach.Object{"size", "sizeToo"}
	shared.users["sizeToo"] = []fact_reach.Place{outer}
	settlement := settle(shared, fact_reach.Kind{Name: "function_purity"})
	wantShared := []fact_reach.Removal{
		{Place: warm, Object: "size", Reason: "unrecorded function_purity"},
		{Place: outer, Object: "sizeToo", Reason: "unrecorded function_purity"},
	}
	if !reflect.DeepEqual(settlement.Removed, wantShared) {
		test.Errorf("at a place two functions share, removed %v, want %v", settlement.Removed, wantShared)
	}
}
