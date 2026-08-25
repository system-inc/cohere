// Mutable ranges: over which span of a function's evaluation a value is still being written.
//
// This is the half of React's `InferMutationAliasingRanges.ts` that does not read effects, run over
// this graph to produce a `MutableRanges` table, which nothing in this tree computes today. oxc
// transcribes the whole pass at
// `oxc_react_compiler/src/react_compiler_inference/infer_mutation_aliasing_ranges.rs`, 1,107 lines;
// where the two disagree React wins, and each disagreement is recorded at the line that resolves it.
//
// A value's mutable range is the span of program points over which it is still being written. Once
// the range is closed, the value is settled: nothing after `end` changes it. That is the fact
// `find_disjoint_mutable_values` reads to decide which values must be memoized as one unit, and it
// is the same fact a dead-store analysis needs to say "this write is the last one and nothing read
// it". This package computes it as general infrastructure; the React consumer is one caller.
//
// # A range is an INTERVAL, not a set, and that is upstream's decision rather than a summary of it
//
// Measured rather than inferred. React represents it as `{start, end}` with membership
// `inRange({id}, range) { return id >= range.start && id < range.end; }` (development bundle line
// 32315), a HALF-OPEN interval `[start, end)`. oxc's `MutableRange` carries the same two fields.
// There is no set anywhere in either implementation.
//
// The consequence is load-bearing and it is why this is stated rather than assumed. A value mutated
// at instruction 3 and again at instruction 20, untouched between, has the interval [3, 21). Every
// instruction from 4 to 19 reports as "inside the mutable range" even though nothing there writes
// it. The interval SPANS THE GAP, deliberately. Upstream relies on that: a reactive scope covering
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
// # What is computed WITHOUT effect facts, and what is not, measured on the real tree
//
// `Place.Effect` is `EffectUnknown` on every place lowering produces, and Stage 1 effect inference
// is what fills it. This pass was built to find out how much of a range survives that absence
// rather than to wait for it, and the answer is specific.
//
// Upstream writes `mutableRange` at exactly two kinds of site.
//
// The DEFINITION sites need no effects at all. Every instruction lvalue opens its identifier's
// range at that instruction's evaluation order and closes it at order+1, unconditionally, in a loop
// over `eachInstructionLValue` that reads no effect (React lines 41916-41927, oxc 805-834). A phi
// whose value is mutated after creation opens at the block's first instruction minus one. This is
// the whole `start` half of the algorithm plus the initial `end`.
//
// The EXTENSION sites are where every effect fact enters, and there is only one function that
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
// One number here corrects an earlier reading of this same corpus. `StoreContext` measures ZERO on
// it, which reads as a rule that never fires, and it is instead an artifact of the harness: the
// corpus is lowered with a NIL checker, so `captureOf` cannot resolve a captured binding and
// lowering emits `StoreGlobal` instead. Seeded with a real checker the shape appears and the
// widening fires, which `TestRangesWidenAStoreIntoACapturedBinding` pins. A zero measured through a
// checker-less lowering is not a fact about the code.
//
// # Stage 1 landed during this work, and it closes the gap rather than shrinking it
//
// The measurements above were taken against a tree where `Place.Effect` was `EffectUnknown`
// everywhere. Effect inference has since landed in this package as `InferAliasingEffects`, and it
// supplies exactly the input the extension half needs: an `AliasingEffect` per instruction carrying
// `Kind`, `From` and `Into`, with `Kind.IsMutation()` naming the four mutation kinds React's
// `mutations` list is built from.
//
// Measured over the same corpus with that table present: all 12,127 sites `MutationSites` returns
// carry an effect list, 125,110 effects in total, of which 22,879 are mutations. So the extension
// half is no longer blocked, and this pass is deliberately NOT extended here - that is Stage 3's
// work and it is a port of `AliasingState`, a worklist over the alias graph with its own
// termination argument, rather than a few more lines in this file.
//
// What this file guarantees for that work: the definition half is exact and mutation-tested, the
// interval representation is upstream's and pinned, and `MutationSites` is the list to walk. The
// two `RangeGap` constants are what a Stage 3 pass removes when it lands.
//
// # The direction of the error, which is the property a consumer must know
//
// Every gap here makes a range SHORTER than upstream's, never longer. A missing `mutate` call is a
// missing `max`, and `max` only grows `end`. So `RangeOf` returns an interval contained in
// upstream's, and `IsMutableAt` answers false where upstream answers true, never the reverse.
//
// That is the safe direction for a dead-store consumer and the unsafe direction for a memoization
// consumer, so it is stated in the API rather than only here: `RangeGaps` names it, and
// `TestRangesAreAnUnderApproximation` pins the direction with a case whose answer must change when
// Stage 1 lands.
//
// # Why this does not wait for Stage 1, and what the seam is
//
// `MutationSites` returns the instructions whose ranges Stage 1 would widen, computed from the
// instruction shape alone. When effects land, the extension half is a loop over that list calling
// `widen`, and nothing in the definition half changes. The interface is therefore already the shape
// the finished pass needs, and the measurement above says exactly how many cases are waiting.
//
// # Convergence, and why there is no fixpoint here at all
//
// This is worth stating because the neighbouring pass has one and the symmetry is misleading.
// `InferReactive` iterates to a fixpoint because reactivity propagates transitively through phis
// and control, so marking one value can mark another. Ranges do not propagate that way in the
// definition half: an lvalue's range depends on its own instruction's order and nothing else, so
// one ordered walk is exact and a second walk would change nothing. Upstream's own structure agrees
// - `inferMutationAliasingRanges` is a single pass over blocks, and the only iteration in it is
// `AliasingState.mutate`'s worklist, which walks the ALIAS graph rather than the control-flow graph
// and is part of the extension half this declines.
//
// So there is no round count to bound and no convergence equality to hand-write. If the extension
// half is added, its worklist is over the alias graph and its termination comes from the
// `seen`/`kind` monotone check upstream already carries, which is a different mechanism from
// `InferReactive`'s growing set.
//
// # Reverse postorder is the right walk here, and the reason is not the usual one
//
// `Function.Blocks` is in reverse postorder and this walks it in that order. For a convergence
// analysis that choice is about speed; here it is about CORRECTNESS of the numbering rather than
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
package hir

import "sort"

// MutableRange is the half-open span of evaluation over which a value is still being written.
//
// Membership is `Start <= order < End`, which is React's `inRange` exactly. A range with both
// fields zero is UNSET: the value has no recorded definition point. That is a real state rather
// than an error - a function's parameters and context values arrive already defined, so nothing in
// this pass opens a range for them.
//
// The zero value is the unset range, which is what `Identifier.MutableRange` holds before this pass
// runs and what a caller must handle.
type MutableRange struct {
	Start EvaluationOrder
	End   EvaluationOrder
}

// IsSet reports whether a range was ever opened.
func (r MutableRange) IsSet() bool { return r.Start != 0 || r.End != 0 }

// Contains reports whether an evaluation position lies inside the range.
//
// Half-open, matching React's `inRange`: the start is inside and the end is not. An unset range
// contains nothing, which is the answer that keeps a caller from having to check IsSet first.
func (r MutableRange) Contains(order EvaluationOrder) bool {
	if !r.IsSet() {
		return false
	}
	return order >= r.Start && order < r.End
}

// IsValid reports whether a range satisfies upstream's invariant.
//
// React's `validateMutableRange` asserts `(start === 0 && end === 0) || end > start`. A range that
// is set but empty or inverted is a bug in whatever produced it, and upstream raises rather than
// tolerating it.
func (r MutableRange) IsValid() bool {
	if !r.IsSet() {
		return true
	}
	return r.End > r.Start
}

// RangeGap names a range-widening rule this pass cannot apply, for a caller that needs to know.
//
// Modelled on `ReactiveGap` deliberately: a gap answerable through the API is one a consumer can
// decline on, while a gap living only in a comment is one the next reader inherits by accident.
type RangeGap uint8

const (
	// RangeGapMutationExtension is a range widened by a mutation recorded as an effect.
	//
	// Upstream's `AliasingState.mutate` extends `end` to one past the mutating instruction, for the
	// mutated value and everything transitively aliased to it. Its input is `instr.effects`, which
	// is `EffectUnknown` everywhere in this tree; see `MutationSites` for the count.
	RangeGapMutationExtension RangeGap = iota
	// RangeGapAliasPropagation is a range widened because a mutation reached the value through an
	// alias, a capture, or a `createFrom` edge.
	//
	// Same missing input. Listed separately because closing the first without the second would give
	// a value its own mutations and not those of the objects it was aliased into, which is a
	// different and still-wrong answer rather than a smaller one.
	RangeGapAliasPropagation
)

// RangeGaps are the widening rules InferMutableRanges does not apply. See RangeGap.
//
// Returned as a value rather than documented alone so a test can assert on it, which makes closing
// a gap a visible event rather than a silent improvement.
func RangeGaps() []RangeGap {
	return []RangeGap{RangeGapMutationExtension, RangeGapAliasPropagation}
}

// MutableRanges is the table this pass produces: one range per value.
//
// # Why a side table rather than a field on Identifier
//
// Upstream stores the range ON the identifier, `identifier.mutableRange`, and oxc does the same in
// its `Environment`. This does not, and the reason is ownership rather than design preference:
// `Identifier` lives in `hir.go`, which is a shared file, and Stage 1 effect inference is being
// built in this package at the same time as this. A pass that adds a field to a shared struct
// mid-flight is a pass that conflicts with whatever else is editing it, and the package comment on
// `Identifier.Type` already records that fields are declared up front precisely to avoid that.
//
// The cost is one indirection and one thing a caller must hold. The benefit is that this pass is
// self-contained in one file, which is what makes it reviewable against upstream without reading
// the rest of the package. If ranges become permanent infrastructure, moving them onto `Identifier`
// is a mechanical change and `RangeOf` is the seam that makes it invisible to callers.
//
// A value absent from the table has the unset range, which `Get` returns, so a caller never has to
// distinguish "not computed" from "no range" - upstream does not either, because zero-zero IS the
// unset range there.
type MutableRanges struct {
	ranges map[IdentifierId]MutableRange
}

// Get returns a value's range, or the unset range when this pass never opened one.
func (m *MutableRanges) Get(id IdentifierId) MutableRange {
	if m == nil || m.ranges == nil {
		return MutableRange{}
	}
	return m.ranges[id]
}

// Len reports how many values have a range, for measurement.
func (m *MutableRanges) Len() int {
	if m == nil {
		return 0
	}
	return len(m.ranges)
}

// Contains reports whether a value is still being written at an evaluation position.
//
// This is React's `inRange` reached through the table. Half-open, and false for an unset range.
func (m *MutableRanges) Contains(id IdentifierId, order EvaluationOrder) bool {
	return m.Get(id).Contains(order)
}

// set writes a range, creating the table lazily.
func (m *MutableRanges) set(id IdentifierId, r MutableRange) {
	if m.ranges == nil {
		m.ranges = map[IdentifierId]MutableRange{}
	}
	m.ranges[id] = r
}

// InferMutableRanges computes the mutable range of every value in function, returning them as a
// table keyed by value.
//
// Requires evaluation order, which `Finalize` assigns and which `Construct` re-establishes. Call
// `Construct` first, or take a function from `ForFunction`, which does. Over a graph whose
// `Instruction.Order` is zero every range would open at zero, which is the unset value and would
// silently produce an empty table rather than a wrong one; `TestRangesRequireEvaluationOrder` pins
// that a caller who skips `Finalize` gets nothing rather than nonsense.
//
// Nested functions are NOT included. Upstream analyses a nested function separately, through
// `lowerWithMutationAliasing`, and then RESETS every context operand's range to unset
// (`analyseFunctions`, React 42290-42297). Ranges are therefore per-function by upstream's own
// construction, and a table spanning two functions would key two different values under one id -
// the identifier tables are per-function, which the brief names as a trap that has already cost an
// agent a silent drop. `RangesForNested` walks them explicitly and returns one table each.
//
// Reads no effects. The ranges are exact for every value never mutated after its definition and an
// under-approximation for the rest; see the package comment and `MutationSites`.
//
// Idempotent: every guard tests the range this pass itself wrote, so a second run over the same
// function produces an equal table. `TestRangesAreIdempotent` pins it.
func InferMutableRanges(function *Function) *MutableRanges {
	result := &MutableRanges{}
	if function == nil {
		return result
	}

	for _, block := range function.Blocks {
		// A phi's range opens at the block's first instruction minus one, but ONLY when the phi is
		// mutated after creation, which is a fact this pass cannot establish. See the note in
		// `phiOpensBefore` for why the branch is present and inert rather than deleted.
		firstOrder := blockFirstOrder(function, block)
		for _, phi := range block.Phis {
			id := phi.Place.Identifier
			existing := result.Get(id)
			if opened, ok := phiOpenedRange(existing, firstOrder); ok {
				result.set(id, opened)
			}
		}

		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			order := instruction.Order
			if order == 0 {
				// Unnumbered, so `Finalize` never reached this block. Opening a range at zero
				// would write the unset value and read back as "no range", which is a wrong answer
				// wearing the shape of a right one. Skipping is the honest response.
				continue
			}

			// Every lvalue opens its own range here. This is upstream's unconditional loop over
			// `eachInstructionLValue` and it reads no effect: React lines 41916-41927, oxc 805-834.
			//
			// Reached through `EachInstructionPlace` and the Define role rather than by matching
			// instruction variants, which is how `eachInstructionLValue` in reactive.go already
			// spells it. A new instruction variant is then covered by the visitor's own guard
			// rather than needing a second switch updated in step.
			EachInstructionPlace(instruction, func(place Place, role PlaceRole) {
				if role != PlaceRoleDefine {
					return
				}
				existing := result.Get(place.Identifier)
				changed := false
				if existing.Start == 0 {
					existing.Start = order
					changed = true
				}
				if existing.End == 0 {
					existing.End = maxOrder(order+1, existing.End)
					changed = true
				}
				if changed {
					result.set(place.Identifier, existing)
				}
			})

			// Upstream has a THIRD loop here, over operands rather than lvalues, opening a range
			// for any operand whose end is already past this instruction but whose start is unset:
			// React 41994-41998, oxc 918-926. It is NOT reproduced, and the reason is a measurement
			// rather than a judgment that it does not matter.
			//
			// Its precondition is `End > order && Start == 0`, which requires a value whose end was
			// widened while its start was never opened. Nothing in this tree can produce that state.
			// The two writers of `End` are the lvalue loop above, which always sets `Start` and `End`
			// together, and the `StoreContext` rule below, which only ever widens a value whose start
			// the lvalue loop already opened. So the two fields are never independently set.
			//
			// Measured two-sided over the corpus, 313,292 values in 4,333 functions: 145,113 carry a
			// set range and 0 carry an end without a start. The non-zero control is what makes the
			// zero a measurement rather than a walk that could not see.
			//
			// The writers enumerated when this verdict was taken, because the verdict EXPIRES when one
			// is added: the lvalue loop above, and the StoreContext rule below. Stage 1's mutation
			// extension is exactly such a writer - `AliasingState.mutate` widens `End` on a value it
			// reaches through the alias graph, which can be a value this function never defined, and
			// that is precisely the state this loop exists for. When Stage 1 lands, this loop has to
			// come back and the measurement above has to be re-run rather than inherited.
			//
			// It is omitted rather than kept-and-inert because a loop that cannot fire reads as
			// coverage: a mutation neutralising its guard SURVIVED the whole suite, which is what sent
			// this measurement looking, and a reader would have taken the surviving branch for a
			// tested one.

			// StoreContext widens the STORED VALUE's end from the instruction shape alone, with no
			// effect consulted: React 42002-42004, oxc 960-967. It is the only widening rule in the
			// whole pass that survives the absence of effects, which is why it is here and the
			// mutation extension is not.
			//
			// The reason it is effect-free upstream is that a store into a captured binding is a
			// mutation the instruction itself proves: the value is written into a cell an enclosing
			// scope can observe, so it stays live past this point regardless of what any effect
			// says about aliasing.
			if store, ok := instruction.Value.(*StoreContext); ok {
				existing := result.Get(store.Value.Identifier)
				if existing.End <= order {
					existing.End = order + 1
					result.set(store.Value.Identifier, existing)
				}
			}
		}
	}

	return result
}

// RangesForNested returns one range table per nested function, keyed by FunctionId.
//
// Separate from `InferMutableRanges` because identifier tables are per-function: an id means one
// value in this function and a different value in a nested one, so merging the tables would answer
// confidently and wrongly. Upstream keeps them separate for the same reason and additionally resets
// a nested function's context ranges after analysing it.
func RangesForNested(function *Function) map[FunctionId]*MutableRanges {
	if function == nil {
		return nil
	}
	out := map[FunctionId]*MutableRanges{}
	for index, nested := range function.Functions {
		out[FunctionId(index)] = InferMutableRanges(nested)
	}
	return out
}

// RangeOf returns a value's mutable range from a table.
//
// A thin wrapper over `MutableRanges.Get`, kept because it is the name upstream callers use and
// because it is the seam that would absorb a later move of the range onto `Identifier`: callers
// written against this keep compiling if the storage changes.
func RangeOf(ranges *MutableRanges, id IdentifierId) MutableRange {
	return ranges.Get(id)
}

// IsMutableAt reports whether a value is still being written at an evaluation position.
//
// This is React's `inRange`. Under-approximates for the reason the package comment gives: false
// here can mean "settled" or can mean "we could not see the mutation", and a consumer that must not
// confuse those asks `RangeGaps` first.
func IsMutableAt(ranges *MutableRanges, id IdentifierId, order EvaluationOrder) bool {
	return ranges.Contains(id, order)
}

// MutationSite is one instruction whose range widening needs effect facts.
//
// It is the seam to Stage 1: when `Place.Effect` is filled, the extension half of this pass is a
// walk over these calling `AliasingState.mutate`, and nothing in the definition half changes.
type MutationSite struct {
	// Instruction is the instruction that may mutate a value.
	Instruction InstructionId
	// Order is where it sits in evaluation.
	Order EvaluationOrder
	// Target is the value the instruction shape says is written, where the shape names one.
	Target IdentifierId
	// Kind names the shape, for reporting a count by category rather than a bare total.
	Kind MutationSiteKind
}

// MutationSiteKind is the instruction shape that makes a site a mutation candidate.
type MutationSiteKind uint8

const (
	// MutationSitePropertyStore is `a.b = c`, which mutates the receiver.
	MutationSitePropertyStore MutationSiteKind = iota
	// MutationSiteComputedStore is `a[b] = c`, which mutates the receiver.
	MutationSiteComputedStore
	// MutationSiteStoreContext is a write into a captured binding.
	MutationSiteStoreContext
	// MutationSiteCall is a call, whose arguments may be mutated by the callee.
	//
	// This is the shape effects exist to resolve and the reason the count below is a CEILING rather
	// than a measurement of real mutations: most calls mutate nothing, and only an effect table can
	// say which.
	MutationSiteCall
)

func (k MutationSiteKind) String() string {
	switch k {
	case MutationSitePropertyStore:
		return "property-store"
	case MutationSiteComputedStore:
		return "computed-store"
	case MutationSiteStoreContext:
		return "store-context"
	case MutationSiteCall:
		return "call"
	default:
		return "<unknown>"
	}
}

// MutationSites returns the instructions whose range widening this pass cannot perform.
//
// Computed from the instruction SHAPE alone, which is exactly what is available without effects. It
// is the count the package comment reports and the list a Stage 1 consumer walks.
//
// # Calls are included and they are the reason this is a ceiling
//
// A `PropertyStore` definitely mutates its receiver, so its presence here is a real missing
// widening. A `CallExpression` only MIGHT mutate an argument, and upstream decides which by reading
// the callee's signature into an effect. Counting calls here would overstate the gap by an order of
// magnitude, so they are returned as their own kind and the package comment reports the two totals
// separately rather than summing them into one misleading number.
func MutationSites(function *Function) []MutationSite {
	if function == nil {
		return nil
	}
	var sites []MutationSite
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			switch value := instruction.Value.(type) {
			case *PropertyStore:
				sites = append(sites, MutationSite{
					Instruction: instructionId,
					Order:       instruction.Order,
					Target:      value.Object.Identifier,
					Kind:        MutationSitePropertyStore,
				})
			case *ComputedStore:
				sites = append(sites, MutationSite{
					Instruction: instructionId,
					Order:       instruction.Order,
					Target:      value.Object.Identifier,
					Kind:        MutationSiteComputedStore,
				})
			case *StoreContext:
				sites = append(sites, MutationSite{
					Instruction: instructionId,
					Order:       instruction.Order,
					Target:      value.Value.Identifier,
					Kind:        MutationSiteStoreContext,
				})
			case *CallExpression:
				sites = append(sites, MutationSite{
					Instruction: instructionId,
					Order:       instruction.Order,
					Target:      value.Callee.Identifier,
					Kind:        MutationSiteCall,
				})
			case *MethodCall:
				sites = append(sites, MutationSite{
					Instruction: instructionId,
					Order:       instruction.Order,
					Target:      value.Receiver.Identifier,
					Kind:        MutationSiteCall,
				})
			}
		}
	}
	return sites
}

// ValidateMutableRanges reports every value whose range breaks upstream's invariant.
//
// React's `assertValidMutableRanges` raises a compiler error; this returns the offenders so a test
// can name them, because a linter must not panic on user code and a silent skip would make the
// invariant untestable. An empty result is the passing answer.
//
// The returned ids are sorted, so a failure message is stable between runs. A map is the storage
// and Go randomises its iteration, which is the same hazard `Phi.Operands` carries and which the
// package has already been bitten by once.
func ValidateMutableRanges(ranges *MutableRanges) []IdentifierId {
	if ranges == nil || ranges.ranges == nil {
		return nil
	}
	var invalid []IdentifierId
	for id, r := range ranges.ranges {
		if !r.IsValid() {
			invalid = append(invalid, id)
		}
	}
	sort.Slice(invalid, func(i, j int) bool { return invalid[i] < invalid[j] })
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
// at the line. This one is NOT unreachable in the code: it is unreachable because its INPUT is
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
func phiOpensBefore(existing MutableRange, firstOrder EvaluationOrder) bool {
	if firstOrder == 0 {
		return false
	}
	return existing.End > firstOrder
}

// phiOpenedRange returns a phi's range opened early, and whether it moved.
//
// Upstream opens it at `firstInstructionIdOfBlock - 1`, one BEFORE the block's first instruction,
// rather than at the instruction itself. The offset is load-bearing rather than cosmetic: a phi's
// value exists at the top of the block, before anything in it runs, so a range starting at the
// first instruction would exclude the merge point itself and `Contains` would answer false at the
// one position the phi is definitionally live.
//
// Split out of the loop so the offset is testable. It only runs when `phiOpensBefore` fires, which
// needs an end widened by Stage 1, so no lowered source can reach it today and a mutation moving
// the offset by one survived every fixture over real code.
func phiOpenedRange(existing MutableRange, firstOrder EvaluationOrder) (MutableRange, bool) {
	if !phiOpensBefore(existing, firstOrder) || existing.Start != 0 {
		return existing, false
	}
	existing.Start = firstOrder - 1
	return existing, true
}

// blockFirstOrder returns the evaluation position of a block's first instruction.
//
// Upstream falls back to the TERMINAL's order for an empty block, which is
// `block.instructions.at(0)?.id ?? block.terminal.id`. Reproduced, because an empty block is common
// in this graph - every goto-only join block is one - and using zero there would open a phi's range
// at a position no instruction occupies, which reads back as unset.
func blockFirstOrder(function *Function, block *BasicBlock) EvaluationOrder {
	for _, instructionId := range block.Instructions {
		if instruction := function.Instructions[instructionId]; instruction != nil {
			return instruction.Order
		}
	}
	return TerminalOrder(block.Terminal)
}

// maxOrder returns the larger of two evaluation positions.
//
// Upstream writes `Math.max(instr.id + 1, existing)` inside a branch that has just tested the
// existing value is zero, so the max is redundant there. It is reproduced rather than simplified
// because the redundancy is upstream's and a later widening rule reaching this line would need it.
func maxOrder(a, b EvaluationOrder) EvaluationOrder {
	if a > b {
		return a
	}
	return b
}
