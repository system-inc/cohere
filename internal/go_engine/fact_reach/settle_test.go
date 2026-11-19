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

// answering is a compare that reads each question's answer from answers, counting how often each is asked.
func answering(
	answers map[string]fact_reach.Comparison,
	asked map[string]int,
) func(fact_reach.Question) fact_reach.Comparison {
	return func(question fact_reach.Question) fact_reach.Comparison {
		asked[question.Key]++
		return answers[question.Key]
	}
}

// A callee the build lints whose purity differs takes its unreached caller out of the standing set, the finding the
// reach alone would lose, and the walk stops where the caller's own answer reads equal.
func TestACalleesDifferingAnswerTakesItsCallerOut(test *testing.T) {
	test.Parallel()
	standing := map[fact_reach.Place]bool{warm: true, outer: true}
	asked := map[string]int{}
	settlement := fact_reach.Settle(standing, []fact_reach.Place{size}, nil, warmFixture(), answering(
		map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Differs, purityOfWarm.Key: fact_reach.Equal},
		asked))
	wantRemoved := []fact_reach.Removal{{Place: warm, Object: "size", Reason: "answer function_purity"}}
	if !reflect.DeepEqual(settlement.Removed, wantRemoved) {
		test.Errorf("removed %v, want %v", settlement.Removed, wantRemoved)
	}
	if standing[warm] || !standing[outer] {
		test.Errorf("standing is %v, want Outer alone: Warm's own answer read equal, so nothing above it moves",
			standing)
	}
	if settlement.Asked != 2 || settlement.Differing["function_purity"] != 1 {
		test.Errorf("asked %d with %v differing, want 2 asked and one function_purity differing", settlement.Asked,
			settlement.Differing)
	}
}

// An answer that reads equal cuts the walk off at once: the caller stands, and its own answer is never asked.
func TestAnEqualAnswerMovesNothing(test *testing.T) {
	test.Parallel()
	standing := map[fact_reach.Place]bool{warm: true, outer: true}
	asked := map[string]int{}
	settlement := fact_reach.Settle(standing, []fact_reach.Place{size}, nil, warmFixture(), answering(
		map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Equal}, asked))
	if len(settlement.Removed) != 0 || !standing[warm] || !standing[outer] {
		test.Errorf("removed %v leaving %v, want nothing removed", settlement.Removed, standing)
	}
	if asked[purityOfWarm.Key] != 0 {
		test.Errorf("Warm's purity was asked %d times, want never: Warm stands", asked[purityOfWarm.Key])
	}
}

// An answer the build cannot give is taken as differing, and each differing answer carries the walk one caller up.
func TestAnUnknownAnswerJoinsAndADifferenceCarriesUp(test *testing.T) {
	test.Parallel()
	standing := map[fact_reach.Place]bool{warm: true, outer: true}
	// Warm's answer is absent from the answers, so it reads as the zero Comparison, Unknown.
	settlement := fact_reach.Settle(standing, []fact_reach.Place{size}, nil, warmFixture(), answering(
		map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Differs}, map[string]int{}))
	wantRemoved := []fact_reach.Removal{
		{Place: warm, Object: "size", Reason: "answer function_purity"},
		{Place: outer, Object: "Warm", Reason: "unknown function_purity"},
	}
	if !reflect.DeepEqual(settlement.Removed, wantRemoved) {
		test.Errorf("removed %v, want %v", settlement.Removed, wantRemoved)
	}
	if len(standing) != 0 {
		test.Errorf("standing is %v, want nothing", standing)
	}
	if settlement.Unknown["function_purity"] != 1 {
		test.Errorf("unknown answers %v, want one of function_purity", settlement.Unknown)
	}
}

// An object named before any answer is asked takes its users out with its own reason, and their recorded answers are
// asked as any body's that left.
func TestANamedObjectTakesItsUsersOut(test *testing.T) {
	test.Parallel()
	standing := map[fact_reach.Place]bool{size: true, warm: true, outer: true}
	source := warmFixture()
	source.users["os.Exit"] = []fact_reach.Place{size}
	asked := map[string]int{}
	settlement := fact_reach.Settle(standing, nil, []fact_reach.Named{{Object: "os.Exit", Reason: "no-return"}}, source,
		answering(map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Equal}, asked))
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
	standing := map[fact_reach.Place]bool{outer: true}
	asked := map[string]int{}
	settlement := fact_reach.Settle(standing, []fact_reach.Place{size, warm}, nil, source, answering(
		map[string]fact_reach.Comparison{purityOfSize.Key: fact_reach.Differs, purityOfWarm.Key: fact_reach.Equal},
		asked))
	if asked[purityOfSize.Key] != 1 {
		test.Errorf("size's purity was asked %d times, want once", asked[purityOfSize.Key])
	}
	if len(settlement.Removed) != 0 || !standing[outer] {
		test.Errorf("removed %v leaving %v, want none: Warm, size's one user, was reached before", settlement.Removed,
			standing)
	}
}

// With nothing reached and nothing named, nothing is asked and everything stands.
func TestNothingReachedAsksNothing(test *testing.T) {
	test.Parallel()
	standing := map[fact_reach.Place]bool{size: true, warm: true, outer: true}
	asked := map[string]int{}
	settlement := fact_reach.Settle(standing, nil, nil, warmFixture(), answering(nil, asked))
	if settlement.Asked != 0 || len(asked) != 0 || len(standing) != 3 {
		test.Errorf("asked %d, leaving %v, want nothing asked and all three standing", settlement.Asked, standing)
	}
}
