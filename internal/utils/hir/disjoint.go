// Disjoint mutable values: which values are so entangled by mutation that they must be treated as
// one unit.
//
// This is React's `findDisjointMutableValues` (development bundle line 32377) together with the
// `DisjointSet` it builds (line 32144), run over this graph. oxc transcribes both at
// `oxc_react_compiler/src/react_compiler_inference/infer_reactive_scope_variables.rs`; where the two
// disagree React wins, and each disagreement is recorded at the line that resolves it.
//
// # What the question actually is
//
// Two values that alias the same underlying object cannot be memoized independently. If `a` is
// pushed into `b` and then `b` is mutated, caching `a` across a render where `b` changed hands back
// a stale object. Upstream's answer is not "track the alias graph at every consumer" -- it is to
// collapse the entangled values into ONE equivalence class up front, and let every consumer ask a
// single question: what is this value's representative? That is what makes this a union-find rather
// than a reachability walk. The structure is chosen because the consumers only ever ask for a
// canonical name, never for a path.
//
// Two consumers exist upstream, and the second is the one that matters here.
// `inferReactiveScopeVariables` (line 32233) turns each class into a reactive scope.
// `inferReactivePlaces` (line 42887) wraps the result in a `ReactivityMap` whose whole use of it is
// `aliasedIdentifiers.find(place.identifier) ?? place.identifier` (line 43011) -- a representative
// lookup with a fallback to the value itself. `reactive.go` in this package already declares
// `ReactiveGapAliasing` for exactly that missing input, quoting `findDisjointMutableValues` by name.
// `Find` below is that lookup and `RepresentativeOf` is the fallback form, so the gap closes on the
// API rather than on a comment.
//
// # The input, measured rather than assumed, because an earlier attempt at this stage DECLINED
//
// Stage 3 was dispatched once before and correctly refused to build. At that point the widening half
// of `InferMutableRanges` did not exist, so every mutable range had width exactly one, every
// `end > start + 1` was false for a structural reason, and the pass would have produced 23,256
// members in 23,256 singleton classes: an identity function that compiles, passes fixtures, and
// tells every consumer that nothing is entangled with anything.
//
// The gates this file reads are NOT the width gate Stage 2.5 measured, so the input was re-measured
// against these predicates specifically, with a checker present -- a lowering without one never
// emits `StoreContext` and reports zeroes that are facts about the harness rather than the code.
// Over 400 corpus files, 685 outermost functions:
//
//	set ranges                                            47,672
//	max width                                                811   (was 2 before Stage 2.5)
//	distinct widths                                          676   (was 2)
//	invalid intervals                                          0   upstream's own invariant
//
//	lvalue gate  `end > start + 1`             21,253 of 47,672    44.6% fire
//	operand gate `isMutable(instr, operand)`   23,697 of 45,712    51.8% fire
//	companion    `range.start > 0`                        43,885
//	phi gate     React's two-term test            841 of  1,881    44.7% fire
//
// Both gates are live on roughly half their reads, so the classes below are computed rather than
// structural. The resulting distribution is in `TestDisjointClassSizesAreNotAllSingletons`, which
// pins the non-degeneracy as a test rather than leaving it in this comment: 5,368 members in 1,842
// classes, largest 47, mean 2.91. That test is the guard against this pass silently reverting to the
// identity function if a future change to ranges narrows them back.
//
// # DIVERGENCE FROM oxc: the loop-carried phi disjunct is NOT applied
//
// oxc's phi predicate carries a third term React has no counterpart for
// (`infer_reactive_scope_variables.rs:264-269`): a phi whose operand is defined at or after the
// block's first instruction is treated as a loop back-edge reassignment and unioned anyway. It
// carries a long comment about scope dependency stability.
//
// React 7.1.1's bundle has no such branch. Its test is the bare `start + 1 !== end && end > first`.
// React is the authority for this rule, so React's two-term test is what this reproduces, and oxc's
// third term is recorded here without being applied. Measured on the corpus so the size of the
// divergence is known rather than guessed: 13 phis of 1,881 satisfy oxc's extra disjunct while
// failing React's test, so applying it would union 13 additional phi groups across 400 files. Should
// this ever need revisiting, the term is one `||` and the measurement is here.
//
// # DIVERGENCE FROM BOTH: mayAllocate's primitive gate is forced FALSE
//
// Upstream's `mayAllocate` returns `lvalue.identifier.type.kind !== 'Primitive'` for
// `CallExpression`, `MethodCall` and `TaggedTemplateExpression` -- a call allocates unless its result
// is known to be a primitive. `Identifier.Type` is nil on every identifier this lowering produces,
// and there is no primitive predicate anywhere in this package (control: a grep for `Primitive` in
// `internal/utils/hir` returns 55 hits, so the zero is a real absence rather than a bad pattern).
//
// With no type information the two available answers are opposite errors. Answering TRUE treats
// every call as allocating, which unions values upstream leaves separate: a false MERGE, and a
// merge cannot be undone by a consumer. Answering FALSE leaves a call's lvalue out of the operand
// list unless its range is genuinely wide, which is a missing merge a consumer can still detect
// through the gap list. The second is the conservative direction and it is what this does, recorded
// as `DisjointGapPrimitiveCallResult` so a consumer can decline on it rather than inherit it
// silently. When a type lattice lands, this becomes `!isPrimitive(lvalue)` and the gap closes.
//
// Measured: 1,974 method calls and their call siblings across the corpus reach this arm.
//
// # Two shortcuts to that lattice were measured and neither works
//
// The type upstream reads here is not TypeScript's. A call's result type comes from unifying the
// callee's own function type (`InferTypes.ts:276`), so it is `Primitive` only when the callee is a
// known global whose signature says so. That made two cheaper routes look plausible, in the way
// `isUseRefType` turned out to be a call-site fact rather than a type fact. Both were tried:
//
//	answer TRUE for every call            false positives 8 to 10, scope exact 16 to 11
//	ask `effectSignature.Result` first    identical, 10 and 11
//
// The second is identical to the first because the table cannot discriminate. Instrumented over the
// clean corpus, `lookupSignature` answers `known=false` for almost every callee that reaches this
// arm -- 39 `useMemo`, 6 `useState`, 5 `useRef`, 5 `log` -- because the table holds array and Map
// methods rather than hooks and module globals. So the lookup returns the same TRUE the naive flip
// does, at every site that matters.
//
// Recorded because both readings are natural and both are wrong: the gap really does need the
// lattice, not a signature lookup, and the FALSE above stays the conservative answer until it lands.
//
// # enableForest is off, and that is upstream's default rather than a simplification
//
// React's phi branch has an `else if (fn.env.config.enableForest)` arm unioning each phi with each
// operand pairwise. `enableForest` is an experimental config flag defaulting to false, and this
// graph has no config surface at all, so the arm is not ported. It is named here because it is the
// one piece of upstream's phi handling that is absent rather than reproduced, and a reader diffing
// against the bundle would otherwise find it missing with no explanation.
//
// # Termination is NOT a fixpoint, and the proof is structural
//
// There is no worklist here and no iteration to convergence. The pass makes exactly one ordered walk
// over blocks, phis and instructions, and every `Union` it performs is a monotone merge of two
// classes into one. The number of classes therefore decreases by at most one per `Union` and never
// increases, so the walk terminates because the walk is finite, not because a value stops changing.
// This is a different termination argument from `InferMutableRanges.mutate`, whose `seen` map is
// per-call and bounded by two kinds per identifier, and the distinction is stated because that map
// looks hoistable and is not.
//
// `Find` itself recurses, and its recursion is bounded by the tree height, which path compression
// keeps near one. Compression is applied on every `Find` exactly as upstream does, so a chain is
// walked at most once. `TestDisjointFindTerminatesOnALongChain` builds a 10,000-long chain by hand
// and proves the walk returns, because a union-find whose compression is wrong does not give a wrong
// answer, it exhausts the stack -- the same silent-failure shape the ranges pass records for its own
// termination, and the reason both are proven empirically rather than asserted.
//
// # Iteration order, and the map that would otherwise leak into the answer
//
// `Phi.Operands` is a Go map with randomised iteration order, so it is read through
// `PhiOperandsInOrder` here exactly as `reactive.go` reads it. Union is order-INDEPENDENT in its
// final partition -- the classes are the same whichever order the merges happen in -- but the chosen
// REPRESENTATIVE is not, and the representative is what a consumer keys on. A pass whose answer is
// stable but whose key is randomised produces a table that differs between runs, which is the shape
// that makes a cache non-deterministic. Reading in sorted order removes it.
package hir

import "sort"

// DisjointGap names a union rule this pass cannot apply, for a caller that needs to know.
//
// Modelled on `RangeGap` and `ReactiveGap` deliberately, and for the same reason: a gap answerable
// through the API is one a consumer can decline on, while a gap living only in a comment is one the
// next reader inherits by accident.
type DisjointGap uint8

const (
	// DisjointGapPrimitiveCallResult is a call whose result type would have excluded it.
	//
	// Upstream's `mayAllocate` treats a call as allocating unless its lvalue's type is `Primitive`.
	// `Identifier.Type` is nil throughout this tree, so the test is forced to its conservative
	// answer and calls are never counted as allocating on the strength of the shape alone. The
	// consequence for a consumer is a MISSING merge rather than a spurious one: two values upstream
	// would unify through a call's allocated result may appear in separate classes here.
	//
	// See the package comment for why false rather than true is the safe direction.
	DisjointGapPrimitiveCallResult DisjointGap = iota
)

// DisjointGaps are the union rules FindDisjointMutableValues does not apply. See DisjointGap.
//
// Returned as a value rather than documented alone so a test can assert on it, which makes closing a
// gap a visible event rather than a silent improvement.
func DisjointGaps() []DisjointGap {
	return []DisjointGap{DisjointGapPrimitiveCallResult}
}

// DisjointSet is a union-find over values, keyed by IdentifierId.
//
// This is React's `DisjointSet` (line 32144) with the same two operations and the same path
// compression. Membership is sparse: a value that was never passed to `Union` is absent, and `Find`
// answers `0, false` for it rather than inventing a singleton class. That absence is meaningful --
// upstream's consumer spells it `find(id) ?? id`, falling back to the value itself -- and
// `RepresentativeOf` is that fallback written once so every consumer does not re-spell it.
//
// The zero value is an empty set and is ready to use.
type DisjointSet struct {
	parent map[IdentifierId]IdentifierId
}

// Union merges every value in items into one class.
//
// The first item becomes the class representative when none of the items is already in the set,
// which is upstream's rule exactly: it takes `items.shift()`, finds its root, and adopts the rest
// under that root. An empty list is a no-op here; upstream raises an invariant on it, and the
// difference is deliberate because every call site below builds the list conditionally and an empty
// one is the ordinary outcome rather than a bug.
//
// Note that upstream's `union` MUTATES the caller's array through `shift()`. This takes the slice by
// value and never writes to it, which is a difference in the implementation rather than in what it
// decides; the call sites below would be correct under either.
func (d *DisjointSet) Union(items []IdentifierId) {
	if len(items) == 0 {
		return
	}
	if d.parent == nil {
		d.parent = map[IdentifierId]IdentifierId{}
	}

	first := items[0]
	root, ok := d.Find(first)
	if !ok {
		root = first
		d.parent[first] = first
	}

	for _, item := range items[1:] {
		itemParent, present := d.parent[item]
		if !present {
			d.parent[item] = root
			continue
		}
		if itemParent == root {
			continue
		}
		// Walk this item's chain to the root, repointing every link at the new root. Upstream's
		// loop, transcribed: it rewrites the whole path rather than only the item, which is what
		// keeps the structure flat when two established classes merge.
		current := item
		for itemParent != root {
			d.parent[current] = root
			current = itemParent
			itemParent = d.parent[current]
		}
	}
}

// Find returns the representative of a value's class, and whether the value is in the set at all.
//
// Path compression is applied on the way out, which is what keeps the structure flat and what makes
// the recursion depth safe. Upstream returns `null` for an absent value; the second return is that
// null, kept separate from the id because zero is a usable IdentifierId and conflating them would
// make an absent value read as a member of class zero.
func (d *DisjointSet) Find(item IdentifierId) (IdentifierId, bool) {
	if d.parent == nil {
		return 0, false
	}
	parent, ok := d.parent[item]
	if !ok {
		return 0, false
	}
	if parent == item {
		return item, true
	}
	root, ok := d.Find(parent)
	if !ok {
		return 0, false
	}
	d.parent[item] = root
	return root, true
}

// RepresentativeOf returns a value's class representative, falling back to the value itself.
//
// This is upstream's consumer spelling, `aliasedIdentifiers.find(place.identifier) ?? place.identifier`
// (line 43011), written once here so that every consumer does not re-derive the fallback. A value
// that was never unified is its own representative, which is the answer that lets a caller key a map
// on the result without first asking whether the value is a member.
func (d *DisjointSet) RepresentativeOf(item IdentifierId) IdentifierId {
	if root, ok := d.Find(item); ok {
		return root
	}
	return item
}

// Has reports whether a value was ever unified into a class.
func (d *DisjointSet) Has(item IdentifierId) bool {
	if d.parent == nil {
		return false
	}
	_, ok := d.parent[item]
	return ok
}

// Size reports how many values are members of some class.
func (d *DisjointSet) Size() int { return len(d.parent) }

// Sets returns the classes, each sorted, with the outer list ordered by first member.
//
// This is upstream's `buildSets` with a determinism guarantee added. Upstream iterates a `Map` whose
// order is insertion order and therefore stable for it; the backing map here is a Go map, so the
// order is randomised and would leak into anything a caller derived from the result. Both levels are
// sorted for that reason. The classes themselves are identical either way -- only the order is
// pinned, and it is pinned because a non-deterministic ordering is the shape that makes a cache
// non-reproducible without ever producing a wrong answer.
func (d *DisjointSet) Sets() [][]IdentifierId {
	if len(d.parent) == 0 {
		return nil
	}
	members := make([]IdentifierId, 0, len(d.parent))
	for item := range d.parent {
		members = append(members, item)
	}
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })

	grouped := map[IdentifierId][]IdentifierId{}
	var order []IdentifierId
	for _, item := range members {
		root := d.RepresentativeOf(item)
		if _, seen := grouped[root]; !seen {
			order = append(order, root)
		}
		grouped[root] = append(grouped[root], item)
	}

	sets := make([][]IdentifierId, 0, len(order))
	for _, root := range order {
		sets = append(sets, grouped[root])
	}
	return sets
}

// FindDisjointMutableValues groups the values of function that must be treated as one unit.
//
// Requires evaluation order and single-assignment form: call `Construct` first, or take a function
// from `ForFunction`, which does. Infers ranges for itself; a caller holding a range table already
// passes it to `FindDisjointMutableValuesWithRanges` and avoids the second inference.
//
// Nested functions are NOT included, for the same reason `InferMutableRanges` excludes them: an
// IdentifierId names one value in this function and a different value in a nested one, so a set
// spanning two functions would confidently unify two unrelated values. Upstream runs this per
// function for the same reason.
func FindDisjointMutableValues(function *Function) *DisjointSet {
	if function == nil {
		return &DisjointSet{}
	}
	return FindDisjointMutableValuesWithRanges(function, InferMutableRanges(function))
}

// FindDisjointMutableValuesWithRanges is the pass, taking a range table the caller already has.
//
// Faithful to React's walk order: phis before instructions within each block, blocks in
// `Function.Blocks` order. That order is reverse postorder here, and it is safe for the reason the
// ranges pass records: this asks nothing of block order itself. Every predicate reads an
// `EvaluationOrder` from a range or from an instruction, which `MarkEvaluationOrder` assigns as a
// total order independent of the walk that reads it. The `declarations` map below is the one piece
// of state that carries across instructions, and it is order-sensitive in exactly the way upstream's
// is -- first writer wins -- so it is populated by the same walk upstream uses.
func FindDisjointMutableValuesWithRanges(function *Function, ranges *MutableRanges) *DisjointSet {
	scopeIdentifiers := &DisjointSet{}
	if function == nil {
		return scopeIdentifiers
	}

	// declarations maps a source binding to the FIRST value it took, which is what lets a phi reach
	// back to the declaration that introduced the variable. Upstream keys it on `declarationId` and
	// writes only when absent (`declareIdentifier`, line 32381); `DeclarationId` is this graph's
	// counterpart and carries the same meaning, one entry per source-level binding.
	declarations := map[DeclarationId]IdentifierId{}
	declareIdentifier := func(place Place) {
		declaration := declarationOf(function, place.Identifier)
		if _, present := declarations[declaration]; !present {
			declarations[declaration] = place.Identifier
		}
	}

	for _, block := range function.Blocks {
		firstOrder := blockFirstOrder(function, block)

		for _, phi := range block.Phis {
			phiRange := ranges.Get(phi.Place.Identifier)

			// React's two-term test, line 32387. The first term asks whether the range is wider than
			// a single instruction, and it is written as `start + 1 !== end` rather than
			// `end > start + 1` because upstream means "not exactly one wide" -- on an unset range,
			// both fields zero, `start + 1 !== end` is TRUE while `end > start + 1` is false. The
			// second term then rejects it, since an unset end of zero is never greater than a
			// block's first order. Transcribed in upstream's spelling rather than the tidier one, so
			// that the two terms keep their independent meanings.
			//
			// See the package comment for oxc's third disjunct, which is deliberately not applied.
			mutatedAfterCreation := phiRange.Start+1 != phiRange.End && phiRange.End > firstOrder
			if !mutatedAfterCreation {
				// Upstream's `else if (enableForest)` arm sits here and is not ported; the flag
				// defaults to false and this graph has no config surface. See the package comment.
				continue
			}

			operands := []IdentifierId{phi.Place.Identifier}
			if declaration, present := declarations[declarationOf(function, phi.Place.Identifier)]; present {
				operands = append(operands, declaration)
			}
			// Read through PhiOperandsInOrder because `Phi.Operands` is a Go map.
			//
			// # This is DEFENSIVE rather than load-bearing, and a mutation proved it
			//
			// Replacing this with a bare `range phi.Operands` SURVIVES the sweep, and it is a
			// genuine equivalence rather than a fixture gap. The reason is a property of `Union`:
			// the class representative is taken from `items[0]`, which is the phi, and every operand
			// is adopted under that root regardless of the order they are offered in. So neither the
			// partition nor the representative can depend on this loop's order.
			//
			// Verified directly rather than reasoned about: `Union([phi, decl, opA, opB])` and
			// `Union([phi, decl, opB, opA])` produce the same class and the same root. The order
			// that CAN move a representative is the order of separate `Union` CALLS -- `{1,2}` then
			// `{3,2}` roots at 3, while `{3,2}` then `{1,2}` roots at 1 -- and that ordering is the
			// block and instruction walk, which is deterministic already.
			//
			// Kept because it costs nothing, because it is what `reactive.go` does with the same
			// map, and because the equivalence rests on `Union` keeping its current
			// representative rule. A future change there would make this load-bearing again with
			// nothing to announce it.
			for _, blockId := range PhiOperandsInOrder(phi) {
				operands = append(operands, phi.Operands[blockId].Identifier)
			}
			scopeIdentifiers.Union(operands)
		}

		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}

			var operands []IdentifierId

			lvalueRange := ranges.Get(instruction.LValue.Identifier)
			if lvalueRange.End > lvalueRange.Start+1 || mayAllocate(instruction) {
				operands = append(operands, instruction.LValue.Identifier)
			}

			switch value := instruction.Value.(type) {
			case *DeclareLocal:
				declareIdentifier(value.LValue)
			case *DeclareContext:
				declareIdentifier(value.LValue)
			case *StoreLocal:
				operands = appendStore(function, ranges, instruction, declareIdentifier, operands, value.LValue, value.Value)
			case *StoreContext:
				operands = appendStore(function, ranges, instruction, declareIdentifier, operands, value.LValue, value.Value)
			case *Destructure:
				// Upstream walks `eachPatternOperand` of the pattern, declaring each bound place and
				// adding the ones whose range is wider than one. `Destructure` here carries both a
				// `LValue` and a `Pattern` field; `Pattern` is the one lowering fills, and
				// `eachPatternPlace` is the package's own walk over it.
				//
				// # The width test below is LIVE code whose input is absent, not a dead branch
				//
				// A mutation neutralising `placeRange.End > placeRange.Start+1` here SURVIVES, and
				// the reason is measured rather than argued. Across 200 corpus files: 154
				// destructures binding 497 places, of which ZERO carry a range wider than one. Four
				// hand-written shapes that mutate a destructured binding immediately -- object,
				// multi-binding, array, and one inside a loop -- also produce zero.
				//
				// The mechanism, read off the lowered instructions rather than inferred. For
				// `const {a} = o; a.push(1);` lowering emits the Destructure at order 2 binding `a`
				// as identifier 13, and then a SEPARATE `LoadLocal` at order 3 as identifier 14.
				// The mutation widens the load, which gets the range [3,7); the pattern binding
				// keeps [2,3). Single-assignment form puts a fresh identifier between the binding
				// and every use of it, so the widening never lands on the bound place itself.
				//
				// Upstream has the same test because ITS ranges reach the binding: React runs this
				// over a graph where `declarationId` grouping and its own alias propagation put the
				// widened end back on the destructured identifier. Ours does not, today.
				//
				// Kept rather than deleted, following the precedent `ranges.go` sets for
				// `phiOpensBefore` and the `createdFrom` branch: the code is upstream's, it is
				// correct, and it costs nothing, while deleting it means re-deriving it when the
				// input arrives. The verdict expires when the widening reaches a destructured
				// binding -- which is what `RangeGapCreateFromPropagation` closing would do -- and
				// at that point this becomes killable and a fixture is owed.
				eachPatternPlace(value.Pattern, func(place Place, role PlaceRole) {
					if role != PlaceRoleDefine {
						return
					}
					declareIdentifier(place)
					placeRange := ranges.Get(place.Identifier)
					if placeRange.End > placeRange.Start+1 {
						operands = append(operands, place.Identifier)
					}
				})
				operands = appendMutableOperand(ranges, instruction, operands, value.Value)
			case *MethodCall:
				operands = appendMutableOperands(instruction, ranges, operands)
				// The property is added UNCONDITIONALLY, with no range test and no mutability test
				// (line 32411). It is the only place in the whole pass where an operand joins the
				// class on its identity alone. Upstream's reason is that a method call's receiver
				// and the function it resolves to are inseparable: `o.f()` can mutate through `f`,
				// so the property cannot be memoized apart from the call that reaches it.
				operands = append(operands, value.Property.Identifier)
			default:
				operands = appendMutableOperands(instruction, ranges, operands)
			}

			if len(operands) != 0 {
				scopeIdentifiers.Union(operands)
			}
		}
	}

	return scopeIdentifiers
}

// appendStore adds the operands a StoreLocal or StoreContext contributes.
//
// Shared between the two arms because upstream shares them in one `else if` (line 32396): both
// declare their lvalue, both add it when its range is wider than one, and both add the stored value
// when it is still mutable here. Factored rather than duplicated so the two cannot drift.
func appendStore(
	function *Function,
	ranges *MutableRanges,
	instruction *Instruction,
	declareIdentifier func(Place),
	operands []IdentifierId,
	lvalue Place,
	value Place,
) []IdentifierId {
	declareIdentifier(lvalue)
	lvalueRange := ranges.Get(lvalue.Identifier)
	if lvalueRange.End > lvalueRange.Start+1 {
		operands = append(operands, lvalue.Identifier)
	}
	return appendMutableOperand(ranges, instruction, operands, value)
}

// appendMutableOperand adds one operand when it is still being written at this instruction.
//
// This is upstream's paired test, `isMutable(instr, operand) && operand.mutableRange.start > 0`.
// The two halves are not redundant: `isMutable` is `inRange`, which is already false for an unset
// range, but a range legitimately STARTING at zero would pass `inRange` while naming a value with no
// definition point. Upstream carries both and so does this.
func appendMutableOperand(
	ranges *MutableRanges,
	instruction *Instruction,
	operands []IdentifierId,
	operand Place,
) []IdentifierId {
	operandRange := ranges.Get(operand.Identifier)
	if operandRange.Contains(instruction.Order) && operandRange.Start > 0 {
		operands = append(operands, operand.Identifier)
	}
	return operands
}

// appendMutableOperands adds every non-defining operand of the instruction that is still mutable.
//
// This is upstream's `eachInstructionOperand` loop, which yields the value's operands and NOT the
// instruction's own lvalue -- the lvalue is handled by the range test above it and adding it here
// would double-count. `PlaceRoleDefine` is the role that names an lvalue in this graph, so the two
// remaining roles, Use and Receiver, are exactly upstream's operand set.
func appendMutableOperands(
	instruction *Instruction,
	ranges *MutableRanges,
	operands []IdentifierId,
) []IdentifierId {
	EachPlace(instruction.Value, func(place Place, role PlaceRole) {
		if role == PlaceRoleDefine {
			return
		}
		operands = appendMutableOperand(ranges, instruction, operands, place)
	})
	return operands
}

// declarationOf returns the source binding a value belongs to.
//
// One `let` reassigned three times is three IdentifierIds and one DeclarationId, which is the
// grouping upstream's `declarationId` names. A value whose identifier is missing from the table
// answers zero, which groups every such value together -- acceptable because the only consumer is
// the `declarations` map, whose entries are written from real places and read for phis of real
// bindings.
func declarationOf(function *Function, id IdentifierId) DeclarationId {
	if function == nil {
		return 0
	}
	if identifier := function.Identifiers[id]; identifier != nil {
		return identifier.Declaration
	}
	return 0
}

// mayAllocate reports whether an instruction produces a freshly allocated value.
//
// React's `mayAllocate` (line 32318), transcribed arm for arm. A value that allocates gets its own
// scope even when its range is a single instruction, because the allocation itself is the thing that
// must not be repeated across renders: re-running `{}` hands back a new object identity and defeats
// every downstream comparison.
//
// The three call arms diverge from upstream and the reason is recorded as
// `DisjointGapPrimitiveCallResult`. Upstream answers `lvalue.identifier.type.kind !== 'Primitive'`;
// `Identifier.Type` is nil throughout this tree, so the honest answer is the conservative one and
// this returns false for them. See the package comment for why false rather than true.
//
// Written as an explicit type switch with every upstream arm named, including the ones returning
// false, rather than as a short list of the true cases. Upstream's own switch is exhaustive and
// ends in `assertExhaustive`, so an unlisted variant there is a build error; the equivalent
// discipline here is that a reader can diff this switch against the bundle's line by line. The
// default returns false, which is the safe answer for a variant nobody has classified.
func mayAllocate(instruction *Instruction) bool {
	switch value := instruction.Value.(type) {
	case *Destructure:
		// Upstream: `doesPatternContainSpreadElement(value.lvalue.pattern)`. A spread in a
		// destructuring pattern allocates -- `const {a, ...rest} = o` builds `rest` as a new object.
		// Everything else in a pattern only binds existing values.
		return patternContainsSpread(value.Pattern)

	// Upstream returns false for all of these.
	case *PostfixUpdate, *PrefixUpdate, *Await, *DeclareLocal, *DeclareContext, *StoreLocal,
		*LoadGlobal, *MetaProperty, *TypeCastExpression, *LoadLocal, *LoadContext, *StoreContext,
		*PropertyDelete, *ComputedLoad, *ComputedDelete, *JsxText, *TemplateLiteral, *Primitive,
		*GetIterator, *IteratorNext, *NextPropertyOf, *Debugger, *StartMemoize, *FinishMemoize,
		*UnaryExpression, *BinaryExpression, *PropertyLoad, *StoreGlobal:
		return false

	// Upstream returns `lvalue.identifier.type.kind !== 'Primitive'` for these three. Forced false;
	// see DisjointGapPrimitiveCallResult.
	case *TaggedTemplateExpression, *CallExpression, *MethodCall:
		return false

	// Upstream returns true for all of these: each one constructs a value.
	case *RegExpLiteral, *PropertyStore, *ComputedStore, *ArrayExpression, *JsxExpression,
		*JsxFragment, *NewExpression, *ObjectExpression, *UnsupportedNode, *ObjectMethod,
		*FunctionExpression:
		return true

	default:
		return false
	}
}

// patternContainsSpread reports whether a destructuring pattern binds a rest element.
//
// Upstream's `doesPatternContainSpreadElement`. A rest binding allocates a fresh object or array to
// hold the remainder, which is what makes the destructure an allocation site rather than a pure
// rebinding. Nested patterns are walked, because `const {a: {...rest}} = o` allocates just as much
// as the flat form does.
func patternContainsSpread(pattern Pattern) bool {
	switch p := pattern.(type) {
	case *ObjectPattern:
		if p.Rest != nil {
			return true
		}
		for _, property := range p.Properties {
			if patternContainsSpread(property.Value) {
				return true
			}
		}
	case *ArrayPattern:
		if p.Rest != nil {
			return true
		}
		for _, element := range p.Elements {
			if patternContainsSpread(element.Value) {
				return true
			}
		}
	}
	return false
}
