package fact_reach_test

import (
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// fixture is a package's host side as a test writes it: each object's users, and each body's recorded questions.
type fixture struct {
	users    map[fact_reach.Object][]fact_reach.Place
	recorded map[fact_reach.Place][]fact_reach.Question
}

func (source fixture) Users(object fact_reach.Object) []fact_reach.Place { return source.users[object] }

func (source fixture) Recorded(place fact_reach.Place) []fact_reach.Question {
	return source.recorded[place]
}

// The bodies of the reviewer's example (#w72zxtq, must 6): `size` names `platformSize`, which a build's file declares
// otherwise, so the reach lints it; `Warm` names only `size`, and `Outer` only `Warm`.
var (
	size  = fact_reach.Place{File: "shared.go", Offset: 10}
	warm  = fact_reach.Place{File: "shared.go", Offset: 60}
	outer = fact_reach.Place{File: "shared.go", Offset: 90}

	purityOfSize = fact_reach.Question{Key: "function_purity size", Kind: "function_purity", About: "size"}
	purityOfWarm = fact_reach.Question{Key: "function_purity Warm", Kind: "function_purity", About: "Warm"}
)

// warmFixture is that package: Warm uses size, Outer uses Warm, and the host recorded the purity of size and of Warm.
func warmFixture() fixture {
	return fixture{
		users: map[fact_reach.Object][]fact_reach.Place{"size": {warm}, "Warm": {outer}},
		recorded: map[fact_reach.Place][]fact_reach.Question{
			size: {purityOfSize},
			warm: {purityOfWarm},
		},
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
	settlement := fact_reach.Settle(standing, []fact_reach.Place{size}, nil, warmFixture(), compare)
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
		warmFixture(), compare)
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
		warmFixture(), compare)
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

// An object named before any answer is asked takes its users out with its own reason, and their recorded answers are
// asked as any body's that left.
func TestANamedObjectTakesItsUsersOut(test *testing.T) {
	test.Parallel()
	source := warmFixture()
	source.users["os.Exit"] = []fact_reach.Place{size}
	compare, asked := answering(map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Equal})
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{size: true, warm: true, outer: true}, nil,
		[]fact_reach.Named{{Object: "os.Exit", Reason: "no-return"}}, source, compare)
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
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{outer: true}, []fact_reach.Place{size, warm}, nil, source,
		compare)
	if asked[purityOfSize.Key] != 1 {
		test.Errorf("size's purity was asked %d times, want once", asked[purityOfSize.Key])
	}
	if len(settlement.Removed) != 0 || !settlement.Standing[outer] {
		test.Errorf("removed %v leaving %v, want none: Warm, size's one user, was reached before", settlement.Removed,
			settlement.Standing)
	}
}

// With nothing reached and nothing named, nothing is asked and everything stands, and a set never handed still gives
// the settlement one of its own.
func TestNothingReachedAsksNothing(test *testing.T) {
	test.Parallel()
	compare, asked := answering(nil)
	settlement := fact_reach.Settle(map[fact_reach.Place]bool{size: true, warm: true, outer: true}, nil, nil,
		warmFixture(), compare)
	if settlement.Asked != 0 || len(asked) != 0 || len(settlement.Standing) != 3 {
		test.Errorf("asked %d, leaving %v, want nothing asked and all three standing", settlement.Asked,
			settlement.Standing)
	}
	none := fact_reach.Settle(nil, nil, nil, warmFixture(), compare)
	if none.Standing == nil || len(none.Standing) != 0 {
		test.Errorf("a nil set left %v standing, want an empty set of the settlement's own", none.Standing)
	}
}
