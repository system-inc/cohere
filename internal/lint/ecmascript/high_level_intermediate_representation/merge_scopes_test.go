package high_level_intermediate_representation

import (
	"testing"

	"github.com/system-inc/cohere/modules/mutation_aliasing"
	"github.com/system-inc/cohere/modules/static_single_assignment"
)

// ---------------------------------------------------------------------------
// The algorithm, pinned against React's own merge pass
// ---------------------------------------------------------------------------

// mergeCase builds a synthetic function with the given scopes and instructions.
//
// Synthetic rather than lowered, for the reason `TestScopeHullMatchesReact` records: the point is to
// pin the sweep against React's, and a lowered input cannot be made to produce an arbitrary scope
// table. Each instruction gets an lvalue and optionally a use, each carrying an identifier whose
// scope and mutable range are stated outright.
type mergeCase struct {
	name string
	// scopes are [start, end) in scope-id order starting at 1.
	scopes []mutation_aliasing.MutableRange
	// instructions are {order, lvalueScope, useScope}; a scope index of 0 means no scope.
	instructions [][3]int
	terminal     static_single_assignment.EvaluationOrder
	// terminalScope, when non-zero, gives the terminal a Return carrying a place in that scope.
	// Zero leaves the terminal an Unreachable with no operands.
	terminalScope int
	// wantGroups maps each scope id to the id it should end up in.
	wantGroups map[ScopeId]ScopeId
	// wantRanges is the range each surviving scope should carry.
	wantRanges map[ScopeId]mutation_aliasing.MutableRange
}

func buildMergeCase(test *testing.T, testCase mergeCase) (*Function, *ReactiveScopes) {
	test.Helper()

	function := &Function{}
	scopes := &ReactiveScopes{
		byIdentifier: map[static_single_assignment.IdentifierId]ScopeId{},
		ranges:       map[ScopeId]mutation_aliasing.MutableRange{},
		members:      map[ScopeId][]static_single_assignment.IdentifierId{},
	}
	for index, scopeRange := range testCase.scopes {
		id := ScopeId(index + 1)
		scopes.ranges[id] = scopeRange
		scopes.order = append(scopes.order, id)
	}

	block := &BasicBlock{Id: 0, Kind: BlockKindBlock}
	function.Identifiers = append(function.Identifiers, nil)
	nextIdentifier := static_single_assignment.IdentifierId(1)

	place := func(scopeIndex int) Place {
		id := nextIdentifier
		nextIdentifier++
		function.Identifiers = append(function.Identifiers, &Identifier{Id: id})
		if scopeIndex != 0 {
			scope := ScopeId(scopeIndex)
			scopes.byIdentifier[id] = scope
			scopes.members[scope] = append(scopes.members[scope], id)
		}
		return Place{Identifier: id}
	}

	for _, spec := range testCase.instructions {
		order := static_single_assignment.EvaluationOrder(spec[0])
		lvalue := place(spec[1])
		use := place(spec[2])
		instruction := &Instruction{
			Id:     InstructionId(len(function.Instructions)),
			Order:  order,
			LValue: lvalue,
			Value:  &LoadLocal{Place: use},
		}
		function.Instructions = append(function.Instructions, instruction)
		block.Instructions = append(block.Instructions, instruction.Id)
	}
	if testCase.terminalScope != 0 {
		block.Terminal = &Return{Value: place(testCase.terminalScope), Order: testCase.terminal}
	} else {
		block.Terminal = &Unreachable{Order: testCase.terminal}
	}
	function.Blocks = append(function.Blocks, block)

	return function, scopes
}

// TestMergeMatchesReact pins the sweep against React's real implementation.
//
// These five cases were not invented. They were run through React 7.1.1's own
// `mergeOverlappingReactiveScopesHIR` -- reached by copying the development bundle, appending an
// export inside its IIFE, and linking the four peer dependencies that live only in the pnpm store --
// and the expectations below are the output that produced, transcribed.
//
// The cases exist to separate implementations rather than to confirm one, and `nestedButInterleaved`
// is the whole reason this table is here. Two scopes that nest perfectly, [1,9) and [3,5), still
// merge, because the outer one is read at instruction 4 while the inner is open. No pairwise
// range-overlap rule can produce that answer: the ranges nest, so a comparison of the two intervals
// says nothing is wrong. A port written from the natural description of this pass -- "union any two
// scopes that overlap without nesting" -- passes `overlapNonNested`, `properlyNested`, `disjoint`
// and `sameStartSameEnd`, and fails exactly this one.
func TestMergeMatchesReact(test *testing.T) {
	test.Parallel()

	cases := []mergeCase{
		{
			// Two scopes overlapping without nesting. Scope 1 ends at 5 while scope 2 is open, so
			// scope 2 is above it on the stack and both are unioned into scope 1, widened to [1,8).
			name:         "overlapNonNested",
			scopes:       []mutation_aliasing.MutableRange{{Start: 1, End: 5}, {Start: 3, End: 8}},
			instructions: [][3]int{{1, 1, 0}, {3, 2, 0}, {4, 0, 1}},
			terminal:     9,
			wantGroups:   map[ScopeId]ScopeId{1: 1, 2: 1},
			wantRanges:   map[ScopeId]mutation_aliasing.MutableRange{1: {Start: 1, End: 8}},
		},
		{
			// Properly nested with no interleaving use: nothing merges and no range moves.
			name:         "properlyNested",
			scopes:       []mutation_aliasing.MutableRange{{Start: 1, End: 9}, {Start: 3, End: 5}},
			instructions: [][3]int{{1, 1, 0}, {3, 2, 0}},
			terminal:     10,
			wantGroups:   map[ScopeId]ScopeId{1: 1, 2: 2},
			wantRanges:   map[ScopeId]mutation_aliasing.MutableRange{1: {Start: 1, End: 9}, 2: {Start: 3, End: 5}},
		},
		{
			// The case that separates this pass from a pairwise overlap rule. The ranges nest, but
			// scope 1's value is read at 4 while scope 2 sits above it, so they union. React widens
			// nothing here, because scope 2's range is already inside scope 1's.
			name:         "nestedButInterleavedUse",
			scopes:       []mutation_aliasing.MutableRange{{Start: 1, End: 9}, {Start: 3, End: 5}},
			instructions: [][3]int{{1, 1, 0}, {3, 2, 0}, {4, 0, 1}},
			terminal:     10,
			wantGroups:   map[ScopeId]ScopeId{1: 1, 2: 1},
			wantRanges:   map[ScopeId]mutation_aliasing.MutableRange{1: {Start: 1, End: 9}},
		},
		{
			// Two scopes starting together and ending together cannot nest either way, so the start
			// branch unions them directly rather than waiting for an end or a use.
			name:         "sameStartSameEnd",
			scopes:       []mutation_aliasing.MutableRange{{Start: 2, End: 6}, {Start: 2, End: 6}},
			instructions: [][3]int{{2, 1, 2}},
			terminal:     7,
			wantGroups:   map[ScopeId]ScopeId{1: 1, 2: 1},
			wantRanges:   map[ScopeId]mutation_aliasing.MutableRange{1: {Start: 2, End: 6}},
		},
		{
			// Three scopes ending at the same position with different starts. The closing order is
			// start-descending, so scope 3 closes first, then 2, then 1 -- and each of them is
			// buried under the ones that started later, so all three union. This is the case that
			// exercises `sortByStartDescending` on a real multi-scope group: 11 end positions on
			// the corpus hold more than one scope, up to 5 of them.
			name:         "threeEndTogether",
			scopes:       []mutation_aliasing.MutableRange{{Start: 1, End: 9}, {Start: 3, End: 9}, {Start: 5, End: 9}},
			instructions: [][3]int{{1, 1, 0}, {3, 2, 0}, {5, 3, 0}, {6, 0, 1}},
			terminal:     10,
			wantGroups:   map[ScopeId]ScopeId{1: 1, 2: 1, 3: 1},
			wantRanges:   map[ScopeId]mutation_aliasing.MutableRange{1: {Start: 1, End: 9}},
		},
		{
			// Two scopes sharing an end with a third nested strictly inside both. Nothing merges:
			// the inner scope closes before the shared end is reached, so when 1 and 2 close they
			// are each on top of the stack in turn. React's own answer, and it is the negative
			// control for the case above -- a shared end alone does not force a union.
			name:         "sharedEndWithInterleave",
			scopes:       []mutation_aliasing.MutableRange{{Start: 1, End: 7}, {Start: 2, End: 7}, {Start: 3, End: 5}},
			instructions: [][3]int{{1, 1, 0}, {2, 2, 0}, {3, 3, 0}},
			terminal:     8,
			wantGroups:   map[ScopeId]ScopeId{1: 1, 2: 2, 3: 3},
			wantRanges: map[ScopeId]mutation_aliasing.MutableRange{
				1: {Start: 1, End: 7},
				2: {Start: 2, End: 7},
				3: {Start: 3, End: 5},
			},
		},
		{
			// The case that makes the closing order load-bearing, minimised from the one function on
			// the corpus where reversing it changes an answer (two scopes ending at 292, starting at
			// 30 and 24). Both scopes end at 9. The later-started one is pushed second, so it sits
			// on top. Closing start-descending pops it first, while it is on top, so nothing unions
			// and both survive. Closing in the other order pops the earlier-started one out from
			// underneath, which unions them spuriously.
			//
			// React's own answer is that they stay separate. Without this case a mutation reversing
			// `sortByStartDescending` survives the entire rest of this table, because every other
			// shared-end case here either unions under both orderings or holds one scope per
			// position.
			name:         "sharedEndReversedStack",
			scopes:       []mutation_aliasing.MutableRange{{Start: 5, End: 9}, {Start: 3, End: 9}},
			instructions: [][3]int{{3, 2, 0}, {5, 1, 0}},
			terminal:     10,
			wantGroups:   map[ScopeId]ScopeId{1: 1, 2: 2},
			wantRanges:   map[ScopeId]mutation_aliasing.MutableRange{1: {Start: 5, End: 9}, 2: {Start: 3, End: 9}},
		},
		{
			// The union arrives through the terminal rather than through an instruction. Scope 1 is
			// open across the whole function and scope 2 opens inside it; the terminal at 6 reads
			// scope 1's value while scope 2 sits above it on the stack, so `visitPlace` on the
			// terminal operand is the only thing that can union them.
			//
			// Without this case a mutation deleting the terminal-place visit entirely survives. It
			// is reachable on the corpus -- 990 scoped terminal places over 400 files -- but inert
			// there, because every one of those unions is already produced by an instruction visit.
			// So this is a genuine blind spot rather than an unreachable branch, and only a
			// synthetic case can pin it.
			name:          "terminalOperandUnions",
			scopes:        []mutation_aliasing.MutableRange{{Start: 1, End: 9}, {Start: 3, End: 9}},
			instructions:  [][3]int{{1, 1, 0}, {3, 2, 0}},
			terminal:      6,
			terminalScope: 1,
			wantGroups:    map[ScopeId]ScopeId{1: 1, 2: 1},
			wantRanges:    map[ScopeId]mutation_aliasing.MutableRange{1: {Start: 1, End: 9}},
		},
		{
			// Disjoint scopes never interact: the first leaves the stack before the second is
			// pushed, so no union can see both.
			name:         "disjoint",
			scopes:       []mutation_aliasing.MutableRange{{Start: 1, End: 3}, {Start: 5, End: 8}},
			instructions: [][3]int{{1, 1, 0}, {5, 2, 0}},
			terminal:     9,
			wantGroups:   map[ScopeId]ScopeId{1: 1, 2: 2},
			wantRanges:   map[ScopeId]mutation_aliasing.MutableRange{1: {Start: 1, End: 3}, 2: {Start: 5, End: 8}},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			function, scopes := buildMergeCase(test, testCase)
			merged := MergeOverlappingReactiveScopes(function, scopes)

			for scope, want := range testCase.wantGroups {
				if got := merged.GroupOf(scope); got != want {
					test.Errorf("scope %d merged into %d, want %d -- this is React's own answer",
						scope, got, want)
				}
			}
			for scope, want := range testCase.wantRanges {
				if got := merged.RangeOf(scope); got != want {
					test.Errorf("scope %d range = [%d,%d), want [%d,%d) -- React's own answer",
						scope, got.Start, got.End, want.Start, want.End)
				}
			}
			if got, want := merged.Length(), len(testCase.wantRanges); got != want {
				test.Errorf("%d scopes survived, want %d", got, want)
			}
		})
	}
}

// TestMergeIsNotAPairwiseOverlapRule is the negative control for the case above.
//
// It asserts the specific input on which the stack discipline and the natural pairwise description
// disagree, so a reader who simplifies this pass into "union scopes whose ranges overlap without
// nesting" cannot do it silently. The two ranges here nest, so a pairwise rule finds nothing to do
// and leaves two scopes; React unions them because of the use at instruction 4.
func TestMergeIsNotAPairwiseOverlapRule(test *testing.T) {
	test.Parallel()

	function, scopes := buildMergeCase(test, mergeCase{
		scopes:       []mutation_aliasing.MutableRange{{Start: 1, End: 9}, {Start: 3, End: 5}},
		instructions: [][3]int{{1, 1, 0}, {3, 2, 0}, {4, 0, 1}},
		terminal:     10,
	})
	merged := MergeOverlappingReactiveScopes(function, scopes)

	if merged.Length() != 1 {
		test.Fatalf("got %d surviving scopes, want 1; the interleaving use did not union them, which "+
			"is what a pairwise range comparison would also conclude", merged.Length())
	}
	// And the ranges genuinely nest, which is what makes this a control rather than a duplicate.
	outer, inner := mutation_aliasing.MutableRange{Start: 1, End: 9}, mutation_aliasing.MutableRange{Start: 3, End: 5}
	if !(outer.Start <= inner.Start && inner.End <= outer.End) {
		test.Fatal("this case no longer nests, so it stopped separating the two spellings")
	}
}

// TestMergeSkipsDegenerateScopes pins upstream's guard, which is what makes the plain min/max safe.
//
// `collectScopeInfo` refuses a scope whose start equals its end, so an unset [0,0) scope never
// enters the sweep. Without that guard the plain minimum in `apply` would drag a merged group's
// start to zero, which is the failure `AssignReactiveScopes` uses an adopt-or-minimise hull to
// avoid. Both halves are asserted: the degenerate scope must survive untouched, and the real scopes
// around it must merge normally.
func TestMergeSkipsDegenerateScopes(test *testing.T) {
	test.Parallel()

	function, scopes := buildMergeCase(test, mergeCase{
		scopes:       []mutation_aliasing.MutableRange{{Start: 0, End: 0}, {Start: 1, End: 5}, {Start: 3, End: 8}},
		instructions: [][3]int{{1, 2, 1}, {3, 3, 0}, {4, 0, 2}},
		terminal:     9,
	})
	merged := MergeOverlappingReactiveScopes(function, scopes)

	if got := merged.GroupOf(1); got != 1 {
		test.Errorf("the unset scope merged into %d; it must never enter the sweep at all", got)
	}
	if got := merged.RangeOf(1); got != (mutation_aliasing.MutableRange{Start: 0, End: 0}) {
		test.Errorf("the unset scope's range moved to [%d,%d)", got.Start, got.End)
	}
	// The control: the two real scopes around it must still merge, or this test would pass on a
	// pass that simply does nothing.
	if merged.GroupOf(3) != merged.GroupOf(2) {
		test.Error("the two real scopes did not merge, so this test measured nothing")
	}
	if got := merged.RangeOf(merged.GroupOf(2)); got.Start == 0 {
		test.Errorf("a merged group took a zero start from the unset scope: [%d,%d)", got.Start, got.End)
	}
}

// TestMergeConservesMembership asserts no value is dropped or duplicated by a merge.
//
// A merge that loses members would still reach zero overlapping pairs, which is the failure this
// stage is uniquely positioned to commit, so membership is asserted separately from the ranges.
func TestMergeConservesMembership(test *testing.T) {
	test.Parallel()

	function, scopes := buildMergeCase(test, mergeCase{
		scopes:       []mutation_aliasing.MutableRange{{Start: 1, End: 5}, {Start: 3, End: 8}, {Start: 10, End: 12}},
		instructions: [][3]int{{1, 1, 0}, {3, 2, 0}, {4, 0, 1}, {10, 3, 0}},
		terminal:     13,
	})
	merged := MergeOverlappingReactiveScopes(function, scopes)

	before := map[static_single_assignment.IdentifierId]bool{}
	for _, scope := range scopes.Ids() {
		for _, member := range scopes.MembersOf(scope) {
			before[member] = true
		}
	}
	after := map[static_single_assignment.IdentifierId]bool{}
	for _, scope := range merged.Ids() {
		for _, member := range merged.MembersOf(scope) {
			if after[member] {
				test.Errorf("value %d appears in two surviving scopes", member)
			}
			after[member] = true
		}
	}
	if len(before) != len(after) {
		test.Errorf("membership changed: %d values before, %d after", len(before), len(after))
	}
	for member := range before {
		if !after[member] {
			test.Errorf("value %d was dropped by the merge", member)
		}
	}
}

// TestMergeIsASingleSweep pins the termination argument empirically.
//
// The claim in the doc comment is that this pass never revisits anything: one pass over the blocks,
// each instruction and each terminal handled exactly once. That is a stronger claim than "it
// terminates" and it is the one worth pinning, because the natural pairwise spelling of this pass
// does iterate and a reader carrying that model would add a loop.
func TestMergeIsASingleSweep(test *testing.T) {
	test.Parallel()

	testCase := mergeCase{
		scopes:       []mutation_aliasing.MutableRange{{Start: 1, End: 5}, {Start: 3, End: 8}},
		instructions: [][3]int{{1, 1, 0}, {3, 2, 0}, {4, 0, 1}, {5, 0, 2}},
		terminal:     9,
	}
	function, scopes := buildMergeCase(test, testCase)

	state := &mergeSweepState{rangeOf: scopes.RangeOf}
	state.starts, state.ends = collectScopeInfo(function, scopes)
	memberRanges := scopes.MemberRanges()
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			state.visitInstructionId(instruction.Order)
			EachInstructionPlace(instruction, func(place Place, role PlaceRole) {
				state.visitPlace(instruction.Order, place, scopes, memberRanges)
			})
		}
		state.visitInstructionId(TerminalOrder(block.Terminal))
	}

	// One visit per instruction plus one for the terminal, and not one more.
	if want := len(testCase.instructions) + 1; state.visits != want {
		test.Errorf("the sweep visited %d positions over %d instructions and one terminal; anything "+
			"above %d means something is revisiting", state.visits, len(testCase.instructions), want)
	}
	// And both queues must be fully drained, which is what makes the bound exact rather than lucky.
	if len(state.starts) != 0 || len(state.ends) != 0 {
		test.Errorf("the sweep left %d starts and %d ends unconsumed, so some scope never closed",
			len(state.starts), len(state.ends))
	}
}

// TestMergeGapsAreDeclared makes closing a gap a visible event rather than a silent improvement.
func TestMergeGapsAreDeclared(test *testing.T) {
	test.Parallel()

	gaps := MergeGaps()
	if len(gaps) != 2 {
		test.Fatalf("got %d gaps, want 2; a gap closing or opening should be a deliberate edit", len(gaps))
	}
	if gaps[0] != MergeGapPrimitiveOperandSkip || gaps[1] != MergeGapBlockScopeAlignment {
		test.Error("the declared gaps are not the two this pass documents")
	}
}

// TestMergeHandlesAnEmptyScopeTable pins the ordinary answer for a function with no entangled values.
func TestMergeHandlesAnEmptyScopeTable(test *testing.T) {
	test.Parallel()

	if got := MergeOverlappingReactiveScopes(nil, nil); got.Length() != 0 || got.Unions() != 0 {
		test.Error("a nil input produced a non-empty result")
	}
	// A scope this pass never saw must answer itself, which is what lets a caller key a map on
	// GroupOf without first asking whether the scope merged.
	var empty *MergedScopes
	if got := empty.GroupOf(7); got != 7 {
		test.Errorf("GroupOf on an empty result returned %d, want the scope itself", got)
	}
}
