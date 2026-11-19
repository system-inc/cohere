package standing_facts_test

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// purityOf is the question of the purity of the function a host keys as object, and how the build is asked it: of
// function, with the host's answer encoded.
func purityOf(
	object fact_reach.Object,
	asking fact_reach.Asking,
) (fact_reach.Question, map[string]fact_reach.Asking) {
	question := fact_reach.Question{Key: purity + " " + string(object), Kind: purity, About: object}
	asking.Kind = purity
	return question, map[string]fact_reach.Asking{question.Key: asking}
}

// requireRemoved fails unless the bodies the facts took out are exactly want, in order.
func requireRemoved(test *testing.T, result *standing_facts.Result, want ...fact_reach.Removal) {
	test.Helper()
	if result.Settlement == nil {
		test.Fatalf("the result holds no settlement, want one removing %v", want)
	}
	if !slices.Equal(result.Settlement.Removed, want) {
		test.Errorf("removed %v, want %v", result.Settlement.Removed, want)
	}
}

// The reviewer's example (#w72zxtq, must 6) in a real build: the host recorded size not pure, the build lints size
// already and answers pure, so Warm, which the reach left standing and which discards size's result, leaves.
func TestACalleeWhoseAnswerDiffersTakesItsStandingCallerOut(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	size, warm := found.place(test, "size"), found.place(test, "Warm")
	question, asking := purityOf("facts.size",
		fact_reach.Asking{Function: found.identity("size"), Parameter: -1, Encoded: notPure(test)})
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{warm: true},
		places:   []fact_reach.Place{size, warm},
		asking:   asking,
		users:    map[fact_reach.Object][]fact_reach.Place{"facts.size": {warm}},
		recorded: map[fact_reach.Place][]fact_reach.Question{size: {question}},
	}
	result, resolver := settle(test, found, host)
	if bytes.Equal(buildsPurity(test, found, resolver, found.function(test, "size")), notPure(test)) {
		test.Fatalf("the build answers size not pure, as the host did, so the fixture differs in nothing")
	}
	requireRemoved(test, result,
		fact_reach.Removal{Place: warm, Object: "facts.size", Reason: fact_reach.DiffersReason(purity)})
	if result.Stands(warm.File, warm.Offset) {
		test.Errorf("Warm stands, though size's answer differs in the build")
	}
	if !result.Partial || result.Settlement.Asked != 1 || result.Settlement.Differing[purity] != 1 {
		test.Errorf("partial %t, asked %d, differing %v, want a partial package, one question, one differing",
			result.Partial, result.Settlement.Asked, result.Settlement.Differing)
	}
}

// With the host's answer about size the build's own, nothing moves: Warm stands, and Stands says so of Warm alone,
// never of size, which the build lints.
func TestAnAnswerEqualInTheBuildLeavesTheCallerStanding(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	size, warm := found.place(test, "size"), found.place(test, "Warm")
	question, asking := purityOf("facts.size", fact_reach.Asking{Function: found.identity("size"), Parameter: -1,
		Encoded: purityInAnotherRun(test, "size")})
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{warm: true},
		places:   []fact_reach.Place{size, warm},
		asking:   asking,
		users:    map[fact_reach.Object][]fact_reach.Place{"facts.size": {warm}},
		recorded: map[fact_reach.Place][]fact_reach.Question{size: {question}},
	}
	result, _ := settle(test, found, host)
	requireRemoved(test, result)
	if !result.Stands(warm.File, warm.Offset) {
		test.Errorf("Warm does not stand, though size's answer is the host's")
	}
	if result.Stands(size.File, size.Offset) || result.Stands(warm.File, warm.Offset+1) {
		test.Errorf("a body outside the standing set stands: size, or whatever is one byte past Warm")
	}
	if result.Settlement.Asked != 1 || host.calls["Asking "+question.Key] != 1 {
		test.Errorf("asked %d questions and the host %d times, want once each", result.Settlement.Asked,
			host.calls["Asking "+question.Key])
	}
}

// The host held no no-return fact of stop, nor of below.Stop, and in the build neither returns, so a body after a
// call of one is unreachable there alone: Caller and Quit leave, each for its callee's fact.
func TestAFunctionThatNeverReturnsOnlyInTheBuildTakesItsCallersOut(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	caller, quit := found.place(test, "Caller"), found.place(test, "Quit")
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{caller: true, quit: true},
		places:   []fact_reach.Place{caller, quit},
		functions: []fact_reach.Function{
			{Object: "facts.stop", Identity: found.identity("stop")},
			{Object: "below.Stop", Identity: imported("below", "Stop")},
		},
		users: map[fact_reach.Object][]fact_reach.Place{"facts.stop": {caller}, "below.Stop": {quit}},
	}
	result, _ := settle(test, found, host)
	requireRemoved(test, result,
		fact_reach.Removal{Place: caller, Object: "facts.stop", Reason: "no-return"},
		fact_reach.Removal{Place: quit, Object: "below.Stop", Reason: "no-return"})
	if result.FunctionsCompared != 2 || result.FunctionsNamed != 2 {
		test.Errorf("compared %d functions and named %d, want 2 and 2", result.FunctionsCompared,
			result.FunctionsNamed)
	}
}

// With the host's facts the build's, of this package's stop, of the method halt, and of below.Stop, which the package
// calls as a statement and so reads the fact of, nothing is named, and a function that returns in both is as equal.
func TestNoReturnFactsEqualInTheBuildNameNothing(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	caller, quit := found.place(test, "Caller"), found.place(test, "Quit")
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{caller: true, quit: true},
		places:   []fact_reach.Place{caller, quit},
		functions: []fact_reach.Function{
			{Object: "facts.stop", Identity: found.identity("stop"), NoReturn: true},
			{Object: "facts.gauge.halt", Identity: found.method("gauge", "halt"), NoReturn: true},
			{Object: "below.Stop", Identity: imported("below", "Stop"), NoReturn: true},
			{Object: "facts.Warm", Identity: found.identity("Warm")},
		},
		users: map[fact_reach.Object][]fact_reach.Place{
			"facts.stop": {caller}, "facts.gauge.halt": {caller}, "below.Stop": {quit}, "facts.Warm": {caller},
		},
	}
	result, _ := settle(test, found, host)
	requireRemoved(test, result)
	if !result.Stands(caller.File, caller.Offset) || !result.Stands(quit.File, quit.Offset) {
		test.Errorf("Caller or Quit does not stand, though every no-return fact is the host's")
	}
	if result.FunctionsCompared != 4 || result.FunctionsNamed != 0 || len(host.usersAsked) != 0 {
		test.Errorf("compared %d functions, named %d, asked the users of %v, want 4 compared and none named",
			result.FunctionsCompared, result.FunctionsNamed, host.usersAsked)
	}
}

// A function of this package declared in another file than the host's identity names is another declaration, as a
// platform's file declares the same name with another body: it is not found, though its fact here is the host's, and
// its users leave.
func TestAFunctionOfThisPackageDeclaredInAnotherFileIsNotFound(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	caller := found.place(test, "Caller")
	elsewhere := found.identity("stop")
	elsewhere.File = found.file + ".other"
	host := &fakeHost{
		standing:  map[fact_reach.Place]bool{caller: true},
		places:    []fact_reach.Place{caller},
		functions: []fact_reach.Function{{Object: "facts.stop", Identity: elsewhere, NoReturn: true}},
		users:     map[fact_reach.Object][]fact_reach.Place{"facts.stop": {caller}},
	}
	result, _ := settle(test, found, host)
	requireRemoved(test, result,
		fact_reach.Removal{Place: caller, Object: "facts.stop", Reason: "unknown no-return"})
}

// A question whose key the host cannot explain, and one about a parameter past the function's last, each read
// unknown, and each takes its object's users out.
func TestAQuestionTheBuildCannotPlaceReadsUnknown(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	size, scale := found.place(test, "size"), found.place(test, "scale")
	warm, reading := found.place(test, "Warm"), found.place(test, "Reading")
	unexplained := fact_reach.Question{Key: "a key of no question", Kind: purity, About: "facts.size"}
	pastTheLast, asking := purityOf("facts.scale",
		fact_reach.Asking{Function: found.identity("scale"), Parameter: 1, Encoded: notPure(test)})
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{warm: true, reading: true},
		places:   []fact_reach.Place{size, scale, warm, reading},
		asking:   asking,
		users:    map[fact_reach.Object][]fact_reach.Place{"facts.size": {warm}, "facts.scale": {reading}},
		recorded: map[fact_reach.Place][]fact_reach.Question{size: {unexplained}, scale: {pastTheLast}},
	}
	result, _ := settle(test, found, host)
	requireRemoved(test, result,
		fact_reach.Removal{Place: warm, Object: "facts.size", Reason: fact_reach.UnknownReason(purity)},
		fact_reach.Removal{Place: reading, Object: "facts.scale", Reason: fact_reach.UnknownReason(purity)})
	if result.Settlement.Unknown[purity] != 2 {
		test.Errorf("unknown answers %v, want two of %s", result.Settlement.Unknown, purity)
	}
}

// An answer of a parameter is asked of the parameter, never of its function: scale is pure and its parameter holds no
// purity, which is what the host recorded of the parameter, so Reading stands.
func TestAnAnswerOfAParameterIsAskedOfTheParameter(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	scale, reading := found.place(test, "scale"), found.place(test, "Reading")
	question, asking := purityOf("facts.scale",
		fact_reach.Asking{Function: found.identity("scale"), Parameter: 0, Encoded: notPure(test)})
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{reading: true},
		places:   []fact_reach.Place{scale, reading},
		asking:   asking,
		users:    map[fact_reach.Object][]fact_reach.Place{"facts.scale": {reading}},
		recorded: map[fact_reach.Place][]fact_reach.Question{scale: {question}},
	}
	result, resolver := settle(test, found, host)
	if bytes.Equal(buildsPurity(test, found, resolver, found.function(test, "scale")), notPure(test)) {
		test.Fatalf("the build answers scale not pure, so an answer of scale would read as its parameter's does")
	}
	requireRemoved(test, result)
	if !result.Stands(reading.File, reading.Offset) {
		test.Errorf("Reading does not stand, though the parameter's answer is the host's")
	}
}

// An answer the host read from another package, of below.Pure, is asked before any body leaves: no body of this
// package records it, and where it differs its object is named, so Total leaves with no body's question asked.
func TestAnImportedAnswerThatDiffersNamesItsObjectBeforeAnyBodyIsAsked(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	total := found.place(test, "Total")
	question, asking := purityOf("below.Pure",
		fact_reach.Asking{Function: imported("below", "Pure"), Parameter: -1, Encoded: notPure(test)})
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{total: true},
		places:   []fact_reach.Place{total},
		imported: []fact_reach.Question{question},
		asking:   asking,
		users:    map[fact_reach.Object][]fact_reach.Place{"below.Pure": {total}},
	}
	result, resolver := settle(test, found, host)
	if bytes.Equal(buildsPurity(test, found, resolver, found.importedFunction(test, "below", "Pure")),
		notPure(test)) {
		test.Fatalf("the build answers below.Pure not pure, as the host did, so the fixture differs in nothing")
	}
	requireRemoved(test, result,
		fact_reach.Removal{Place: total, Object: "below.Pure", Reason: fact_reach.DiffersReason(purity)})
	if result.ImportedAsked != 1 || result.ImportedNamed != 1 || result.Settlement.Asked != 0 {
		test.Errorf("asked %d imported answers, named %d, and the walk asked %d, want 1, 1 and none",
			result.ImportedAsked, result.ImportedNamed, result.Settlement.Asked)
	}
}

// An imported answer the walk meets again, recorded under a body that left, is the verdict it already has: the host
// is asked what the key asks once.
func TestAnImportedAnswerTheWalkMeetsAgainIsAskedOnce(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	total := found.place(test, "Total")
	question, asking := purityOf("below.Pure",
		fact_reach.Asking{Function: imported("below", "Pure"), Parameter: -1, Encoded: notPure(test)})
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{total: true},
		places:   []fact_reach.Place{total},
		imported: []fact_reach.Question{question},
		asking:   asking,
		users:    map[fact_reach.Object][]fact_reach.Place{"below.Pure": {total}},
		recorded: map[fact_reach.Place][]fact_reach.Question{total: {question}},
	}
	result, _ := settle(test, found, host)
	if result.Settlement.Asked != 1 || host.calls["Asking "+question.Key] != 1 {
		test.Errorf("the walk asked %d questions and the host was asked the key %d times, want once each",
			result.Settlement.Asked, host.calls["Asking "+question.Key])
	}
}

// deep.Pure is pure where it is declared, but exported from two imports down, so no fact on it reaches this package
// and its readers here hold none: the build's answer is the kind's answer of no fact, which is what a host two imports
// up held too, so Total stands.
func TestAFactThatNeverReachesThePackageIsAnsweredAsNoFact(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	total := found.place(test, "Total")
	question, asking := purityOf("deep.Pure",
		fact_reach.Asking{Function: imported("deep", "Pure"), Parameter: -1, Encoded: notPure(test)})
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{total: true},
		places:   []fact_reach.Place{total},
		imported: []fact_reach.Question{question},
		asking:   asking,
		users:    map[fact_reach.Object][]fact_reach.Place{"deep.Pure": {total}},
	}
	result, resolver := settle(test, found, host)
	if bytes.Equal(buildsPurity(test, found, resolver, found.importedFunction(test, "deep", "Pure")),
		notPure(test)) {
		test.Fatalf("deep answers its own Pure not pure, so asking past what reaches the package would read equal too")
	}
	requireRemoved(test, result)
	if !result.Stands(total.File, total.Offset) || result.ImportedAsked != 1 || result.ImportedNamed != 0 {
		test.Errorf("Total stands %t, with %d imported answers asked and %d named, want standing, 1 and 0",
			result.Stands(total.File, total.Offset), result.ImportedAsked, result.ImportedNamed)
	}
}

// An object whose answers hang on the order the host walked the package in takes its users out with the host's own
// reason, before anything is asked, and the host's list is not appended to.
func TestAnOrderBoundObjectTakesItsUsersOut(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	warm, caller := found.place(test, "Warm"), found.place(test, "Caller")
	orderBound := make([]fact_reach.Named, 1, 4)
	orderBound[0] = fact_reach.Named{Object: "facts.size", Reason: "order"}
	host := &fakeHost{
		standing:   map[fact_reach.Place]bool{warm: true, caller: true},
		places:     []fact_reach.Place{warm, caller},
		orderBound: orderBound,
		functions:  []fact_reach.Function{{Object: "facts.stop", Identity: found.identity("stop")}},
		users:      map[fact_reach.Object][]fact_reach.Place{"facts.size": {warm}, "facts.stop": {caller}},
	}
	result, _ := settle(test, found, host)
	requireRemoved(test, result,
		fact_reach.Removal{Place: warm, Object: "facts.size", Reason: "order"},
		fact_reach.Removal{Place: caller, Object: "facts.stop", Reason: "no-return"})
	if spare := orderBound[:2][1]; spare != (fact_reach.Named{}) {
		test.Errorf("the host's own list of order-bound objects was appended to: %v", spare)
	}
}

// A package the checker hands no host's side of holds nothing standing, and so does no result at all.
func TestWithNoHostNothingStands(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	warm := found.place(test, "Warm")
	result, _ := settle(test, found, nil)
	if result.Partial || result.Settlement != nil || result.Stands(warm.File, warm.Offset) {
		test.Errorf("with no host the result is partial %t, settlement %v, Warm standing %t, want nothing",
			result.Partial, result.Settlement, result.Stands(warm.File, warm.Offset))
	}
	var none *standing_facts.Result
	if none.Stands(warm.File, warm.Offset) {
		test.Errorf("Warm stands in no result at all")
	}
}

// A package the build lints whole asks nothing of its host past whether anything stands, whatever the host holds, and
// nothing stands in it.
func TestAPackageLintedWholeAsksNothing(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	size, warm := found.place(test, "size"), found.place(test, "Warm")
	question, asking := purityOf("facts.size",
		fact_reach.Asking{Function: found.identity("size"), Parameter: -1, Encoded: notPure(test)})
	host := &fakeHost{
		whole:     true,
		standing:  map[fact_reach.Place]bool{warm: true},
		places:    []fact_reach.Place{size, warm},
		functions: []fact_reach.Function{{Object: "facts.stop", Identity: found.identity("stop")}},
		imported:  []fact_reach.Question{question},
		asking:    asking,
		users:     map[fact_reach.Object][]fact_reach.Place{"facts.size": {warm}},
		recorded:  map[fact_reach.Place][]fact_reach.Question{size: {question}},
	}
	result, _ := settle(test, found, host)
	if want := map[string]int{"Standing": 1}; !reflect.DeepEqual(host.calls, want) {
		test.Errorf("the pass called its host %v, want %v", host.calls, want)
	}
	if result.Partial || result.Settlement != nil || result.Stands(warm.File, warm.Offset) {
		test.Errorf("linted whole, the result is partial %t, settlement %v, Warm standing %t, want nothing",
			result.Partial, result.Settlement, result.Stands(warm.File, warm.Offset))
	}
}
