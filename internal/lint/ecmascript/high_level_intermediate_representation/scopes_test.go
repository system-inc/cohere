package high_level_intermediate_representation

import (
	"testing"

	"github.com/system-inc/cohere/modules/mutation_aliasing"
	"github.com/system-inc/cohere/modules/static_single_assignment"
)

// scopesFor lowers one source and returns the outermost function with its scopes.
//
// Typed for the reason `disjointFor` is: a checker-less lowering never emits `StoreContext`, so a
// plain harness would make several assertions below pass vacuously.
func scopesFor(test *testing.T, source string) (*Function, *ReactiveScopes) {
	test.Helper()
	function, ranges := rangesFor(test, source)
	set := FindDisjointMutableValuesWithRanges(function, ranges)
	return function, AssignReactiveScopesWithSets(function, ranges, set)
}

// ---------------------------------------------------------------------------
// The hull, measured against React's own merge loop
// ---------------------------------------------------------------------------

// TestScopeHullMatchesReact pins the range merge against React's real implementation.
//
// These ten cases were not invented. They were run through React 7.1.1's own merge loop -- the body
// of `inferReactiveScopeVariables` at bundle line 32235, reached by copying the development bundle
// and appending an export inside its IIFE -- and this table is the output that produced.
//
// The cases exist to separate implementations rather than to confirm one. A plain
// `min(start), max(end)` passes `simpleHull`, `singleton`, `identicalRanges` and
// `laterStartsHigher`, and fails every case involving an unset member, because zero is the unset
// sentinel and a naive minimum drags the hull's start to zero. `firstUnset` and `runOfUnsets`
// separate the adoption branch from the skip branch, which are two different behaviours sharing one
// `if/else if`.
func TestScopeHullMatchesReact(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		ranges []mutation_aliasing.MutableRange
		want   mutation_aliasing.MutableRange
	}{
		{
			"simpleHull",
			[]mutation_aliasing.MutableRange{{Start: 3, End: 5}, {Start: 1, End: 9}, {Start: 7, End: 8}},
			mutation_aliasing.MutableRange{Start: 1, End: 9},
		},
		{
			"firstUnset",
			[]mutation_aliasing.MutableRange{{Start: 0, End: 0}, {Start: 4, End: 6}, {Start: 2, End: 9}},
			mutation_aliasing.MutableRange{Start: 2, End: 9},
		},
		{
			"middleUnset",
			[]mutation_aliasing.MutableRange{{Start: 4, End: 6}, {Start: 0, End: 0}, {Start: 2, End: 9}},
			mutation_aliasing.MutableRange{Start: 2, End: 9},
		},
		{
			"allUnset",
			[]mutation_aliasing.MutableRange{{Start: 0, End: 0}, {Start: 0, End: 0}},
			mutation_aliasing.MutableRange{Start: 0, End: 0},
		},
		{
			"runOfUnsets",
			[]mutation_aliasing.MutableRange{{Start: 0, End: 0}, {Start: 0, End: 0}, {Start: 5, End: 7}},
			mutation_aliasing.MutableRange{Start: 5, End: 7},
		},
		{
			"singleton",
			[]mutation_aliasing.MutableRange{{Start: 3, End: 7}},
			mutation_aliasing.MutableRange{Start: 3, End: 7},
		},
		{
			"identicalRanges",
			[]mutation_aliasing.MutableRange{{Start: 2, End: 4}, {Start: 2, End: 4}},
			mutation_aliasing.MutableRange{Start: 2, End: 4},
		},
		{
			"laterStartsHigher",
			[]mutation_aliasing.MutableRange{{Start: 2, End: 4}, {Start: 6, End: 9}},
			mutation_aliasing.MutableRange{Start: 2, End: 9},
		},
		{
			"trailingUnsetOnly",
			[]mutation_aliasing.MutableRange{{Start: 2, End: 4}, {Start: 0, End: 0}},
			mutation_aliasing.MutableRange{Start: 2, End: 4},
		},
		{
			"unsetEndOnlyGuard",
			[]mutation_aliasing.MutableRange{{Start: 5, End: 9}, {Start: 0, End: 0}, {Start: 0, End: 0}},
			mutation_aliasing.MutableRange{Start: 5, End: 9},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			// Build a function whose identifiers carry exactly these ranges, and a set unioning
			// them into one class. Synthetic rather than lowered, because the point is to pin the
			// merge arithmetic against React's, and a lowered input cannot be made to produce an
			// arbitrary range table.
			function := &Function{}
			ranges := &mutation_aliasing.MutableRanges{}
			var members []static_single_assignment.IdentifierId
			for index, memberRange := range testCase.ranges {
				id := static_single_assignment.IdentifierId(index + 1)
				function.Identifiers = append(function.Identifiers, nil)
				ranges.Set(id, memberRange)
				members = append(members, id)
			}
			function.Identifiers = append(function.Identifiers, nil)

			set := &DisjointSet{}
			set.Union(members)

			scopes := AssignReactiveScopesWithSets(function, ranges, set)
			if scopes.Length() != 1 {
				test.Fatalf("got %d scopes, want exactly 1; the class did not form", scopes.Length())
			}
			got := scopes.RangeOf(scopes.Ids()[0])
			if got != testCase.want {
				test.Errorf("hull = [%d,%d), want [%d,%d) -- this is React's own answer for this input",
					got.Start, got.End, testCase.want.Start, testCase.want.End)
			}
		})
	}
}

// TestScopeHullIsNotAPlainMinimum is the negative control for the case above.
//
// The whole reason the merge is written as an `if/else if` rather than as `min` is the unset
// sentinel, and a reader simplifying it would not obviously be wrong. This asserts the specific
// input on which the two spellings differ, so the simplification cannot land silently.
func TestScopeHullIsNotAPlainMinimum(test *testing.T) {
	test.Parallel()

	function := &Function{Identifiers: []*Identifier{nil, nil, nil}}
	ranges := &mutation_aliasing.MutableRanges{}
	ranges.Set(1, mutation_aliasing.MutableRange{Start: 4, End: 6})
	ranges.Set(2, mutation_aliasing.MutableRange{Start: 0, End: 0})

	set := &DisjointSet{}
	set.Union([]static_single_assignment.IdentifierId{1, 2})

	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	got := scopes.RangeOf(scopes.Ids()[0])
	if got.Start == 0 {
		test.Fatal("the unset member dragged the hull's start to zero, which is a plain minimum " +
			"rather than upstream's guarded merge; zero is the UNSET sentinel, not a position")
	}
	if got != (mutation_aliasing.MutableRange{Start: 4, End: 6}) {
		test.Errorf("hull = [%d,%d), want [4,6)", got.Start, got.End)
	}
}

// ---------------------------------------------------------------------------
// One scope per class, which is the whole shape of this pass
// ---------------------------------------------------------------------------

// TestScopeIsOnePerEquivalenceClass pins the central claim: a scope names a class, not a value.
//
// Upstream keys its scope map on the disjoint set's group representative (bundle line 32235), so
// every member of a class reaches the same scope object. A pass that minted one scope per value
// would compile, would assign a scope to everything, and would be wrong in the way that matters --
// the consumer asks "are these two memoized together" and would always hear no.
func TestScopeIsOnePerEquivalenceClass(test *testing.T) {
	test.Parallel()

	function := &Function{Identifiers: []*Identifier{nil, nil, nil, nil, nil, nil}}
	ranges := &mutation_aliasing.MutableRanges{}
	for id := static_single_assignment.IdentifierId(1); id <= 5; id++ {
		ranges.Set(id, mutation_aliasing.MutableRange{
			Start: static_single_assignment.EvaluationOrder(id),
			End:   static_single_assignment.EvaluationOrder(id) + 3,
		})
	}

	set := &DisjointSet{}
	set.Union([]static_single_assignment.IdentifierId{1, 2, 3})
	set.Union([]static_single_assignment.IdentifierId{4, 5})

	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	if scopes.Length() != 2 {
		test.Fatalf("got %d scopes for 2 classes over 5 values; a scope must name a class", scopes.Length())
	}
	scopeOfOne, scopeOfTwo, scopeOfThree := scopes.ScopeOf(1), scopes.ScopeOf(2), scopes.ScopeOf(3)
	if scopeOfOne != scopeOfTwo || scopeOfTwo != scopeOfThree {
		test.Errorf("values 1,2,3 are one class but got scopes %d,%d,%d", scopeOfOne, scopeOfTwo, scopeOfThree)
	}
	if scopeOfFour, scopeOfFive := scopes.ScopeOf(4), scopes.ScopeOf(5); scopeOfFour != scopeOfFive {
		test.Errorf("values 4,5 are one class but got scopes %d,%d", scopeOfFour, scopeOfFive)
	}
	if scopes.ScopeOf(1) == scopes.ScopeOf(4) {
		test.Error("two separate classes were given one scope, which is a false merge")
	}
}

// TestScopeAbsentValueHasNoScope pins that a value outside every class gets zero rather than a scope.
//
// Upstream's `identifier.scope` stays null for a value `findDisjointMutableValues` never admitted,
// and the consumer at bundle line 45732 branches on `identifier.scope == null`. Zero is that null
// here, which is why zero is not a valid ScopeId.
func TestScopeAbsentValueHasNoScope(test *testing.T) {
	test.Parallel()

	function := &Function{Identifiers: []*Identifier{nil, nil, nil, nil}}
	ranges := &mutation_aliasing.MutableRanges{}
	ranges.Set(1, mutation_aliasing.MutableRange{Start: 1, End: 4})
	ranges.Set(2, mutation_aliasing.MutableRange{Start: 2, End: 5})

	set := &DisjointSet{}
	set.Union([]static_single_assignment.IdentifierId{1, 2})

	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	if got := scopes.ScopeOf(3); got != 0 {
		test.Errorf("an unscoped value answered scope %d, want 0; zero is the absent scope", got)
	}
	if scopes.ScopeOf(1) == 0 {
		test.Error("a scoped value answered zero, which makes the absent sentinel unreadable")
	}
}

// TestScopeMemberRangesAreRewrittenToTheHull pins upstream's second write.
//
// `identifier.mutableRange = scope.range` at bundle line 32261 is the line a reader does not expect,
// and it is not incidental: after this pass a member reports its class's span rather than its own,
// because entangled values are memoized as one unit and therefore live as one unit. A port that
// assigned scopes and left ranges alone would satisfy every other test in this file.
func TestScopeMemberRangesAreRewrittenToTheHull(test *testing.T) {
	test.Parallel()

	function := &Function{Identifiers: []*Identifier{nil, nil, nil, nil}}
	ranges := &mutation_aliasing.MutableRanges{}
	ranges.Set(1, mutation_aliasing.MutableRange{Start: 3, End: 5})
	ranges.Set(2, mutation_aliasing.MutableRange{Start: 1, End: 9})

	set := &DisjointSet{}
	set.Union([]static_single_assignment.IdentifierId{1, 2})

	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	rewritten := scopes.MemberRanges()

	want := mutation_aliasing.MutableRange{Start: 1, End: 9}
	for _, id := range []static_single_assignment.IdentifierId{1, 2} {
		if got := rewritten.Get(id); got != want {
			test.Errorf("value %d reports [%d,%d) after entanglement, want the hull [%d,%d)",
				id, got.Start, got.End, want.Start, want.End)
		}
	}

	// The input table is deliberately not modified, which is where this differs from upstream.
	if got := ranges.Get(1); got != (mutation_aliasing.MutableRange{Start: 3, End: 5}) {
		test.Errorf("the input range table was mutated to [%d,%d); this pass must leave it alone so a "+
			"caller can still see the pre-entanglement range", got.Start, got.End)
	}
	// A value with no scope is absent from the rewritten view.
	if got := rewritten.Get(3); got.IsSet() {
		test.Errorf("an unscoped value appears in the rewritten ranges as [%d,%d)", got.Start, got.End)
	}
}

// ---------------------------------------------------------------------------
// Determinism and termination
// ---------------------------------------------------------------------------

// TestScopeAssignmentIsDeterministic pins that scope ids do not move between runs.
//
// The classes come from `DisjointSet.Sets`, which sorts precisely so that a Go map's randomised
// iteration cannot leak into the answer. Ids are handed out in that order, so a scope id is stable.
// A pass whose partition is right but whose keys move between runs produces a table that differs
// every time, which is the shape that makes a cache non-reproducible without ever being wrong.
func TestScopeAssignmentIsDeterministic(test *testing.T) {
	test.Parallel()

	const source = `
		function Component(props) {
			const items = [];
			const other = {};
			items.push(props.a);
			other.value = props.b;
			return <div>{items}{other}</div>;
		}
	`
	_, first := scopesFor(test, source)
	if first.Length() == 0 {
		test.Fatal("no scopes formed, so this test measured nothing")
	}
	for attempt := 0; attempt < 8; attempt++ {
		_, again := scopesFor(test, source)
		if again.Length() != first.Length() {
			test.Fatalf("run %d produced %d scopes, first run produced %d", attempt, again.Length(), first.Length())
		}
		for index, id := range first.Ids() {
			if again.Ids()[index] != id {
				test.Fatalf("run %d assigned scope ids in a different order", attempt)
			}
			firstRange, againRange := first.RangeOf(id), again.RangeOf(id)
			if firstRange != againRange {
				test.Fatalf("run %d gave scope %d range [%d,%d), first run gave [%d,%d)",
					attempt, id, againRange.Start, againRange.End, firstRange.Start, firstRange.End)
			}
			if len(first.MembersOf(id)) != len(again.MembersOf(id)) {
				test.Fatalf("run %d gave scope %d a different membership", attempt, id)
			}
		}
	}
}

// TestScopeAssignmentIsASinglePass proves the termination argument empirically.
//
// The claim in the package comment is that there is no fixpoint here at all: one visit per class and
// one per member, both over slices fixed before the walk begins. That is a weak claim compared to
// the union-find upstream of it, and a weak claim is still worth pinning, because the failure mode
// of getting it wrong is not a wrong answer but a hang -- the same silent shape `ranges.go` and
// `disjoint.go` both prove empirically rather than assert.
//
// Counted rather than argued: every member appears in exactly one scope, and the members summed
// across scopes equal the set's size. A pass that revisited would double-count; one that iterated to
// convergence could not finish this test at all.
func TestScopeAssignmentIsASinglePass(test *testing.T) {
	test.Parallel()

	function := &Function{}
	ranges := &mutation_aliasing.MutableRanges{}
	set := &DisjointSet{}

	// 2,000 values in 200 classes of 10. Large enough that a quadratic or iterating implementation
	// is visible as a hang rather than as a slow test.
	const classes = 200
	const perClass = 10
	function.Identifiers = append(function.Identifiers, nil)
	for class := 0; class < classes; class++ {
		var members []static_single_assignment.IdentifierId
		for member := 0; member < perClass; member++ {
			id := static_single_assignment.IdentifierId(class*perClass + member + 1)
			function.Identifiers = append(function.Identifiers, nil)
			ranges.Set(id, mutation_aliasing.MutableRange{
				Start: static_single_assignment.EvaluationOrder(id),
				End:   static_single_assignment.EvaluationOrder(id) + 2,
			})
			members = append(members, id)
		}
		set.Union(members)
	}

	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	if scopes.Length() != classes {
		test.Fatalf("got %d scopes for %d classes", scopes.Length(), classes)
	}

	seen := map[static_single_assignment.IdentifierId]ScopeId{}
	total := 0
	for _, id := range scopes.Ids() {
		for _, member := range scopes.MembersOf(id) {
			if previous, already := seen[member]; already {
				test.Fatalf("value %d appears in scope %d and again in scope %d; the walk revisited",
					member, previous, id)
			}
			seen[member] = id
			total++
		}
	}
	if total != set.Size() {
		test.Errorf("scopes hold %d members but the set has %d; the walk is not one pass over each",
			total, set.Size())
	}
}

// TestScopeAssignmentIsIdempotent pins that running the pass twice changes nothing.
//
// `Construct` is not idempotent in this package, which is a trap a caller can walk into; this pass
// is, because it reads two tables and writes a third rather than mutating its input. Asserted rather
// than assumed, because the property is what makes the input table safe to reuse.
func TestScopeAssignmentIsIdempotent(test *testing.T) {
	test.Parallel()

	function, ranges := rangesFor(test, `
		function Component(props) {
			const items = [];
			items.push(props.value);
			return <div>{items}</div>;
		}
	`)
	set := FindDisjointMutableValuesWithRanges(function, ranges)

	first := AssignReactiveScopesWithSets(function, ranges, set)
	second := AssignReactiveScopesWithSets(function, ranges, set)

	if first.Length() != second.Length() {
		test.Fatalf("second run produced %d scopes, first produced %d", second.Length(), first.Length())
	}
	for _, id := range first.Ids() {
		if first.RangeOf(id) != second.RangeOf(id) {
			test.Errorf("scope %d moved between runs", id)
		}
	}
}

// TestScopeHandlesANilFunction pins the degenerate inputs.
func TestScopeHandlesANilFunction(test *testing.T) {
	test.Parallel()

	if got := AssignReactiveScopes(nil); got == nil || got.Length() != 0 {
		test.Error("a nil function must produce an empty table rather than a nil one or a panic")
	}
	var empty *ReactiveScopes
	if empty.ScopeOf(1) != 0 || empty.Length() != 0 || empty.MembersOf(1) != nil {
		test.Error("the nil table must read as empty rather than panicking")
	}
	if got := empty.RangeOf(1); got.IsSet() {
		test.Error("the nil table must answer the unset range")
	}
	if got := empty.MemberRanges(); got == nil || got.Length() != 0 {
		test.Error("the nil table's rewritten ranges must be empty rather than nil")
	}
	if got := ValidateScopes(nil, nil); got != nil {
		test.Errorf("validating nothing returned %v", got)
	}
}

// TestScopeMembersOfIsStableAcrossCalls pins that a caller reading membership twice sees the same
// slice contents, and documents why the defensive copy in the pass is not load-bearing.
//
// # A mutation removing that copy survives, and it is a genuine equivalence rather than a gap
//
// The first version of this test wrote through `MembersOf` and then asserted the disjoint set was
// unharmed. It passed, and it could not have failed: `DisjointSet.Sets` allocates a fresh outer
// slice and fresh inner slices on every call, so nothing a caller does to the result can reach the
// set. The test was asserting a property no implementation could violate, which is the shape the
// brief names as a fixture that is weaker than the property it claims to guard.
//
// The distinguishing input would have to be two readers sharing one `Sets()` result.
// `AssignReactiveScopesWithSets` calls `Sets()` itself (scopes.go, the `classes :=` line), so each
// invocation gets its own slices and no second holder exists. There is no such input, so the copy
// is equivalent to the alias and no fixture can score it.
//
// The copy is kept anyway, for the reason `disjoint.go` keeps its `PhiOperandsInOrder` read after
// measuring the same verdict: it costs one allocation per scope, it is what a caller reading
// `MembersOf` would assume, and the equivalence rests entirely on `Sets` continuing to allocate. A
// change there would make this load-bearing again with nothing to announce it. This test pins what
// is observable -- that membership is stable and complete -- rather than pretending to pin the copy.
func TestScopeMembersOfIsStableAcrossCalls(test *testing.T) {
	test.Parallel()

	function := &Function{Identifiers: []*Identifier{nil, nil, nil, nil}}
	ranges := &mutation_aliasing.MutableRanges{}
	ranges.Set(1, mutation_aliasing.MutableRange{Start: 1, End: 4})
	ranges.Set(2, mutation_aliasing.MutableRange{Start: 2, End: 5})
	ranges.Set(3, mutation_aliasing.MutableRange{Start: 3, End: 6})
	set := &DisjointSet{}
	set.Union([]static_single_assignment.IdentifierId{1, 2, 3})

	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	scope := scopes.Ids()[0]

	first := scopes.MembersOf(scope)
	if len(first) != 3 {
		test.Fatalf("got %d members, want 3", len(first))
	}
	second := scopes.MembersOf(scope)
	for index := range first {
		if first[index] != second[index] {
			test.Errorf("membership moved between reads at index %d", index)
		}
	}
	// Membership must be complete and must agree with the per-value index, which is the property a
	// consumer actually depends on.
	for _, member := range first {
		if scopes.ScopeOf(member) != scope {
			test.Errorf("value %d is listed in scope %d but indexes to %d",
				member, scope, scopes.ScopeOf(member))
		}
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// TestValidateScopesCatchesAnEmptyHull pins the branch the corpus cannot reach today.
//
// # This fixture exists because a mutation neutralising the zero-start check survived
//
// A walk over 400 files of real code measured zero empty hulls, which is the good news and is also
// why nothing in the suite could see that branch break. The branch is
// not dead: it is the guard that fires if `RangeGapLoopCarriedInversion`'s unset population ever
// grows until a whole equivalence class falls into it, at which point the hull is [0,0) and
// describes nothing.
//
// So the input is built by hand rather than lowered, because the corpus cannot produce it and the
// verdict "unreachable" would expire the moment ranges change. That is the distinguishing input the
// brief asks to be named before a fixture is written: a class every member of which has an unset
// range, which React's own merge loop hulls to [0,0) -- confirmed against the 7.1.1 bundle as the
// `allUnset` case in TestScopeHullMatchesReact.
//
// Upstream raises here (`CompilerError.invariant`, bundle line 32271). This returns the offenders
// instead, so the assertion is on which scopes are named rather than on a panic.
func TestValidateScopesCatchesAnEmptyHull(test *testing.T) {
	test.Parallel()

	// A function with real instructions, so maxInstruction is non-zero and the third condition is
	// not what fires. Without this the test would pass for the wrong reason.
	function, _ := rangesFor(test, `
		function Component(props) {
			const items = [];
			items.push(props.value);
			return <div>{items}</div>;
		}
	`)

	// Two values whose ranges are unset, unioned into one class. This is the shape an all-unset
	// loop-carried class would take.
	unsetA, unsetB := static_single_assignment.IdentifierId(1), static_single_assignment.IdentifierId(2)
	handmade := &mutation_aliasing.MutableRanges{}
	handmade.Set(unsetA, mutation_aliasing.MutableRange{Start: 0, End: 0})
	handmade.Set(unsetB, mutation_aliasing.MutableRange{Start: 0, End: 0})
	set := &DisjointSet{}
	set.Union([]static_single_assignment.IdentifierId{unsetA, unsetB})

	scopes := AssignReactiveScopesWithSets(function, handmade, set)
	if scopes.Length() != 1 {
		test.Fatalf("got %d scopes, want 1", scopes.Length())
	}
	if got := scopes.RangeOf(scopes.Ids()[0]); got.IsSet() {
		test.Fatalf("the all-unset class hulled to [%d,%d); React hulls it to [0,0)", got.Start, got.End)
	}

	invalid := ValidateScopes(function, scopes)
	if len(invalid) != 1 {
		test.Fatalf("ValidateScopes named %d scopes, want 1; the empty hull must be reported because "+
			"upstream treats it as a compiler bug and raises", len(invalid))
	}
	if invalid[0] != scopes.Ids()[0] {
		test.Errorf("ValidateScopes named scope %d, want %d", invalid[0], scopes.Ids()[0])
	}

	// Two-sided: a valid hull over the same function must not be named, or the check above would
	// pass for an implementation that condemns everything.
	valid := &mutation_aliasing.MutableRanges{}
	valid.Set(unsetA, mutation_aliasing.MutableRange{Start: 1, End: 3})
	valid.Set(unsetB, mutation_aliasing.MutableRange{Start: 2, End: 4})
	validSet := &DisjointSet{}
	validSet.Union([]static_single_assignment.IdentifierId{unsetA, unsetB})
	if named := ValidateScopes(function, AssignReactiveScopesWithSets(function, valid, validSet)); len(named) != 0 {
		test.Errorf("a valid scope was named as invalid: %v", named)
	}
}

// TestValidateScopesCatchesAStartOfZeroWithARealEnd pins the start half of the zero check.
//
// # This is the second fixture written for one survivor, and the first one was wrong
//
// A mutation neutralising `scopeRange.Start == 0` survived, and it survived again after
// `TestValidateScopesCatchesAnEmptyHull` was added for it. That test hulls to [0,0), where
// `End == 0` fires first and short-circuits the whole disjunction, so the start check never gets to
// decide the case and neutralising it changes nothing. The hypothesis about why the mutant survived
// was right about the mechanism and wrong about which input could expose it, which is precisely the
// failure the brief names.
//
// The distinguishing input is a hull whose start is zero and whose end is not: [0,5). Only the start
// check can condemn it. That is reachable through the merge whenever a member carries a range of
// [0,n) -- start at the unset sentinel, end real -- which `MutableRange.IsSet` reports as a set
// range, so nothing upstream of here rejects it.
//
// Upstream carries the two tests as independent disjuncts for this reason, and reproducing them as
// one collapsed check would be a silent narrowing.
func TestValidateScopesCatchesAStartOfZeroWithARealEnd(test *testing.T) {
	test.Parallel()

	function, _ := rangesFor(test, `
		function Component(props) {
			const items = [];
			items.push(props.value);
			return <div>{items}</div>;
		}
	`)

	ranges := &mutation_aliasing.MutableRanges{}
	ranges.Set(1, mutation_aliasing.MutableRange{Start: 0, End: 5})
	set := &DisjointSet{}
	set.Union([]static_single_assignment.IdentifierId{1})

	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	hull := scopes.RangeOf(scopes.Ids()[0])
	if hull.Start != 0 || hull.End != 5 {
		test.Fatalf("hull = [%d,%d), want [0,5); this test needs a zero start with a real end, which "+
			"is the only shape the start check alone can condemn", hull.Start, hull.End)
	}

	named := ValidateScopes(function, scopes)
	if len(named) != 1 {
		test.Fatalf("ValidateScopes named %d scopes for a hull of [0,5), want 1; a scope starting at "+
			"the unset sentinel describes no position and upstream raises on it", len(named))
	}
}

// TestValidateScopesCatchesAnEndOfZero pins the second half of the zero check separately.
//
// A hull with a real start and a zero end cannot arise from the merge -- the end only ever grows --
// but the condition is upstream's and is written as two independent tests, so it is scored as two.
func TestValidateScopesCatchesAnEndOfZero(test *testing.T) {
	test.Parallel()

	function, _ := rangesFor(test, `
		function Component(props) {
			const items = [];
			items.push(props.value);
			return <div>{items}</div>;
		}
	`)
	ranges := &mutation_aliasing.MutableRanges{}
	ranges.Set(1, mutation_aliasing.MutableRange{Start: 5, End: 0})
	set := &DisjointSet{}
	set.Union([]static_single_assignment.IdentifierId{1})

	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	if named := ValidateScopes(function, scopes); len(named) != 1 {
		test.Errorf("ValidateScopes named %d scopes for a hull ending at zero, want 1", len(named))
	}
}

// TestScopeGapsAreDeclared pins the gap list, so closing one is a visible event.
func TestScopeGapsAreDeclared(test *testing.T) {
	test.Parallel()

	gaps := ScopeGaps()
	if len(gaps) != 1 || gaps[0] != ScopeGapPostAlignmentWidening {
		test.Errorf("ScopeGaps() = %v; a change here means a gap opened or closed and the package "+
			"comment plus the consumers must be re-read", gaps)
	}
}
