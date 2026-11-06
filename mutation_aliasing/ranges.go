// Mutable ranges: over which span of a function's evaluation a value is still being written.
//
// This is React's `InferMutationAliasingRanges.ts`, run over this graph to produce a
// `MutableRanges` table. Stage 2 built the half that reads no effects; Stage 2.5 added the half that
// does, which is the `AliasingState` graph and its `mutate` worklist at the bottom of this file. oxc
// transcribes the whole pass at
// `oxc_react_compiler/src/react_compiler_inference/infer_mutation_aliasing_ranges.rs`, 1,107 lines;
// where the two disagree React wins, and each disagreement is recorded at the line that resolves it.
// Measured across both halves: React and oxc agree line for line on `mutate` and on the graph
// builders, so no divergence between them was found here. The one divergence from both is the
// loop-carried inversion, recorded at the line in the lvalue loop.
//
// The pass lived in cohere's high-level IR until #kr2yp54 moved it here, so every measurement in this
// file was taken there, on cohere's corpus, and the instruction names it uses (`StoreContext`,
// `PropertyStore`, `FunctionExpression`) are that IR's. Graph is how either IR reaches the pass.
//
// A value's mutable range is the span of program points over which it is still being written. Once
// the range is closed, the value is settled: nothing after `end` changes it. That is the fact
// `find_disjoint_mutable_values` reads to decide which values must be memoized as one unit, and it
// is the same fact a dead-store analysis needs to say "this write is the last one and nothing read
// it". This package computes it as general infrastructure; the React consumer is one caller.
//
// # A range is an interval, not a set, and that is upstream's decision rather than a summary of it
//
// Measured rather than inferred. React represents it as `{start, end}` with membership
// `inRange({id}, range) { return id >= range.start && id < range.end; }` (development bundle line
// 32315), a half-open interval `[start, end)`. oxc's `MutableRange` carries the same two fields.
// There is no set anywhere in either implementation.
//
// The consequence is load-bearing and it is why this is stated rather than assumed. A value mutated
// at instruction 3 and again at instruction 20, untouched between, has the interval [3, 21). Every
// instruction from 4 to 19 reports as "inside the mutable range" even though nothing there writes
// it. The interval spans the gap, deliberately. Upstream relies on that: a reactive scope covering
// the value has to cover both mutations and everything between them, because splitting the scope
// would let a memoized value escape half-mutated. A set-of-indices representation would answer
// "is instruction 10 a write" correctly and answer upstream's actual question wrongly.
//
// So this reproduces the interval, and `TestRangesSpanTheGapBetweenDisjointWrites` pins the gap
// case specifically, because on straight-line code with one write the two representations are
// indistinguishable and a set would pass every other test in this file.
//
// The interval also carries an invariant upstream asserts and this reproduces: a range is either
// entirely unset (`start == 0 && end == 0`) or genuinely non-empty (`end > start`). React's
// `validateMutableRange` (line 20999) raises on anything else. `ValidateMutableRanges` below is
// that check, exposed so a caller can assert it rather than trust it.
//
// # What is computed without effect facts, and what is not, measured on the real tree
//
// `Place.Effect` is `EffectUnknown` on every place lowering produces, and Stage 1 effect inference
// is what fills it. This pass was built to find out how much of a range survives that absence
// rather than to wait for it, and the answer is specific.
//
// Upstream writes `mutableRange` at exactly two kinds of site.
//
// The definition sites need no effects at all. Every instruction lvalue opens its identifier's
// range at that instruction's evaluation order and closes it at order+1, unconditionally, in a loop
// over `eachInstructionLValue` that reads no effect (React lines 41916-41927, oxc 805-834). A phi
// whose value is mutated after creation opens at the block's first instruction minus one. This is
// the whole `start` half of the algorithm plus the initial `end`.
//
// The extension sites are where every effect fact enters, and there is only one function that
// writes a wider `end`: `AliasingState.mutate` (React 42186-42206, oxc 240-275). It is called once
// per entry in `mutations`, and `mutations` is populated exclusively from `instr.effects` matching
// `Mutate | MutateConditionally | MutateTransitive | MutateTransitiveConditionally`. No effects
// means no mutations list means `mutate` is never called means no range is ever widened past its
// definition point. The one exception is `StoreContext`, which widens `end` from the instruction
// shape alone (React 42002-42004, oxc 962-967) and is reproduced here.
//
// Measured on the corpus `lower_corpus_test.go` uses, 4,333 functions including nested ones,
// lowered and converted to single-assignment form:
//
//	values in the corpus                                313,292
//	values this pass gives a range                      145,113   46.3%
//	values it leaves unset                              168,179   parameters, globals, temporaries
//	                                                              whose defining place is not an
//	                                                              lvalue
//	values with an invalid interval                           0   upstream's own invariant
//	places carrying a non-Unknown effect                      0   the two-sided control
//
//	definite mutation sites this pass cannot widen          760   PropertyStore 646,
//	                                                              ComputedStore 114
//	call sites, a CEILING rather than a count            11,367   most mutate nothing; only an
//	                                                              effect table separates them
//
// So the definition half is computable in full today and it covers 145,113 of the 313,292 values.
// The extension half is blocked on Stage 1 at 760 sites where the instruction shape alone proves a
// mutation, plus up to 11,367 call sites where only effects can decide. Against 136,752
// instructions, the definite gap is 0.56%.
//
// One number here corrects an earlier reading of this same corpus. `StoreContext` measures zero on
// it, which reads as a rule that never fires, and it is instead an artifact of the harness: the
// corpus is lowered with a nil checker, so `captureOf` cannot resolve a captured binding and
// lowering emits `StoreGlobal` instead. Seeded with a real checker the shape appears and the
// widening fires, which `TestRangesWidenAStoreIntoACapturedBinding` pins. A zero measured through a
// checker-less lowering is not a fact about the code.
//
// # Stage 2.5 closed the extension half, and the numbers above are the before picture
//
// Everything above this heading describes the pass as it stood when only the definition half
// existed. It is kept because the measurements are real and because the seam it describes is the one
// that was then closed. What follows is what changed.
//
// Effect inference landed as `InferAliasingEffects`, supplying an `AliasingEffect` per instruction
// carrying `Kind`, `From` and `Into`. `AliasingState` is now ported below -- the directed edge graph
// and the `mutate` worklist over it -- and `InferMutableRanges` runs it before the definition half.
//
// # The headline is a distribution, and the count was the number that hid the problem
//
// Measured over 400 files of the corpus, lowered with a checker present, before and after:
//
//	                     before      after
//	setRanges            72,032      75,933
//	widerThan1               32      40,139
//	maxWidth                  2         929
//	invalid                   0           0
//
// The before column is the one worth dwelling on. `setRanges` at 72,032 reads as a healthy
// substrate and is what a count-based check would have reported as success. The number that
// mattered was `maxWidth 2`, and the 32 values reaching it were ALL from `StoreContext`, the single
// effect-free widening rule -- measured with a `storeContexts` counter that returned exactly 32.
// Every value in the table carried a valid range of width one, so every downstream predicate reading
// `end > start + 1` or `inRange` was false for a structural reason rather than a computed one.
//
// That is a specific failure shape worth naming: an analysis can be inert because its input is
// uniformly degenerate rather than absent. A zero from missing input announces itself; a zero from
// input that is present, well-formed, correct, and uniformly carrying the one value that makes every
// predicate false looks exactly like a working pass.
//
// # The direction of the error, restated now that the widening exists
//
// The remaining gap still makes a range shorter than upstream's rather than longer, for the same
// reason as before: every missing widening is a missing `max`, and `max` only grows `end`. The one
// divergence from both upstreams goes the same way rather than the other: a loop-carried value
// whose two writes produce an inverted interval is left unset rather than carrying the interval
// upstream ships, which its own validator declares invalid. Documented at the line in the lvalue
// loop and named by `RangeGapLoopCarriedInversion`, so a consumer that needs those values declines
// on them rather than reading a range this pass invented. Measured: 48 values across 400 files.
//
// # Convergence: there is a worklist now, and its termination is not a fixpoint argument
//
// The previous version of this comment said there was no fixpoint here and no convergence equality
// to hand-write. Half of that survives. The definition half still needs one ordered walk and no
// iteration. The widening half is a worklist, and its termination argument is written out in full on
// `mutate` rather than summarised here, because it is the part most likely to be got wrong: it does
// not terminate by the widening reaching a fixpoint, it terminates because the `seen` map admits
// each identifier at most once per mutation kind and there are two kinds.
//
// That distinction is load-bearing and it was verified by mutation rather than asserted: neutralising
// the `seen` comparison produces a pass that hangs rather than one that gives a wrong answer, which
// is the failure mode this tree has hit before in other passes and which does not announce itself.
//
// # Reverse postorder is the right walk here, and the reason is not the usual one
//
// `Function.Blocks` is in reverse postorder and this walks it in that order. For a convergence
// analysis that choice is about speed; here it is about correctness of the numbering rather than
// the iteration, and the distinction matters because the neighbouring hazard is real: reverse
// postorder visits a post-loop block before the loop body, so a pass asking "was A written before
// B" from block order alone gets a wrong answer.
//
// This pass never asks that question of block order. It asks it of `Instruction.Order`, which
// `MarkEvaluationOrder` assigns by walking the same reverse postorder and which is therefore a
// total order on instructions that does not depend on the walk that reads it. A consumer comparing
// two positions compares two `EvaluationOrder` values, and the back edge shows up as a decrease,
// which is the documented and correct signal. That is why this is safe where an order-sensitive
// report over blocks would not be.
package mutation_aliasing

import (
	"sort"

	"github.com/system-inc/cohere/static_single_assignment"
)

// RangeGap names a range-widening rule this pass cannot apply, for a caller that needs to know.
//
// Modelled on `ReactiveGap` deliberately: a gap answerable through the API is one a consumer can
// decline on, while a gap living only in a comment is one the next reader inherits by accident.
type RangeGap uint8

const (
	// RangeGapLoopCarriedInversion is a loop-carried value left unset because its two writes
	// produced an inverted interval.
	//
	// A loop-carried value is defined at a high evaluation order by the back-edge store, while the
	// mutation reaching it through the phi happened at a lower one. Upstream's two guards are each
	// conditioned on their own field being unset, so both writes land independently and the end
	// falls at or before the start. Measured on this corpus: 48 values across 400 files, every one
	// loop-carried.
	//
	// This pass leaves those unset rather than clamping, because the true range starts before the
	// back-edge store and how far before is exactly what the folded phi hides. A consumer that
	// needs those values must decline on them rather than read a range this pass invented. See the
	// lvalue loop for the full reasoning and `TestRangesStayValidAcrossALoopBackEdge` for the pin.
	RangeGapLoopCarriedInversion RangeGap = iota
)

// RangeGaps are the widening rules InferMutableRanges does not apply. See RangeGap.
//
// Returned as a value rather than documented alone so a test can assert on it, which makes closing
// a gap a visible event rather than a silent improvement. Stage 2.5 closed two of the three this
// list once held, and CreateFrom propagation closed later when property loads and destructuring
// began emitting the effect. Those are exactly the events the mechanism exists to make visible:
// the test naming them failed and had to be rewritten rather than quietly passing.
func RangeGaps() []RangeGap {
	return []RangeGap{RangeGapLoopCarriedInversion}
}

// InferMutableRanges computes the mutable range of every value in function, returning them as a
// table keyed by value.
//
// Requires evaluation order, which an IR's `Finalize` assigns and its `Construct` re-establishes.
// Call `Construct` first, or in cohere take a function from `ForFunction`, which does. Over a graph whose
// `Instruction.Order` is zero every range would open at zero, which is the unset value and would
// silently produce an empty table rather than a wrong one; `TestRangesRequireEvaluationOrder` pins
// that a caller who skips `Finalize` gets nothing rather than nonsense.
//
// Nested functions are not included. Upstream analyses a nested function separately, through
// `lowerWithMutationAliasing`, and then resets every context operand's range to unset
// (`analyseFunctions`, React 42290-42297). Ranges are therefore per-function by upstream's own
// construction, and a table spanning two functions would key two different values under one id -
// the identifier tables are per-function, which the brief names as a trap that has already cost an
// agent a silent drop. cohere's `RangesForNested` walks them explicitly and returns one table each.
//
// Reads the effects Graph.Effects gives. Stage 2.5 closed the widening gap this comment previously
// declined: the effects' mutations widen ranges through the alias graph, and the definition half
// then opens every range that the widening left without a start. Each IR's own InferMutableRanges
// infers its effects and hands them in through its Graph.
//
// Idempotent: every guard tests the range this pass itself wrote, so a second run over the same
// function produces an equal table. `TestRangesAreIdempotent` pins it.
//
// # The order of the two halves is upstream's and it is load-bearing
//
// Upstream runs the widening (its Part 1) entirely before the definition half (its Part 2), at React
// 41851 against 41917 and oxc 668 against 800. That is not incidental sequencing, and interleaving
// them -- which is what this file did while the widening was absent, harmlessly -- produces different
// answers once the widening is present.
//
// The reason is that every definition-half write is guarded on the field being unset. The lvalue
// loop opens `Start` only `if Start == 0` and `End` only `if End == 0`. So a value whose `End` was
// already widened past its definition point must not have that `End` overwritten by `order + 1`, and
// running the definition half first would do exactly that for any value mutated before its own
// defining instruction in evaluation order, which single-assignment form makes ordinary through
// phis and loops.
//
// Reproduced in upstream's order, with the phases split into `AliasingGraph.Widen` and the loop below.
func InferMutableRanges[G Graph[F, B, P], F any, B comparable, P any](
	graph G,
	function F,
	options Options,
) *MutableRanges {
	result := &MutableRanges{}

	// Part 1: build the alias graph and widen every range a mutation reaches.
	BuildAliasingGraph(graph, function, options).Widen(result)

	// Part 2: open a range at every definition point that the widening left unopened.
	//
	// One visitor for every place, made once per call, which reads what to do from the walk. A
	// visitor handed to a Graph method escapes, since the compiler cannot see through a type
	// parameter's method, and so does every local it captures; one per instruction would be an
	// allocation per instruction.
	walk := &definitionWalk[G, F, B, P]{graph: graph, result: result}
	visit := walk.visitPlace
	for _, block := range graph.Blocks(function) {
		// A phi's range opens at the block's first instruction minus one, but only when the phi is
		// mutated after creation, which is a fact this pass cannot establish. See the note in
		// `phiOpensBefore` for why the branch is present and inert rather than deleted.
		firstOrder := BlockFirstOrder(graph, function, block)
		for _, phi := range graph.Phis(block) {
			id := graph.IdentifierOf(phi.Place)
			existing := result.Get(id)
			if opened, ok := phiOpenedRange(existing, firstOrder); ok {
				result.Set(id, opened)
			}
		}

		count := graph.InstructionCount(function, block)
		for index := 0; index < count; index++ {
			order, ok := graph.InstructionOrder(function, block, index)
			if !ok {
				continue
			}
			if order == 0 {
				// Unnumbered, so `Finalize` never reached this block. Opening a range at zero
				// would write the unset value and read back as "no range", which is a wrong answer
				// wearing the shape of a right one. Skipping is the honest response.
				continue
			}
			walk.order = order

			// Every lvalue opens its own range here. This is upstream's unconditional loop over
			// `eachInstructionLValue` and it reads no effect: React lines 41916-41927, oxc 805-834.
			//
			// Reached through Graph's place visitor and the Define role rather than by matching
			// instruction variants, which is how `eachInstructionLValue` in cohere's reactive.go
			// already spells it. A new instruction variant is then covered by the visitor's own
			// guard rather than needing a second switch updated in step. definitionWalk.openLValue
			// is the body.
			walk.lvalues = true
			graph.EachInstructionPlace(function, block, index, visit)

			// The third loop, over operands rather than lvalues: React 41994-41998, oxc 918-926.
			// It opens a range for any operand whose end is already past this instruction but whose
			// start was never opened.
			//
			// # This loop was deliberately omitted by Stage 2 and its omission verdict has expired
			//
			// Stage 2 measured zero values in that state over the whole corpus and recorded the
			// verdict with the enumerated writers of `End` that made it true: the lvalue loop above,
			// which always sets Start and End together, and the StoreContext rule below, which only
			// widens a value the lvalue loop already opened. It also wrote down the exact condition
			// under which the verdict would expire, which was a widening pass reaching a value
			// through the alias graph.
			//
			// That pass is `AliasingGraph.Widen` above and the verdict is now void. The widening
			// writes `End` on values this function's instructions never define as an lvalue -- a
			// parameter, a context value, the return value -- so `End > order && Start == 0` is now
			// reachable and this loop is the only thing that gives those values a start. Re-measured
			// rather than inherited: the corpus count is in this stage's report, and
			// `TestRangesThirdLoopOpensAWidenedOperand` pins the state directly.
			walk.lvalues = false
			graph.EachInstructionPlace(function, block, index, visit)

			// StoreContext widens the stored value's end from the instruction shape alone, with no
			// effect consulted: React 42002-42004, oxc 960-967. It is the only widening rule in the
			// whole pass that survives the absence of effects, which is why it is here and the
			// mutation extension is not.
			//
			// The reason it is effect-free upstream is that a store into a captured binding is a
			// mutation the instruction itself proves: the value is written into a cell an enclosing
			// scope can observe, so it stays live past this point regardless of what any effect
			// says about aliasing.
			//
			// An IR with no captured variables in its graph has no such store (Adamic's).
			if stored, ok := graph.StoredContextValue(function, block, index); ok {
				existing := result.Get(stored)
				if existing.End <= order {
					existing.End = order + 1
					result.Set(stored, existing)
				}
			}
		}
	}

	// Options.ParametersDefinedOnEntry, Adamic's rule, after both halves: a parameter is defined on
	// entry, before the first instruction, not where it's first read, which is where the operand loop
	// above opens a range nothing defines. So its range starts at the function's first instruction,
	// and one nothing widened is that instruction alone.
	if options.ParametersDefinedOnEntry {
		for _, parameter := range graph.Parameters(function) {
			id := graph.IdentifierOf(parameter)
			existing := result.Get(id)
			if existing.End == 0 {
				existing.End = 2
			}
			existing.Start = 1
			result.Set(id, existing)
		}
	}

	return result
}

// RangeOf returns a value's mutable range from a table.
//
// A thin wrapper over `MutableRanges.Get`, kept because it is the name upstream callers use and
// because it is the seam that would absorb a later move of the range onto `Identifier`: callers
// written against this keep compiling if the storage changes.
func RangeOf(ranges *MutableRanges, id static_single_assignment.IdentifierId) MutableRange {
	return ranges.Get(id)
}

// IsMutableAt reports whether a value is still being written at an evaluation position.
//
// This is React's `inRange`. Under-approximates for the reason the package comment gives: false
// here can mean "settled" or can mean "we could not see the mutation", and a consumer that must not
// confuse those asks `RangeGaps` first.
func IsMutableAt(
	ranges *MutableRanges,
	id static_single_assignment.IdentifierId,
	order static_single_assignment.EvaluationOrder,
) bool {
	return ranges.Contains(id, order)
}

// ValidateMutableRanges reports every value whose range breaks upstream's invariant.
//
// React's `assertValidMutableRanges` raises a compiler error; this returns the offenders so a test
// can name them, because a linter must not panic on user code and a silent skip would make the
// invariant untestable. An empty result is the passing answer.
//
// The returned ids are sorted, so a failure message is stable between runs. A map is the storage
// and Go randomises its iteration, a hazard the package has already been bitten by once.
func ValidateMutableRanges(ranges *MutableRanges) []static_single_assignment.IdentifierId {
	if ranges == nil || ranges.ranges == nil {
		return nil
	}
	var invalid []static_single_assignment.IdentifierId
	for id, value := range ranges.ranges {
		if !value.IsValid() {
			invalid = append(invalid, id)
		}
	}
	sort.Slice(invalid, func(left, right int) bool { return invalid[left] < invalid[right] })
	return invalid
}

// phiOpensBefore reports whether a phi's range should open before its block.
//
// Upstream's test is `phi.place.identifier.mutableRange.end > firstInstructionIdOfBlock`, which
// asks whether the phi's value is still being mutated after the block it merges in. That end can
// only have been widened by `AliasingState.mutate`, so with no effect facts the test is false for
// every phi in this tree and the branch never fires today.
//
// # This is a live branch rather than a deleted one, and the reason is a measurement
//
// The standing rule in this tree is that an unreachable branch is deleted with the reason recorded
// at the line. This one is not unreachable in the code: it is unreachable because its input is
// uniformly absent, which is a different verdict with a different expiry, and the distinction is
// exactly the one the brief warns expires when a caller appears.
//
// The callers enumerated when the verdict was taken: `InferMutableRanges` alone. The input
// enumerated: a phi identifier's `End`, which today is written only by the lvalue loop and by
// StoreContext, neither of which can name a phi place.
//
// Measured on the corpus: 0 of the phis over 1,657 functions satisfy it. When Stage 1 lands and
// `mutate` widens a phi's end, this fires with no further edit, and
// `TestRangesPhiOpensBeforeItsBlockWhenWidened` seeds the widened state by hand so the branch is
// proven to work rather than asserted by a comment.
func phiOpensBefore(existing MutableRange, firstOrder static_single_assignment.EvaluationOrder) bool {
	if firstOrder == 0 {
		return false
	}
	return existing.End > firstOrder
}

// phiOpenedRange returns a phi's range opened early, and whether it moved.
//
// Upstream opens it at `firstInstructionIdOfBlock - 1`, one before the block's first instruction,
// rather than at the instruction itself. The offset is load-bearing rather than cosmetic: a phi's
// value exists at the top of the block, before anything in it runs, so a range starting at the
// first instruction would exclude the merge point itself and `Contains` would answer false at the
// one position the phi is definitionally live.
//
// Split out of the loop so the offset is testable. It only runs when `phiOpensBefore` fires, which
// needs an end widened by Stage 1, so no lowered source can reach it today and a mutation moving
// the offset by one survived every fixture over real code.
func phiOpenedRange(existing MutableRange, firstOrder static_single_assignment.EvaluationOrder) (MutableRange, bool) {
	if !phiOpensBefore(existing, firstOrder) || existing.Start != 0 {
		return existing, false
	}
	existing.Start = firstOrder - 1
	return existing, true
}

// BlockFirstOrder returns the evaluation position of a block's first instruction.
//
// Upstream falls back to the terminal's order for an empty block, which is
// `block.instructions.at(0)?.id ?? block.terminal.id`. Reproduced, because an empty block is common
// in this graph - every goto-only join block is one - and using zero there would open a phi's range
// at a position no instruction occupies, which reads back as unset.
func BlockFirstOrder[G Graph[F, B, P], F any, B comparable, P any](
	graph G,
	function F,
	block B,
) static_single_assignment.EvaluationOrder {
	count := graph.InstructionCount(function, block)
	for index := 0; index < count; index++ {
		if order, ok := graph.InstructionOrder(function, block, index); ok {
			return order
		}
	}
	return graph.TerminalOrder(block)
}

// maximumOrder returns the larger of two evaluation positions.
//
// Upstream writes `Math.max(instr.id + 1, existing)` inside a branch that has just tested the
// existing value is zero, so the max is redundant there. It is reproduced rather than simplified
// because the redundancy is upstream's and a later widening rule reaching this line would need it.
func maximumOrder(left, right static_single_assignment.EvaluationOrder) static_single_assignment.EvaluationOrder {
	if left > right {
		return left
	}
	return right
}

// =============================================================================
// AliasingState: the widening that makes a mutable range wider than one
// =============================================================================

// aliasingEdgeKind is the kind of a directed data-flow edge between two values.
//
// React and oxc agree exactly: `Capture`, `Alias`, `MaybeAlias`. A `MaybeAlias` edge downgrades a
// definite mutation to a conditional one when it is traversed, which is the only place the kind is
// read during the walk.
type aliasingEdgeKind uint8

const (
	aliasingEdgeCapture aliasingEdgeKind = iota
	aliasingEdgeAlias
	aliasingEdgeMaybeAlias
)

// mutationKind is how certain a mutation is. Ordered, and the order is load-bearing.
//
// The worklist's revisit test is `previous >= current -> skip`, so `Conditional < Definite` is what
// lets a definite mutation re-enter a node a conditional one already reached. Reversing the two
// constants would make the pass terminate on a strictly smaller fixpoint and still look correct.
type mutationKind uint8

const (
	mutationKindConditional mutationKind = 1
	mutationKindDefinite    mutationKind = 2
)

// aliasingEdge is one directed edge, tagged with the sequence index at which it came into being.
//
// `index` is the whole reason this pass is not a naive graph walk. Every edge and every mutation is
// stamped with a monotonically increasing counter as the graph is built, and the walk refuses to
// traverse an edge whose index is at or after the mutation's own index. That is upstream's model of
// time: a mutation cannot flow backwards through an alias that did not exist yet.
type aliasingEdge struct {
	index int
	node  static_single_assignment.IdentifierId
	kind  aliasingEdgeKind
}

// aliasingNodeValue distinguishes a phi from an ordinary object.
//
// Upstream carries a third case, `Function`, which it uses only to append that function's deferred
// diagnostics. This pass reports nothing, so the function case would be an unreachable branch and is
// not represented. The phi case is load-bearing: a phi does not propagate a mutation backwards
// through its alias edges when the mutation arrived travelling forwards, which is the one place the
// walk consults the node's value at all.
type aliasingNodeValue uint8

const (
	aliasingNodeObject aliasingNodeValue = iota
	aliasingNodePhi
)

// aliasingNode is one value in the data-flow graph.
//
// The four maps are the backward edges, kept separately because the walk traverses them under
// different conditions: `createdFrom` always forces a transitive mutation, `captures` is followed
// only for a transitive mutation, and `aliases`/`maybeAliases` are followed only when the walk is
// travelling backwards or the node is not a phi. `edges` is the single forward list, traversed
// unconditionally. Collapsing them into one adjacency list would lose exactly those distinctions.
//
// Insertion order matters for the maps upstream (`FxIndexMap` in oxc, a JavaScript `Map` in React),
// so this keeps an explicit key slice beside each map rather than ranging a Go map. Ranging a Go
// map for its order has already cost this package one defect, and a nondeterministic traversal
// order here would produce a range table that differs between runs on any function with two edges.
type aliasingNode struct {
	id static_single_assignment.IdentifierId

	createdFrom      map[static_single_assignment.IdentifierId]int
	createdFromOrder []static_single_assignment.IdentifierId
	captures         map[static_single_assignment.IdentifierId]int
	capturesOrder    []static_single_assignment.IdentifierId
	aliases          map[static_single_assignment.IdentifierId]int
	aliasesOrder     []static_single_assignment.IdentifierId
	maybeAliases     map[static_single_assignment.IdentifierId]int
	maybeAliasOrder  []static_single_assignment.IdentifierId

	edges []aliasingEdge

	value aliasingNodeValue
}

func newAliasingNode(id static_single_assignment.IdentifierId, value aliasingNodeValue) *aliasingNode {
	return &aliasingNode{
		id:           id,
		createdFrom:  map[static_single_assignment.IdentifierId]int{},
		captures:     map[static_single_assignment.IdentifierId]int{},
		aliases:      map[static_single_assignment.IdentifierId]int{},
		maybeAliases: map[static_single_assignment.IdentifierId]int{},
		value:        value,
	}
}

// insertBackEdge records a backward edge, keeping first-wins semantics and insertion order.
//
// Upstream writes `entry(from).or_insert(index)` in Rust and `if (!map.has(k)) map.set(k, v)` in
// JavaScript: the earliest index for a repeated edge is the one kept. That is not an arbitrary
// choice. The index is compared against the mutation's index to decide whether the edge already
// existed, so keeping the earliest makes the edge visible to the widest set of mutations. Keeping
// the latest would silently narrow every range that flows through a repeated alias.
//
// Mutates: into the edge is recorded in the node's own map, which every caller passes for exactly that
// Mutates: order the edge is appended to the node's own insertion order, which every caller passes for exactly that
func insertBackEdge(
	into map[static_single_assignment.IdentifierId]int,
	order *[]static_single_assignment.IdentifierId,
	from static_single_assignment.IdentifierId,
	index int,
) {
	if _, exists := into[from]; exists {
		return
	}
	into[from] = index
	*order = append(*order, from)
}

func newAliasingState() *aliasingState {
	return &aliasingState{
		nodes:           map[static_single_assignment.IdentifierId]*aliasingNode{},
		identities:      map[static_single_assignment.IdentifierId]uint64{},
		identityMembers: map[uint64]map[static_single_assignment.IdentifierId]struct{}{},
		freezeSources:   map[static_single_assignment.IdentifierId][]static_single_assignment.IdentifierId{},
		immutable:       map[static_single_assignment.IdentifierId]EffectValueKind{},
	}
}

// derivePhiImmutable joins the known kinds after every predecessor has been evaluated.
//
// Upstream's `InferenceState.inferPhi` does not invent a new abstract value for a phi. It maps the
// phi to the union of the instruction values named by its operands, so a conditional mutation is
// later checked against their joined kind. Frozen mixed with Mutable yields MaybeFrozen, which
// suppresses conditional mutation but retains Assign/CreateFrom identity and alias edges.
//
// A backedge or unknown operand leaves the phi mutable. Upstream resolves those through its dataflow
// fixpoint; this pass is deliberately single-shot, and treating an incomplete union as frozen would
// be the unsound direction.
//
// A function rather than a method, since it reads the phi's places through the IR's Graph. Operands
// are sorted by predecessor with one entry each, so ranging them is PhiOperandsInOrder's order.
func derivePhiImmutable[G Graph[F, B, P], F any, B comparable, P any](
	state *aliasingState,
	graph G,
	phi *static_single_assignment.Phi[P],
	seenBlocks map[static_single_assignment.BlockId]bool,
) {
	if phi == nil || len(phi.Operands) == 0 {
		return
	}
	kind := EffectValuePrimitive
	hasFrozen, hasMutable := false, false
	for _, operand := range phi.Operands {
		if !seenBlocks[operand.Predecessor] {
			return
		}
		operandKind, present := state.immutable[graph.IdentifierOf(operand.Place)]
		if !present {
			hasMutable = true
			continue
		}
		hasFrozen = hasFrozen || operandKind == EffectValueFrozen || operandKind == EffectValueMaybeFrozen
		hasMutable = hasMutable || operandKind == EffectValueMaybeFrozen
		if operandKind == EffectValueFrozen || operandKind == EffectValueMaybeFrozen {
			kind = operandKind
		} else if operandKind == EffectValueGlobal && kind == EffectValuePrimitive {
			kind = EffectValueGlobal
		}
	}
	if hasMutable {
		if !hasFrozen {
			return
		}
		kind = EffectValueMaybeFrozen
	}
	state.markImmutable(graph.IdentifierOf(phi.Place), kind)
}

// mutationDirection is whether the walk arrived at a node travelling forwards or backwards.
//
// Read in exactly one place: a phi does not propagate a mutation back through its alias edges when
// the walk reached it travelling forwards. Upstream's condition is
// `direction === 'backwards' || node.value.kind !== 'Phi'`.
type mutationDirection uint8

const (
	mutationDirectionBackwards mutationDirection = iota
	mutationDirectionForwards
)

// mutationQueueEntry is one pending visit in the worklist.
type mutationQueueEntry struct {
	place      static_single_assignment.IdentifierId
	transitive bool
	direction  mutationDirection
	kind       mutationKind
}

// pendingMutation is one mutation deferred until the whole graph is built.
//
// Upstream collects every mutation during the graph walk and applies them all afterwards, which is
// not an optimization. A mutation applied while the graph is half-built would traverse only the
// edges discovered so far, so a value aliased later in the same function would never be reached. The
// index test already models time correctly; applying early would model it twice and wrongly.
type pendingMutation struct {
	index      int
	end        static_single_assignment.EvaluationOrder
	transitive bool
	kind       mutationKind
	place      static_single_assignment.IdentifierId
}

// BuildAliasingGraph constructs the data-flow graph and returns it with the mutations to apply.
//
// This is upstream's Part 1, at React 41736-41850 and oxc 473-645. The walk is over blocks in the
// order the function's block slice holds them, which is reverse postorder, matching upstream's
// iteration over its block map.
//
// # The sequence index is the pass's model of time and it is incremented exactly where upstream does
//
// Every edge-creating effect and every mutation takes the current index and increments it. Effects
// that create a node without an edge (`Create`, `CreateFunction`) do not increment, and neither do
// the effects this pass treats as no-ops. Getting those increments wrong does not fail loudly: it
// shifts every subsequent index by a constant, which changes only which edges a mutation can see,
// and the result is a range table that is subtly narrow or subtly wide with no invariant violated.
// They are transcribed one for one against both implementations for that reason.
func BuildAliasingGraph[G Graph[F, B, P], F any, B comparable, P any](
	graph G,
	function F,
	options Options,
) AliasingGraph {
	state := newAliasingState()
	var mutations []pendingMutation
	index := 0

	// Entry values first. Upstream creates nodes for params, context and the return value before
	// walking any block, and the `assign`/`capture`/`maybeAlias` builders drop an edge whose
	// endpoints do not both exist, so an edge touching a parameter would be silently discarded
	// without these.
	// Upstream picks the parameter kind once, at `InferMutationAliasingEffects.ts:123`: a component
	// or hook gets `ValueKind.Frozen` with reason `ReactiveFunctionArgument`, and only a nested
	// function expression gets `Mutable`. React owns the arguments it passes a component or hook,
	// so nothing the body does may mutate them; a callback's parameters carry no such promise.
	//
	// This is what keeps a prop read out of the scope its consumer creates. Without it, a
	// `PropertyLoad` off a parameter is mutable, a later call conditionally mutates it, its range
	// widens to reach that call, and the load joins a scope upstream leaves it out of. Measured on
	// `useMemo-inner-decl.ts`, where upstream's three loads are one instruction wide and ours
	// reached the call.
	//
	// Graph.ParametersFrozen is that rule's answer for the IR. Adamic's parameters are owned or
	// borrowed values, mutable unless their type says otherwise (its inference marks an immutable one
	// with a Create of its kind), so it answers false.
	parametersAreFrozen := graph.ParametersFrozen(function)
	for _, parameter := range graph.Parameters(function) {
		id := graph.IdentifierOf(parameter)
		state.create(id, aliasingNodeObject)
		if parametersAreFrozen {
			state.markImmutable(id, EffectValueFrozen)
		}
	}
	for _, contextValue := range graph.Context(function) {
		id := graph.IdentifierOf(contextValue)
		state.create(id, aliasingNodeObject)
		if kind, known := options.ContextKinds[id]; known && kind != EffectValueMutable {
			state.markImmutable(id, kind)
		}
	}
	// The return value is an entry value too. Upstream creates its node beside the params and
	// context, and adds an alias edge into it at every `return` terminal, so a mutation of a
	// returned value reaches whatever it was returned from. An IR with no Returns place (Adamic's,
	// where what a function returns is its caller's to account for) has neither.
	returns := graph.Returns(function)
	if returns != nil {
		state.create(graph.IdentifierOf(*returns), aliasingNodeObject)
	}

	seenBlocks := map[static_single_assignment.BlockId]bool{}
	type pendingPhiOperand struct {
		from  static_single_assignment.IdentifierId
		into  static_single_assignment.IdentifierId
		index int
	}
	pendingPhis := map[static_single_assignment.BlockId][]pendingPhiOperand{}

	for _, block := range graph.Blocks(function) {
		for _, phi := range graph.Phis(block) {
			phiId := graph.IdentifierOf(phi.Place)
			state.create(phiId, aliasingNodePhi)
			operands := make([]static_single_assignment.IdentifierId, 0, len(phi.Operands))
			for _, operand := range phi.Operands {
				operands = append(operands, graph.IdentifierOf(operand.Place))
			}
			state.recordFreezeSources(phiId, operands)
			derivePhiImmutable(state, graph, phi, seenBlocks)
			// Deterministic operand order, ascending predecessor id. Assigning indices in an order
			// that varied would change which edges a mutation can see and therefore produce a
			// different range table between runs of the same input.
			for position, operand := range phi.Operands {
				if !seenBlocks[operand.Predecessor] {
					// A back edge: the predecessor has not been walked yet, so the operand's node
					// may not exist. Upstream defers these to the predecessor block and still
					// consumes an index here, so the deferral does not shift the numbering.
					pendingPhis[operand.Predecessor] = append(pendingPhis[operand.Predecessor], pendingPhiOperand{
						from:  operands[position],
						into:  phiId,
						index: index,
					})
					index++
				} else {
					state.assign(index, operands[position], phiId)
					index++
				}
			}
		}
		blockId := graph.Id(block)
		seenBlocks[blockId] = true

		count := graph.InstructionCount(function, block)
		for instructionIndex := 0; instructionIndex < count; instructionIndex++ {
			order, ok := graph.InstructionOrder(function, block, instructionIndex)
			if !ok {
				continue
			}
			for _, effect := range graph.Effects(function, block, instructionIndex) {
				from, into := graph.IdentifierOf(effect.From), graph.IdentifierOf(effect.Into)
				switch {
				case effect.Kind == AliasingEffectFreeze:
					// `InferenceState.freeze` changes the abstract kind immediately. Calls later in
					// evaluation order consult that kind before retaining a conditional mutation, so
					// this cannot be treated as a graph-only no-op even though Freeze itself does not
					// widen a range.
					state.freeze(into)

				case effect.Kind == AliasingEffectCreate:
					state.create(into, aliasingNodeObject)
					// React's rule for closures, Graph.Closure: a closure whose captures are all
					// immutable, and whose body only reads them, is created Frozen. Its captures are
					// what a later Freeze of it freezes.
					if captures, frozen := graph.Closure(
						function,
						block,
						instructionIndex,
						into,
						state.immutable,
					); captures != nil {
						sources := make([]static_single_assignment.IdentifierId, 0, len(captures))
						for _, capture := range captures {
							sources = append(sources, graph.IdentifierOf(capture))
						}
						state.recordFreezeSources(into, sources)
						if frozen {
							effect.Value = EffectValueFrozen
						}
					}
					if effect.Value != EffectValueMutable {
						state.markImmutable(into, effect.Value)
					} else {
						delete(state.immutable, into)
					}

				case effect.Kind == AliasingEffectCreateFrom:
					if kind, immutable := state.immutable[from]; immutable && kind != EffectValueMaybeFrozen {
						state.create(into, aliasingNodeObject)
					} else {
						state.createFrom(index, from, into)
					}
					state.deriveImmutable(from, into)
					index++

				case effect.Kind == AliasingEffectAssign:
					// Upstream creates the target if it is absent, which `Alias` does not do.
					if _, exists := state.nodes[into]; !exists {
						state.create(into, aliasingNodeObject)
					}
					sourceIsMutable := !state.notMutable(from) || state.immutable[from] == EffectValueMaybeFrozen
					// An assignment carries mutability with it, the same way `CreateFrom` does.
					// Upstream's `Assign` arm reads `state.kind(effect.from)` and switches on the
					// result, with `ValueKind.Frozen` its first case
					// (`InferMutationAliasingEffects.ts:947`), so a frozen value assigned into a
					// temporary stays frozen.
					//
					// This also carries kinds through ordinary LoadLocal and StoreLocal values. A
					// LoadContext uses CreateFrom instead: it reads from a mutable box without making
					// the loaded temporary another identity for the box.
					state.deriveImmutable(from, into)
					if sourceIsMutable {
						state.shareIdentity(from, into)
						state.assign(index, from, into)
					}
					index++

				case effect.Kind == AliasingEffectAlias:
					// `Alias` and `Capture` share upstream's refinement. A frozen/primitive source
					// becomes an ImmutableCapture (a range no-op), and a mutable source cannot flow
					// into a frozen/primitive destination. Keeping either edge would let a later
					// mutation of the destination widen a value upstream proved non-mutable.
					if kind, present := state.immutable[from]; present &&
						(kind == EffectValueFrozen || kind == EffectValuePrimitive || kind == EffectValueGlobal) {
						index++
						break
					}
					if kind, present := state.immutable[into]; present &&
						(kind == EffectValueFrozen || kind == EffectValuePrimitive || kind == EffectValueGlobal) {
						index++
						break
					}
					state.assign(index, from, into)
					index++

				case effect.Kind == AliasingEffectMaybeAlias:
					// MaybeAlias has one intentional asymmetry in upstream's shared refinement:
					// it survives primitive/global sources and immutable destinations, but a Frozen
					// source is rewritten to ImmutableCapture. MaybeFrozen retains the edge.
					if kind, present := state.immutable[from]; present &&
						kind == EffectValueFrozen {
						index++
						break
					}
					state.maybeAlias(index, from, into)
					index++

				case effect.Kind == AliasingEffectCapture:
					// Upstream switches on the source's kind here and only two of its four arms
					// reach `state.capture` (`InferMutationAliasingEffects.ts:894-942`):
					//
					//	Frozen               re-applied as `ImmutableCapture`
					//	Global, Primitive     pruned, the arm breaks with no effect pushed
					//	Context               re-applied as `MaybeAlias`
					//	Mutable, MaybeFrozen pushed for a mutable destination
					//
					// `ImmutableCapture` is already a no-op for range widening in this pass, and a
					// pruned effect never reaches it either, so both of the arms this tree can
					// express as immutable sources come out the same way: do not widen. Context
					// remains represented by the conservative Mutable seed.
					//
					// The kind is what this needs and one bit is not enough for it. `notMutable`
					// answers "not Mutable or Context", which is exactly right for the two
					// conditional-mutation readers below and wrong here, because it cannot tell a
					// `Context` source -- which upstream downgrades rather than prunes -- from a
					// frozen one.
					//
					// The destination half is the second condition, and upstream states it in the
					// same comment: "If the destination is not mutable, or the source value has
					// copy-on-write semantics, then we can prune the effect". `destinationType` is
					// null for `Frozen`, `Global` and `Primitive`, and a mutable source into a null
					// destination matches none of the three branches, so it is pruned.
					if kind, present := state.immutable[from]; present &&
						(kind == EffectValueFrozen || kind == EffectValuePrimitive || kind == EffectValueGlobal) {
						index++
						break
					}
					if kind, present := state.immutable[into]; present &&
						(kind == EffectValueFrozen || kind == EffectValuePrimitive || kind == EffectValueGlobal) {
						index++
						break
					}
					state.capture(index, from, into)
					index++

				case effect.Kind == AliasingEffectMutateTransitive,
					effect.Kind == AliasingEffectMutateTransitiveConditionally:
					kind := mutationKindDefinite
					if effect.Kind == AliasingEffectMutateTransitiveConditionally {
						kind = mutationKindConditional
					}
					if kind == mutationKindConditional && state.notMutable(into) {
						// Upstream's `state.mutate` returns `none` for a conditional mutation of
						// anything that is not Mutable or Context, and `applyEffect` then does not
						// push the effect at all. The index still advances: a dropped effect must
						// not shift the numbering the walk assigns to the ones that remain.
						index++
						break
					}
					mutations = append(mutations, pendingMutation{
						index:      index,
						end:        order + 1,
						transitive: true,
						kind:       kind,
						place:      into,
					})
					index++

				case effect.Kind == AliasingEffectMutate:
					mutations = append(mutations, pendingMutation{
						index:      index,
						end:        order + 1,
						transitive: false,
						kind:       mutationKindDefinite,
						place:      into,
					})
					index++

				case effect.Kind == AliasingEffectMutateConditionally:
					if state.notMutable(into) {
						index++
						break
					}
					mutations = append(mutations, pendingMutation{
						index:      index,
						end:        order + 1,
						transitive: false,
						kind:       mutationKindConditional,
						place:      into,
					})
					index++

					// ImmutableCapture and Apply are no-ops for range widening, and consume
					// no index. Upstream's `_ => {}` arm, reproduced deliberately rather than by
					// omission: an effect reaching here must not shift the numbering.
				}
			}
		}

		// Pending phi operands whose predecessor is this block, now that its values exist.
		// Upstream applies them with the index recorded at deferral rather than a fresh one.
		for _, pending := range pendingPhis[blockId] {
			state.assign(pending.index, pending.from, pending.into)
		}
		delete(pendingPhis, blockId)

		// A `return` aliases its operand into the function's return value. Upstream does this from
		// the terminal rather than from an effect, because a return carries no instruction effects.
		if returns != nil {
			if value, ok := graph.ReturnValue(block); ok {
				state.assign(index, graph.IdentifierOf(value), graph.IdentifierOf(*returns))
				index++
			}
		}
	}

	return AliasingGraph{state: state, mutations: mutations}
}
