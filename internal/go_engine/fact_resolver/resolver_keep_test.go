package fact_resolver_test

import (
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// keepProbe is the kind the tests of Keep keep answers of, a boolean about a function, registered nowhere: no test of
// Keep computes a package whole or probes one.
var keepProbe = &fact_resolver.Kind{Name: "keep_probe"}

// keepRun is a run that learned middle, for a test of Keep: its resolver, the hand on its answers, middle as the
// resolver holds it, middle.Middle, the function the tests keep answers about, bottom.Bodied, and how many times a
// question the test asked was computed.
type keepRun struct {
	resolver *fact_resolver.Resolver
	hand     *keepHand
	middle   *fact_resolver.Package
	function *types.Func
	bodied   *types.Func
	computed int
}

// learnMiddle is learnMiddleWith a store that holds nothing.
func learnMiddle(test *testing.T, handed []byte, restoresBottom bool) *keepRun {
	test.Helper()
	return learnMiddleWith(test, keepStore{}, handed, restoresBottom)
}

// learnMiddleWith runs ctrlflow on middle, loaded from source, which is how a resolver learns a package, with store,
// nil for a run with none, and a hand that answers every question with handed, or none when handed is nil. With
// restoresBottom, bottom is restored, so the resolver never learns it and a store holding nothing of it cannot answer
// a question there.
func learnMiddleWith(test *testing.T, store fact_resolver.Store, handed []byte, restoresBottom bool) *keepRun {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"bodiless/middle"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	bottom := loaded.Roots[0].Imports["bodiless/bottom"]
	run := &keepRun{resolver: fact_resolver.New(store), hand: &keepHand{answer: handed, heard: map[string][]string{}}}
	run.resolver.HoldAnswers(run.hand)
	options := check.DispatchOptions()
	options.Resolver = run.resolver
	if restoresBottom {
		options.Restored = func(restoring *packages.Package) bool { return restoring == bottom }
		options.Restore = func(*checker.Action) error { return nil }
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{ctrlflow.Analyzer}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running ctrlflow on %s: %v", action.Package.ID, action.Err)
		}
	}
	run.middle = run.resolver.Package(loaded.Roots[0].Types)
	if run.middle == nil {
		test.Fatal("the resolver never learned middle, which ctrlflow ran on")
	}
	run.function, _ = loaded.Roots[0].Types.Scope().Lookup("Middle").(*types.Func)
	run.bodied = bottomFunction(bottom.Types, "Bodied")
	if run.function == nil || run.bodied == nil {
		test.Fatalf("the fixture declares Middle as %v and Bodied as %v", run.function, run.bodied)
	}
	return run
}

// ask asks keepProbe of object as middle's reader, with a compute that answers answer and counts itself.
func (run *keepRun) ask(object types.Object, answer bool) bool {
	return fact_resolver.Ask(run.resolver, run.middle, keepProbe, object, func(*fact_resolver.Package) bool {
		run.computed++
		return answer
	}, func() bool { return false })
}

// recorded is the answer the run recorded for the store as keepProbe's about middle.Middle, and whether it has one.
func (run *keepRun) recorded() (string, bool) {
	encoded, isRecorded := run.resolver.Recorded(run.middle.Types)[keepProbe.Name]["Middle"]
	return string(encoded), isRecorded
}

// An answer kept is an answer of its own among the ones the run holds, told to the hand once, and nothing recorded
// for the store: that is what gives a callee a kind judged inside its caller's judgment an answer. A question about
// it afterwards goes as it would had nothing been kept, computed and recorded then, so what the run records is what
// it would have recorded without Keep, and the hand, which heard the answer when it was kept, does not hear it again.
func TestAnAnswerKeptIsHeldAndHeardAndRecordedOnlyOnceAskedOf(test *testing.T) {
	test.Parallel()
	run := learnMiddle(test, nil, false)
	fact_resolver.Keep(run.resolver, run.middle, keepProbe, run.function, true)
	if answered := answeredNames(run.resolver.Answered()); !slices.Equal(answered, []string{"keep_probe Middle true"}) {
		test.Errorf("want Middle's answer held as true once kept, got %q", answered)
	}
	if recorded, isRecorded := run.recorded(); isRecorded {
		test.Errorf("want nothing recorded for an answer kept and never asked of, got %q", recorded)
	}
	if answer := run.ask(run.function, true); !answer || run.computed != 1 {
		test.Errorf("want the next question computed, as true, got %t after computing %d times", answer, run.computed)
	}
	if recorded, isRecorded := run.recorded(); !isRecorded || recorded != "true" {
		test.Errorf("want Middle's answer recorded as true once asked of, got %q (recorded: %t)", recorded, isRecorded)
	}
	if answered := answeredNames(run.resolver.Answered()); !slices.Equal(answered, []string{"keep_probe Middle true"}) {
		test.Errorf("want Middle's answer held once, as true, after it was asked of, got %q", answered)
	}
	if heard := run.hand.heard["Middle"]; !slices.Equal(heard, []string{"true"}) {
		test.Errorf("want the hand to hear Middle's answer once, as true, heard %q", heard)
	}
}

// An answer readers were given, or would be, stands, and Keep keeps nothing beside it: one already memoized, which a
// question computed as false, and a hand's, so a run under a hand holds only the answer the hand gives. Handed true
// in each, the run never holds true, the hand never hears it, and the next question is still answered false.
func TestKeepLeavesAnAnswerAlreadyGivenStanding(test *testing.T) {
	test.Parallel()
	for name, standing := range map[string]struct {
		handed       []byte
		asksFirst    bool
		wantAnswered []string
		wantHeard    []string
		wantComputed int
	}{
		"an answer already memoized": {asksFirst: true, wantAnswered: []string{"keep_probe Middle false"},
			wantHeard: []string{"false"}, wantComputed: 1},
		"a hand's answer": {handed: []byte("false")},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			run := learnMiddle(test, standing.handed, false)
			if standing.asksFirst {
				run.ask(run.function, false)
			}
			fact_resolver.Keep(run.resolver, run.middle, keepProbe, run.function, true)
			if answered := answeredNames(run.resolver.Answered()); !slices.Equal(answered, standing.wantAnswered) {
				test.Errorf("want %q held after Keep, got %q", standing.wantAnswered, answered)
			}
			if answer := run.ask(run.function, false); answer || run.computed != standing.wantComputed {
				test.Errorf("want the next question answered false after computing %d times, got %t after %d",
					standing.wantComputed, answer, run.computed)
			}
			if heard := run.hand.heard["Middle"]; !slices.Equal(heard, standing.wantHeard) {
				test.Errorf("want the hand to hear %q of Middle, heard %q", standing.wantHeard, heard)
			}
		})
	}
}

// Nothing is kept in an unsure package, as nothing computed there is: middle, handed a placeholder for bottom.Bodied,
// which the run restored and no store answers, is unsure, so an answer handed to Keep afterwards may rest on the
// placeholder. The run holds nothing of it, and the hand hears nothing.
func TestNothingIsKeptInAnUnsurePackage(test *testing.T) {
	test.Parallel()
	run := learnMiddle(test, nil, true)
	run.ask(run.bodied, true)
	if !run.resolver.Unsure(run.middle.Types) || run.computed != 0 {
		test.Fatalf("want middle unsure after a question of bottom, restored, and nothing computed, got unsure %t "+
			"after computing %d times", run.resolver.Unsure(run.middle.Types), run.computed)
	}
	fact_resolver.Keep(run.resolver, run.middle, keepProbe, run.function, true)
	if answered := answeredNames(run.resolver.Answered()); len(answered) != 0 {
		test.Errorf("want nothing held of Middle in an unsure package, got %q", answered)
	}
	if heard := run.hand.heard["Middle"]; len(heard) != 0 {
		test.Errorf("want the hand to hear nothing of Middle, heard %q", heard)
	}
}

// Nothing is kept about an object of no package, the error interface's Error, which Ask answers no fact without
// computing or keeping anything: the hand hears nothing of it.
func TestNothingIsKeptAboutAnObjectOfNoPackage(test *testing.T) {
	test.Parallel()
	run := learnMiddle(test, nil, false)
	errorInterface, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		test.Fatal("the universe's error is no interface")
	}
	fact_resolver.Keep(run.resolver, run.middle, keepProbe, errorInterface.Method(0), true)
	if heard := run.hand.heard["Error"]; len(heard) != 0 {
		test.Errorf("want the hand to hear nothing of error's Error, heard %q", heard)
	}
}

// Nothing is kept about an instance's object, which Ask answers no fact without computing or keeping anything: the
// Value of middle.Box[int], a method of middle's package that is not its own origin. The run holds nothing of it, and
// the hand hears nothing.
func TestNothingIsKeptAboutAnInstancesObject(test *testing.T) {
	test.Parallel()
	run := learnMiddle(test, nil, false)
	box, isNamed := run.middle.Types.Scope().Lookup("Box").Type().(*types.Named)
	if !isNamed {
		test.Fatal("middle declares no named type Box")
	}
	instance, instantiateError := types.Instantiate(nil, box, []types.Type{types.Typ[types.Int]}, true)
	if instantiateError != nil {
		test.Fatalf("instantiating middle.Box with int: %v", instantiateError)
	}
	boxOfInt, isInstance := instance.(*types.Named)
	if !isInstance || boxOfInt.NumMethods() != 1 || boxOfInt.Method(0).Origin() == boxOfInt.Method(0) {
		test.Fatalf("want middle.Box[int] a named type whose one method is an instance's, got %v", instance)
	}
	fact_resolver.Keep(run.resolver, run.middle, keepProbe, boxOfInt.Method(0), true)
	if answered := answeredNames(run.resolver.Answered()); len(answered) != 0 {
		test.Errorf("want nothing held of an instance's method, got %q", answered)
	}
	if heard := run.hand.heard["Value"]; len(heard) != 0 {
		test.Errorf("want the hand to hear nothing of middle.Box[int]'s Value, heard %q", heard)
	}
}

// An answer kept twice is kept once: held once, and heard by the hand once, never again when a walk hands the same
// verdict over a second time.
func TestAnAnswerKeptTwiceIsHeldAndHeardOnce(test *testing.T) {
	test.Parallel()
	run := learnMiddle(test, nil, false)
	fact_resolver.Keep(run.resolver, run.middle, keepProbe, run.function, true)
	fact_resolver.Keep(run.resolver, run.middle, keepProbe, run.function, true)
	if answered := answeredNames(run.resolver.Answered()); !slices.Equal(answered, []string{"keep_probe Middle true"}) {
		test.Errorf("want Middle's answer held once, as true, got %q", answered)
	}
	if heard := run.hand.heard["Middle"]; !slices.Equal(heard, []string{"true"}) {
		test.Errorf("want the hand to hear Middle's answer once, as true, heard %q", heard)
	}
}

// A kind's walk keeps every verdict it judges, the one a reader asked of among them, so the compute of a question
// keeps the very answer the question then returns. It is one answer: recorded by the question, as any computed answer
// is, held once, and heard by the hand once, never once from Keep and again from Ask.
func TestAnAnswerItsOwnComputeKeptIsHeardOnce(test *testing.T) {
	test.Parallel()
	run := learnMiddle(test, nil, false)
	answer := fact_resolver.Ask(run.resolver, run.middle, keepProbe, run.function,
		func(home *fact_resolver.Package) bool {
			fact_resolver.Keep(run.resolver, home, keepProbe, run.function, true)
			return true
		}, func() bool { return false })
	if recorded, isRecorded := run.recorded(); !answer || !isRecorded || recorded != "true" {
		test.Errorf("want the question answered true and recorded as true, got %t and %q (recorded: %t)", answer,
			recorded, isRecorded)
	}
	if answered := answeredNames(run.resolver.Answered()); !slices.Equal(answered, []string{"keep_probe Middle true"}) {
		test.Errorf("want Middle's answer held once, as true, got %q", answered)
	}
	if heard := run.hand.heard["Middle"]; !slices.Equal(heard, []string{"true"}) {
		test.Errorf("want the hand to hear Middle's answer once, as true, heard %q", heard)
	}
}
