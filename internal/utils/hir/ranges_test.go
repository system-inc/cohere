package hir

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// rangesFor lowers one source, constructs single-assignment form, and returns the outermost
// function together with its ranges.
//
// Typed rather than plain, because a lowering built without a checker resolves every free reference
// as a global and never emits `StoreContext`, which is the one widening rule this pass can apply.
// A plain harness would make `TestRangesWidenAStoreIntoACapturedBinding` pass vacuously; that is
// measured rather than assumed, and the corpus probe that found it is recorded in the report.
func rangesFor(t *testing.T, source string) (*Function, *MutableRanges) {
	t.Helper()

	var outermost *Function
	probe := rule.Rule{
		Name:             "ranges-harness",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ctx.TypeChecker == nil {
						t.Fatal("the typed harness handed this probe a nil checker, so every " +
							"assertion below would pass vacuously")
					}
					forEachFunctionLike(node, func(functionNode *ast.Node) {
						if outermost != nil {
							return
						}
						function := Lower(functionNode, ctx.TypeChecker)
						if function == nil {
							return
						}
						Construct(function)
						outermost = function
					})
				},
			}
		},
	}
	ruletest.RunTypedFiles(t, probe, map[string]string{"fixture.ts": source}, "fixture.ts")
	if outermost == nil {
		t.Fatal("no function was lowered, so this test measured nothing")
	}
	return outermost, InferMutableRanges(outermost)
}

// rangeOfName returns the range of the value a named binding takes, choosing the LAST definition.
//
// Single-assignment form gives one binding several identifiers, so a name alone is ambiguous. The
// last one is chosen because these tests ask about a value after its final write, which is the
// question a consumer asks.
func rangeOfName(t *testing.T, function *Function, name string) MutableRange {
	t.Helper()
	ranges := InferMutableRanges(function)
	var best MutableRange
	var found bool
	for _, identifier := range function.Identifiers {
		if identifier == nil || identifier.Name != name {
			continue
		}
		r := ranges.Get(identifier.Id)
		if !r.IsSet() {
			continue
		}
		if !found || r.Start > best.Start {
			best = r
			found = true
		}
	}
	if !found {
		t.Fatalf("no value named %q carries a range; the lowering probably names it differently", name)
	}
	return best
}

// TestRangesOpenAtTheDefiningInstruction is the base case: a value's range starts where it is
// defined and ends one past it, which is upstream's unconditional lvalue rule.
func TestRangesOpenAtTheDefiningInstruction(t *testing.T) {
	function, ranges := rangesFor(t, `
export function f() {
  const a = 1;
  return a;
}
`)
	if ranges.Len() == 0 {
		t.Fatal("no value got a range, so this pass is inert rather than correctly silent")
	}
	r := rangeOfName(t, function, "a")
	if !r.IsSet() {
		t.Fatal("`a` has no range")
	}
	if r.End != r.Start+1 {
		t.Errorf("a value defined once and never mutated should span exactly one instruction, "+
			"got [%d,%d)", r.Start, r.End)
	}
}

// TestRangesAreHalfOpen pins React's `inRange`: the start is inside the range and the end is not.
//
// This is the property that separates an interval from a set of endpoints, and it is asserted
// directly rather than through a consumer because every consumer would hide it behind its own
// question.
func TestRangesAreHalfOpen(t *testing.T) {
	r := MutableRange{Start: 3, End: 6}
	for _, order := range []EvaluationOrder{3, 4, 5} {
		if !r.Contains(order) {
			t.Errorf("[3,6) should contain %d", order)
		}
	}
	for _, order := range []EvaluationOrder{2, 6, 7} {
		if r.Contains(order) {
			t.Errorf("[3,6) should not contain %d; the end is exclusive, which is React's inRange", order)
		}
	}
	if (MutableRange{}).Contains(0) || (MutableRange{}).Contains(5) {
		t.Error("the unset range must contain nothing, so a caller need not check IsSet first")
	}
}

// TestRangesSpanTheGapBetweenDisjointWrites is the interval-versus-set proof.
//
// A value written at two positions with untouched instructions between them has ONE interval
// covering both plus the gap, not two disjoint spans. Upstream's representation is `{start, end}`
// and its membership test is a comparison, so the gap is inside the range by construction.
//
// This is asserted over a hand-built range rather than over lowered source, deliberately. The
// widening that produces a two-write range is `AliasingState.mutate`, which needs effect facts this
// tree does not have, so no source input can produce the shape today. Building it by hand is what
// makes the REPRESENTATION testable independently of the pass that would populate it, and it is the
// case that would silently pass under a set-of-indices implementation while answering upstream's
// actual question wrongly.
func TestRangesSpanTheGapBetweenDisjointWrites(t *testing.T) {
	// Written at 3, written again at 20, nothing between. Upstream's end is one past the last
	// write.
	r := MutableRange{Start: 3, End: 21}

	if !r.Contains(10) {
		t.Error("an instruction in the GAP between two writes must be inside the range: upstream " +
			"represents this as one interval, so a reactive scope covering the value covers " +
			"everything between its writes. A set of write positions would answer false here and " +
			"would pass every straight-line test in this file")
	}
	if !r.Contains(3) || !r.Contains(20) {
		t.Error("both write positions must be inside the range")
	}
	if r.Contains(21) {
		t.Error("the end stays exclusive even for a widened range")
	}
}

// TestRangesRejectAnInvalidInterval pins upstream's `validateMutableRange` invariant: a range is
// either entirely unset or genuinely non-empty.
func TestRangesRejectAnInvalidInterval(t *testing.T) {
	valid := []MutableRange{{}, {Start: 1, End: 2}, {Start: 5, End: 100}}
	for _, r := range valid {
		if !r.IsValid() {
			t.Errorf("[%d,%d) should be valid", r.Start, r.End)
		}
	}
	invalid := []MutableRange{{Start: 3, End: 3}, {Start: 5, End: 2}, {Start: 4, End: 0}}
	for _, r := range invalid {
		if r.IsValid() {
			t.Errorf("[%d,%d) should be invalid: upstream asserts (start==0 && end==0) || end>start",
				r.Start, r.End)
		}
	}
}

// TestRangesProduceNoInvalidIntervalsOnRealCode runs the invariant over a function with branches,
// a loop, and phis, which is where an off-by-one in the phi rule would show up.
func TestRangesProduceNoInvalidIntervalsOnRealCode(t *testing.T) {
	function, ranges := rangesFor(t, `
export function f(c: boolean, items: number[]) {
  let total = 0;
  for (const item of items) {
    if (c) {
      total = total + item;
    } else {
      total = total - item;
    }
  }
  return total;
}
`)
	if ranges.Len() == 0 {
		t.Fatal("no value got a range over a function with a loop and a branch; this is inert")
	}
	if invalid := ValidateMutableRanges(ranges); len(invalid) != 0 {
		t.Errorf("%d values carry an invalid range: %v", len(invalid), invalid)
	}
	_ = function
}

// TestRangesWidenAStoreIntoACapturedBinding pins the ONE widening rule that survives the absence of
// effect facts.
//
// `n` is declared in `outer` and written from a nested arrow, which lowers to `StoreContext`. React
// widens the stored value's end from the instruction shape alone, with no effect consulted, at
// development-bundle lines 42002-42004 and oxc lines 960-967.
//
// This is also the test that catches a plain harness: with a nil checker `captureOf` cannot resolve
// and no `StoreContext` is emitted at all, so the assertion below would have nothing to find.
func TestRangesWidenAStoreIntoACapturedBinding(t *testing.T) {
	function, _ := rangesFor(t, `
export function outer() {
  let n = 0;
  const bump = () => { n = 1; };
  bump();
  return n;
}
`)

	var stores int
	var walk func(*Function)
	walk = func(f *Function) {
		ranges := InferMutableRanges(f)
		for _, block := range f.Blocks {
			for _, instructionId := range block.Instructions {
				instruction := f.Instructions[instructionId]
				if instruction == nil {
					continue
				}
				store, ok := instruction.Value.(*StoreContext)
				if !ok {
					continue
				}
				stores++
				r := ranges.Get(store.Value.Identifier)
				if !r.Contains(instruction.Order) {
					t.Errorf("the value stored into a captured binding at order %d is not inside "+
						"its own range [%d,%d); the StoreContext widening did not fire",
						instruction.Order, r.Start, r.End)
				}
			}
		}
		for _, nested := range f.Functions {
			walk(nested)
		}
	}
	walk(function)

	if stores == 0 {
		t.Fatal("no StoreContext was lowered, so this test measured nothing. That is what a " +
			"checker-less harness produces, and it is why rangesFor asserts the checker is present")
	}
}

// TestRangesPhiOpensBeforeItsBlockWhenWidened proves the phi branch works, by seeding the state
// that only Stage 1 can produce today.
//
// `phiOpensBefore` is inert on this tree because its input is a widened `End` that only
// `AliasingState.mutate` writes. The branch is kept live rather than deleted, so it needs a test
// that does not depend on the missing input. Seeding the range by hand is what separates "this
// branch is correct and waiting" from "this branch is untested".
func TestRangesPhiOpensBeforeItsBlockWhenWidened(t *testing.T) {
	if phiOpensBefore(MutableRange{}, 10) {
		t.Error("an unset range must not open a phi early")
	}
	if phiOpensBefore(MutableRange{Start: 5, End: 10}, 10) {
		t.Error("a range ending exactly at the block's first instruction is not mutated AFTER it; " +
			"upstream's test is strictly greater")
	}
	if !phiOpensBefore(MutableRange{Start: 5, End: 11}, 10) {
		t.Error("a range ending past the block's first instruction means the phi is mutated after " +
			"creation, so it must open early. This is the branch Stage 1 will make reachable")
	}
	if phiOpensBefore(MutableRange{Start: 1, End: 99}, 0) {
		t.Error("a block with no assigned order must not open a phi at a negative position")
	}
}

// TestRangesAreIdempotent pins that a second run produces an equal table.
//
// `Construct` is famously NOT idempotent in this package, and a pass built beside it inherits the
// suspicion. Every guard here tests the range this pass itself wrote, so a second run reads what
// the first wrote and changes nothing.
func TestRangesAreIdempotent(t *testing.T) {
	function, first := rangesFor(t, `
export function f(c: boolean) {
  let v = 1;
  if (c) { v = 2; }
  return v;
}
`)
	if first.Len() == 0 {
		t.Fatal("the first run produced no ranges, so idempotence here is vacuous")
	}
	second := InferMutableRanges(function)
	if second.Len() != first.Len() {
		t.Fatalf("a second run produced %d ranges against %d", second.Len(), first.Len())
	}
	for id, want := range first.ranges {
		if got := second.Get(id); got != want {
			t.Errorf("value %d moved from [%d,%d) to [%d,%d) on a second run",
				id, want.Start, want.End, got.Start, got.End)
		}
	}
}

// TestRangesRequireEvaluationOrder pins that an unnumbered graph yields nothing rather than
// nonsense.
//
// Opening a range at order zero would write the UNSET value, which reads back as "no range" — a
// wrong answer wearing the shape of a right one. The pass skips instead, so a caller who forgot
// `Finalize` gets an empty table they can notice.
func TestRangesRequireEvaluationOrder(t *testing.T) {
	function, ranges := rangesFor(t, `
export function f() {
  const a = 1;
  return a;
}
`)
	if ranges.Len() == 0 {
		t.Fatal("the numbered control produced nothing, so the comparison below is meaningless")
	}

	for _, instruction := range function.Instructions {
		if instruction != nil {
			instruction.Order = 0
		}
	}
	unnumbered := InferMutableRanges(function)
	if unnumbered.Len() != 0 {
		t.Errorf("an unnumbered graph produced %d ranges; opening at order zero writes the unset "+
			"value and reads back as absent, which is a wrong answer that looks right",
			unnumbered.Len())
	}
}

// TestRangeGapsAreNamed pins the declared gaps, so closing one is a visible event.
//
// Modelled on `TestReactiveGapsAreNamed` deliberately: the two passes decline for the same missing
// input, and a reader comparing them should find the same shape.
func TestRangeGapsAreNamed(t *testing.T) {
	gaps := RangeGaps()
	if len(gaps) != 2 {
		t.Fatalf("expected exactly the two declared gaps, got %d", len(gaps))
	}
	if gaps[0] != RangeGapMutationExtension || gaps[1] != RangeGapAliasPropagation {
		t.Errorf("the declared gaps changed; if one was closed, update the package comment and the "+
			"RangeGap constants together, got %v", gaps)
	}
}

// TestRangesAreAnUnderApproximation pins the DIRECTION of the gap.
//
// Upstream widens `o`'s range to cover the mutation at `mutate(o)`, because the call's effect says
// the argument is mutated. This pass cannot see that, so `o`'s range ends at its definition. The
// test asserts the SHORTER answer, which is the honest expectation, and names the input so that a
// later pass filling effects turns this red and forces the package comment to be revisited.
func TestRangesAreAnUnderApproximation(t *testing.T) {
	function, _ := rangesFor(t, `
declare function mutate(target: number[]): void;
export function f() {
  const o: number[] = [];
  mutate(o);
  return o;
}
`)
	r := rangeOfName(t, function, "o")
	if !r.IsSet() {
		t.Fatal("`o` has no range at all, so this measured nothing")
	}
	if r.End > r.Start+1 {
		t.Errorf("`o`'s range is [%d,%d), which is WIDER than its definition point. That means "+
			"mutation extension is now being computed, which is upstream's answer and a real "+
			"improvement, but RangeGapMutationExtension and the package comment now describe a gap "+
			"that no longer exists; update both", r.Start, r.End)
	}
}

// TestMutationSitesNamesWhatStageOneWouldWiden pins the seam, two-sided: the definite shapes are
// found and an ordinary definition is not mistaken for one.
func TestMutationSitesNamesWhatStageOneWouldWiden(t *testing.T) {
	function, _ := rangesFor(t, `
declare function use(x: unknown): void;
export function f(o: {a: number}, k: string) {
  o.a = 1;
  (o as any)[k] = 2;
  use(o);
  const plain = 3;
  return plain;
}
`)
	counts := map[MutationSiteKind]int{}
	for _, site := range MutationSites(function) {
		counts[site.Kind]++
	}
	if counts[MutationSitePropertyStore] == 0 {
		t.Error("a property store was not named as a mutation site")
	}
	if counts[MutationSiteComputedStore] == 0 {
		t.Error("a computed store was not named as a mutation site")
	}
	if counts[MutationSiteCall] == 0 {
		t.Error("a call was not named as a mutation site; calls are the CEILING half of the gap " +
			"and are counted separately rather than omitted")
	}

	// The two-sided half: a function that mutates nothing names no definite site. Without this a
	// MutationSites that returned every instruction would pass the assertions above.
	clean, _ := rangesFor(t, `
export function g() {
  const a = 1;
  const b = a + 1;
  return b;
}
`)
	for _, site := range MutationSites(clean) {
		if site.Kind != MutationSiteCall {
			t.Errorf("a function with no stores named a %s site", site.Kind)
		}
	}
}

// TestRangesLeaveParametersUnset pins that a value this function did not define gets no range.
//
// Upstream opens a range only at an INSTRUCTION LVALUE. A parameter arrives already defined, so
// nothing opens one for it, and `state.create` records it in the aliasing graph without touching
// `mutableRange`. A pass that opened ranges at operand positions instead would give every parameter
// a one-instruction range at its first USE, which is a different and wrong analysis: it would claim
// the parameter is still being written at the point it is merely read.
//
// This is the fixture a mutation sweep demanded. Inverting the `PlaceRoleDefine` filter in the
// lvalue loop survived the whole suite, and the distinguishing input is exactly this shape:
// measured, parameter `p` moves from unset to [1,2) under that mutant while every other assertion
// in this file stays green, because every OTHER value is defined by an instruction before it is
// used and so its range is already open by the time the operand loop sees it.
func TestRangesLeaveParametersUnset(t *testing.T) {
	function, ranges := rangesFor(t, `
export function f(p: number) {
  const a = p + 1;
  return a;
}
`)
	if ranges.Len() == 0 {
		t.Fatal("no value got a range, so the negative assertion below is vacuous")
	}

	var checked int
	for _, identifier := range function.Identifiers {
		if identifier == nil || identifier.Name != "p" {
			continue
		}
		checked++
		if r := ranges.Get(identifier.Id); r.IsSet() {
			t.Errorf("parameter `p` (value %d) carries range [%d,%d); upstream opens a range only "+
				"at an instruction lvalue, and a parameter is defined by the call rather than by "+
				"any instruction in this function", identifier.Id, r.Start, r.End)
		}
	}
	if checked == 0 {
		t.Fatal("no value named `p` was found, so this test measured nothing")
	}
}

// TestRangeIsSetTreatsEitherFieldAsSet pins that a range with only an end is SET.
//
// This is a unit assertion rather than one over lowered source, deliberately. The state it
// describes - an end without a start - cannot be produced by this pass today: measured two-sided
// over the corpus, 145,113 values carry a set range and 0 carry an end without a start, because the
// two writers of `End` both set `Start` alongside it. Stage 1's mutation extension is a writer that
// does not, so the state becomes reachable then.
//
// Asserting it now is what keeps `IsSet` honest through that change. A mutation reducing it to
// `Start != 0` survived the whole suite, because nothing could construct the input that separates
// the two spellings; the unit test can, and it costs one line.
func TestRangeIsSetTreatsEitherFieldAsSet(t *testing.T) {
	if (MutableRange{}).IsSet() {
		t.Error("the zero range must not read as set")
	}
	if !(MutableRange{Start: 3, End: 4}).IsSet() {
		t.Error("a fully set range must read as set")
	}
	if !(MutableRange{Start: 0, End: 4}).IsSet() {
		t.Error("a range carrying only an END must read as set. Upstream's unset test is " +
			"`start === 0 && end === 0`, both fields, and a spelling that reads only Start would " +
			"report a value widened by Stage 1 as having no range at all")
	}
	if !(MutableRange{Start: 3, End: 0}).IsSet() {
		t.Error("a range carrying only a START must read as set")
	}
}

// TestBlockFirstOrderFallsBackToTheTerminal pins upstream's `?? block.terminal.id`.
//
// An empty block is common in this graph - measured, 4,546 of 28,335 blocks, and 1,198 of those
// carry phis - so the fallback runs constantly. What it cannot yet change is an ANSWER, because the
// only reader is `phiOpensBefore`, whose own precondition needs a widened end that Stage 1 has not
// landed. A mutation replacing the fallback with zero therefore survived every fixture over lowered
// source.
//
// Tested directly against a hand-built block for that reason. Returning zero here would make
// `phiOpensBefore` decline every phi in an empty block once Stage 1 makes it live, which is a
// silent under-report in exactly the shape - a merge point with no instructions - that a loop
// header most often has.
func TestBlockFirstOrderFallsBackToTheTerminal(t *testing.T) {
	function := &Function{}
	withInstruction := &BasicBlock{
		Id:           1,
		Instructions: []InstructionId{0},
		Terminal:     &Goto{Order: 99},
	}
	function.Instructions = []*Instruction{{Id: 0, Order: 7}}
	if got := blockFirstOrder(function, withInstruction); got != 7 {
		t.Errorf("a block with instructions must report its FIRST instruction's order, got %d", got)
	}

	empty := &BasicBlock{Id: 2, Terminal: &Goto{Order: 42}}
	if got := blockFirstOrder(function, empty); got != 42 {
		t.Errorf("an empty block must fall back to its TERMINAL's order, which is upstream's "+
			"`block.instructions.at(0)?.id ?? block.terminal.id`, got %d. Zero would read as "+
			"unassigned and make phiOpensBefore decline every phi in a merge block", got)
	}
}

// TestMutationSitesNameTheMutatedValueNotTheStoredOne pins WHICH value a site names.
//
// `o.a = v` mutates `o`, not `v`. Upstream's effect for it is `Mutate { value: object }`, and the
// widening Stage 1 will apply extends the RECEIVER's range. A site naming the stored value instead
// would hand Stage 1 the wrong identifier and widen a range that was never mutated, which is the
// unsafe direction: a value would look mutable past the point it settled.
//
// This exists because a mutation swapping `value.Object` for `value.Value` at the PropertyStore site
// survived every other assertion in this file. The kind assertions could not see it, which is the
// "fixtures assert the wrong layer" shape: they check that a site was found and never what it says.
func TestMutationSitesNameTheMutatedValueNotTheStoredOne(t *testing.T) {
	function, _ := rangesFor(t, `
export function f(o: {a: number}) {
  const v = 1;
  o.a = v;
  return o;
}
`)

	var checked int
	for _, site := range MutationSites(function) {
		if site.Kind != MutationSitePropertyStore {
			continue
		}
		instruction := function.Instructions[site.Instruction]
		store, ok := instruction.Value.(*PropertyStore)
		if !ok {
			t.Fatalf("a property-store site points at a %T", instruction.Value)
		}
		checked++
		if site.Target != store.Object.Identifier {
			t.Errorf("the site names value %d; a property store mutates its RECEIVER (%d), not the "+
				"value it stores (%d). Handing Stage 1 the stored value would widen the range of "+
				"something that was never mutated",
				site.Target, store.Object.Identifier, store.Value.Identifier)
		}
		if site.Target == store.Value.Identifier {
			t.Error("the site names the STORED value, which is the mutant this test exists for")
		}
	}
	if checked == 0 {
		t.Fatal("no property-store site was found, so this test measured nothing")
	}
}

// TestPhiOpensOneBeforeItsBlock pins the offset upstream uses.
//
// React writes `makeInstructionId(firstInstructionIdOfBlock - 1)`. The minus one is what puts the
// merge point itself inside the range: a phi's value exists at the TOP of the block, before its
// first instruction runs, so opening at the instruction would make `Contains` answer false at the
// position the phi is definitionally live.
//
// Tested on the helper rather than through lowered source, because the branch only runs on a range
// whose end Stage 1 widened. A mutation dropping the minus one survived every fixture over real
// code for exactly that reason.
func TestPhiOpensOneBeforeItsBlock(t *testing.T) {
	widened := MutableRange{Start: 0, End: 20}
	opened, moved := phiOpenedRange(widened, 10)
	if !moved {
		t.Fatal("a phi whose end is past its block's first instruction must open early")
	}
	if opened.Start != 9 {
		t.Errorf("the phi opened at %d; upstream opens at the block's first instruction MINUS ONE, "+
			"which is 9 here, so that the merge point itself is inside the range", opened.Start)
	}
	if !opened.Contains(9) || !opened.Contains(10) {
		t.Error("both the merge point and the block's first instruction must be inside the range")
	}

	if _, moved := phiOpenedRange(MutableRange{Start: 5, End: 20}, 10); moved {
		t.Error("a phi whose start is already set must not be reopened")
	}
	if _, moved := phiOpenedRange(MutableRange{}, 10); moved {
		t.Error("an unset range must not open a phi early")
	}
}
