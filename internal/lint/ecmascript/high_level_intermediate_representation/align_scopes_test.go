package high_level_intermediate_representation

import (
	"testing"

	"github.com/system-inc/cohere/modules/mutation_aliasing"
	"github.com/system-inc/cohere/modules/static_single_assignment"
)

func alignedRangeMap(aligned *AlignedScopes) map[ScopeId]mutation_aliasing.MutableRange {
	out := map[ScopeId]mutation_aliasing.MutableRange{}
	for _, id := range aligned.Ids() {
		out[id] = aligned.RangeOf(id)
	}
	return out
}

// TestAlignMinimisesWithoutAGuard pins the adopt-versus-minimise answer, which has differed across
// three adjacent passes and was established here by running React rather than by reading it.
//
// The rows are the ones the bundle produced on hand-built graphs; see the package comment. The point
// of pinning them synthetically is that the corpus contains no unset scope, so the corpus cannot
// distinguish a guarded implementation from an unguarded one and a future reader adding a guard
// "for safety" would be making an unmeasured change that no corpus test could see.
func TestAlignMinimisesWithoutAGuard(test *testing.T) {
	test.Parallel()

	// A construct whose terminal sits at 5 and whose fallthrough begins at 9, so an active scope
	// starting after 5 has its start minimised back to 5 and its end pushed out to 9.
	build := func(scopeRange mutation_aliasing.MutableRange) (*Function, *ReactiveScopes) {
		return buildAlignCase(test, alignCase{
			scope:        scopeRange,
			terminalId:   5,
			useAt:        7,
			fallthroughs: 9,
		})
	}

	cases := []struct {
		name  string
		start mutation_aliasing.MutableRange
		want  mutation_aliasing.MutableRange
	}{
		{
			"start after the construct is minimised back",
			mutation_aliasing.MutableRange{Start: 7, End: 15},
			mutation_aliasing.MutableRange{Start: 5, End: 15},
		},
		{
			"start already earlier is left alone",
			mutation_aliasing.MutableRange{Start: 3, End: 15},
			mutation_aliasing.MutableRange{Start: 3, End: 15},
		},
		{
			"an unset start stays zero rather than being widened",
			mutation_aliasing.MutableRange{Start: 0, End: 15},
			mutation_aliasing.MutableRange{Start: 0, End: 15},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			function, scopes := build(testCase.start)
			aligned := AlignReactiveScopesToBlockScopes(function, scopes)
			if got := aligned.RangeOf(1); got != testCase.want {
				test.Errorf("scope %v aligned to %v, want %v", testCase.start, got, testCase.want)
			}
		})
	}

	// A fully unset scope never becomes active, because activity is `end > startingId` and an end of
	// zero fails that for every block. It must come back untouched rather than widened to the
	// construct, which is what an implementation missing the activity filter would produce.
	function, scopes := build(mutation_aliasing.MutableRange{Start: 0, End: 0})
	aligned := AlignReactiveScopesToBlockScopes(function, scopes)
	if got := aligned.RangeOf(1); got != (mutation_aliasing.MutableRange{Start: 0, End: 0}) {
		test.Errorf("a fully unset scope aligned to %v; it should never become active at all", got)
	}
}

// TestAlignExcludesBranchTerminals pins the `terminal.kind !== 'branch'` exclusion, measured against
// React itself, and the structural reason it matters in this tree.
//
// It is not a corner case here: `Branch` is the largest terminal population carrying a fallthrough,
// and every fallthrough our lowering reuses is reused by a branch. So the exclusion is what makes
// upstream's "unique fallthroughs" invariant hold on our graph.
func TestAlignExcludesBranchTerminals(test *testing.T) {
	test.Parallel()

	widened := func(
		makeTerminal func(fallthroughBlock static_single_assignment.BlockId) Terminal,
	) mutation_aliasing.MutableRange {
		function, scopes := buildAlignCaseWithTerminal(test, alignCase{
			scope:        mutation_aliasing.MutableRange{Start: 4, End: 6},
			terminalId:   5,
			useAt:        4,
			fallthroughs: 9,
		}, makeTerminal)
		return AlignReactiveScopesToBlockScopes(function, scopes).RangeOf(1)
	}

	ifRange := widened(func(ft static_single_assignment.BlockId) Terminal {
		return &If{Consequent: 2, Alternate: ft, Fallthrough: ft, Order: 5}
	})
	if ifRange != (mutation_aliasing.MutableRange{Start: 4, End: 9}) {
		test.Errorf("an `if` widened to %v, want [4,9); the fallthrough push did not fire", ifRange)
	}

	branchRange := widened(func(ft static_single_assignment.BlockId) Terminal {
		return &Branch{Consequent: 2, Alternate: ft, Fallthrough: ft, Order: 5}
	})
	if branchRange != (mutation_aliasing.MutableRange{Start: 4, End: 6}) {
		test.Errorf("a `branch` widened to %v, want [4,6) unchanged; upstream excludes branch "+
			"terminals from the fallthrough widening", branchRange)
	}
}

// TestAlignPushFilterDeclinesAScopeThatEndedBeforeTheTerminal covers the `end > terminal.id` test on
// the fallthrough push, which is a filter rather than a cost optimisation.
//
// A scope reaches that line only if it was still active at the block's first instruction, so the
// distinguishing input is a scope that is active at the top of the block and has ended by the time
// the sweep reaches the terminal. Without the filter such a scope is dragged forward across a
// construct it never entered.
//
// The corpus alone could not see this: the mutant removing the filter survived the whole suite, and
// only instrumenting the sweep showed the branch declines 876 of 3,269 candidates with every one of
// them a case that would otherwise have widened. That is a fixture blind spot rather than an
// equivalence, and this is the fixture.
func TestAlignPushFilterDeclinesAScopeThatEndedBeforeTheTerminal(test *testing.T) {
	test.Parallel()

	// The scope reads at 2 and ends at 3, so it is active when the block begins (its end exceeds the
	// block's first instruction at 2) and dead by the terminal at 5. The fallthrough begins at 9.
	function, scopes := buildAlignCase(test, alignCase{
		scope:        mutation_aliasing.MutableRange{Start: 2, End: 3},
		terminalId:   5,
		useAt:        2,
		fallthroughs: 9,
	})
	aligned := AlignReactiveScopesToBlockScopes(function, scopes)
	if got := aligned.RangeOf(1); got != (mutation_aliasing.MutableRange{Start: 2, End: 3}) {
		test.Errorf("a scope that ended at 3, before the terminal at 5, aligned to %v; the "+
			"`end > terminal` filter should have declined it rather than dragging its end out to "+
			"the fallthrough", got)
	}
}

// TestAlignRecordsAScopeOnceCoversTheSeenGate pins the `seen` gate on the value-block widening.
//
// The gate is what makes the fourth mechanism fire once per scope rather than once per place. It is
// heavily exercised on the corpus -- 47,476 places arrive with their scope already seen -- and the
// mutant removing it still survived the corpus suite, because widening to the same value range twice
// is idempotent. The distinguishing input needs the scope to meet two different value ranges, so
// that applying the second one is not a no-op.
//
// Built synthetically rather than found, because that shape does not occur in the corpus: a scope
// whose first sighting is inside a narrow value block and whose second is inside a wider one.
func TestAlignRecordsAScopeOnceCoversTheSeenGate(test *testing.T) {
	test.Parallel()

	_, scopes := buildAlignCase(test, alignCase{
		scope:        mutation_aliasing.MutableRange{Start: 4, End: 6},
		terminalId:   5,
		useAt:        4,
		fallthroughs: 9,
	})

	// Two value-block nodes of different spans, both reached by the same scope. Only the first may
	// be applied; the second must be ignored because the scope has already been seen.
	state := &alignSweepState{
		activeSet:       map[ScopeId]bool{},
		seen:            map[ScopeId]bool{},
		valueBlockNodes: map[static_single_assignment.BlockId]*valueBlockNode{},
		ranges:          map[ScopeId]mutation_aliasing.MutableRange{1: {Start: 4, End: 6}},
		scopes:          scopes,
	}
	scopedPlace := Place{Identifier: 1}
	for id, scope := range scopes.byIdentifier {
		if scope == 1 {
			scopedPlace = Place{Identifier: id}
			break
		}
	}

	narrow := &valueBlockNode{start: 3, end: 7}
	wide := &valueBlockNode{start: 1, end: 30}
	state.recordPlace(4, scopedPlace, narrow)
	afterFirst := state.ranges[1]
	state.recordPlace(5, scopedPlace, wide)
	afterSecond := state.ranges[1]

	if afterFirst != (mutation_aliasing.MutableRange{Start: 3, End: 7}) {
		test.Fatalf("the first sighting widened the scope to %v, want [3,7); the value-block "+
			"mechanism did not fire and nothing below measures the gate", afterFirst)
	}
	if afterSecond != afterFirst {
		test.Errorf("a second sighting widened the scope again, from %v to %v; the `seen` gate is "+
			"supposed to make this mechanism fire once per scope rather than once per place",
			afterFirst, afterSecond)
	}
}

// TestAlignGotoWidensAcrossSeveralOpenConstructs covers the goto back-reference, the third mechanism.
//
// A goto whose target is an open fallthrough that is not the innermost one is leaving several
// constructs at once, and every still-active scope has to be widened at both ends to span the outer
// construct. When the target is the innermost open construct the pop on the next block handles it,
// which is why upstream tests for it and why removing that test is not obviously wrong.
//
// This is a genuine rarity rather than a fabricated case: instrumenting the sweep found 2,642 gotos
// targeting an open fallthrough and exactly one of them reaching a non-innermost target. So the
// corpus exercises the branch once and no assertion over totals could see it move, which is why this
// fixture is synthetic and the measurement is recorded here.
func TestAlignGotoWidensAcrossSeveralOpenConstructs(test *testing.T) {
	test.Parallel()

	function, scopes := buildNestedGotoCase(test)
	aligned := AlignReactiveScopesToBlockScopes(function, scopes)

	// The scope reads at 21, inside the inner construct, and both constructs are open when the goto
	// at 22 jumps past the inner fallthrough to the outer one. Its start must be pulled back to the
	// outer construct's terminal at 5, and its end pushed out to the outer fallthrough at 40.
	got := aligned.RangeOf(1)
	if got.Start != 5 {
		test.Errorf("the scope's start is %d, want 5; the goto back-reference should have pulled it "+
			"back to the outer construct's terminal", got.Start)
	}
	if got.End != 40 {
		test.Errorf("the scope's end is %d, want 40; the goto back-reference should have pushed it "+
			"out to the outer construct's fallthrough", got.End)
	}
}

// TestAlignGotoSkipsTheInnermostOpenConstruct covers the other half of the goto back-reference: the
// `start !== activeBlockFallthroughRanges.at(-1)` test that declines a goto to the innermost target.
//
// The first fixture for this mutant was wrong even though the mechanism was understood, which is the
// hazard the port brief names: a fixture written for a survivor is a hypothesis. A goto to a
// non-innermost target takes the same path with the test and without it, so it cannot see the
// mutation at all, and the mutant survived a re-score.
//
// What separates them is that the fallthrough pop on the following block reproduces the start
// widening and not the end one. So the distinguishing input is a scope active at an
// innermost-targeting goto whose end falls short of that fallthrough: upstream leaves it alone and
// the pop then widens only its start, while removing the test drags its end forward too.
func TestAlignGotoSkipsTheInnermostOpenConstruct(test *testing.T) {
	test.Parallel()

	function, scopes := buildInnermostGotoCase(test)
	aligned := AlignReactiveScopesToBlockScopes(function, scopes)

	// Run against React 7.1.1's own `alignReactiveScopesToBlockScopesHIR` on this exact graph, which
	// is how the expectation below was obtained. The first version of this test expected the start
	// to move to 5 as well, on the reasoning that the pop would still fire. React says otherwise and
	// the reason is the activity filter: the scope's end is 9 and the continuation block begins at
	// 20, so the scope is retired before the pop is reached and neither endpoint moves.
	//
	// That is worth keeping as a comment rather than only as an assertion. The pop and the goto
	// back-reference look like two chances to widen the same scope, and they are not: a scope narrow
	// enough to be skipped by the goto test is usually also narrow enough to be retired before the
	// pop.
	if got := aligned.RangeOf(1); got != (mutation_aliasing.MutableRange{Start: 7, End: 9}) {
		test.Errorf("the scope aligned to %v, want [7,9) unchanged; a goto to the INNERMOST open "+
			"fallthrough is skipped, and this scope is retired by the activity filter before the "+
			"fallthrough pop can reach it", got)
	}
}

// TestAlignDeclinesAPlaceReadOutsideItsScope covers upstream's `getPlaceScope` containment test.
//
// It is not subsumed by the activity filter, which is the plausible reading and the reason the
// mutant removing it survived at first. The filter retires a scope whose end has passed the current
// block; this declines a place whose read position its own scope never covered, which can happen at
// any point.
//
// Choosing the range is the whole difficulty and the first attempt was wrong. A scope of [2,4) read
// at 7 also comes back unchanged, and it cannot see this line: its end has already passed the
// block's first instruction, so the activity filter would have excluded it anyway and both spellings
// reach the same verdict by different routes. That fixture passed against the mutant.
//
// The range has to sit after the read instead. A scope of [9,12) read at 7 has an end beyond the
// block start, so nothing retires it, and only the containment test declines it. The expectation is
// React's own output on that graph rather than a derivation.
func TestAlignDeclinesAPlaceReadOutsideItsScope(test *testing.T) {
	test.Parallel()

	function, scopes := buildAlignCase(test, alignCase{
		scope:        mutation_aliasing.MutableRange{Start: 9, End: 12},
		terminalId:   8,
		useAt:        7,
		fallthroughs: 20,
	})
	aligned := AlignReactiveScopesToBlockScopes(function, scopes)
	if got := aligned.RangeOf(1); got != (mutation_aliasing.MutableRange{Start: 9, End: 12}) {
		test.Errorf("a scope of [9,12) read at 7, before its own range opens, aligned to %v; the "+
			"place should never have activated it", got)
	}
}

// TestAlignGapsAreDeclared makes closing a gap a visible event rather than a silent improvement.
func TestAlignGapsAreDeclared(test *testing.T) {
	test.Parallel()

	gaps := AlignGaps()
	if len(gaps) != 0 {
		test.Errorf("AlignGaps returned %v; a change here means a gap opened or closed and the "+
			"reasoning in align_scopes.go needs to move with it", gaps)
	}
}

// TestAlignHandlesAnEmptyScopeTable is the ordinary answer for a function with no entangled values.
func TestAlignHandlesAnEmptyScopeTable(test *testing.T) {
	test.Parallel()

	function := &Function{}
	if got := AlignReactiveScopesToBlockScopes(function, &ReactiveScopes{}); got.Length() != 0 {
		test.Errorf("an empty scope table produced %d scopes", got.Length())
	}
	if got := AlignReactiveScopesToBlockScopes(nil, nil); got.Length() != 0 {
		test.Errorf("nil inputs produced %d scopes", got.Length())
	}
	aligned, merged := AlignThenMergeReactiveScopes(nil, nil)
	if aligned.Length() != 0 || merged.Length() != 0 {
		test.Errorf("nil inputs through the composed spelling produced %d aligned and %d merged",
			aligned.Length(), merged.Length())
	}
}
