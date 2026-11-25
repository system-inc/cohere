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
// collapse the entangled values into one equivalence class up front, and let every consumer ask a
// single question: what is this value's representative? That is what makes this a union-find rather
// than a reachability walk. The structure is chosen because the consumers only ever ask for a
// canonical name, never for a path.
//
// Two consumers exist upstream, and the second is the one that matters here.
// `inferReactiveScopeVariables` (line 32233) turns each class into a reactive scope.
// `inferReactivePlaces` (line 42887) wraps the result in a `ReactivityMap` whose whole use of it is
// `aliasedIdentifiers.find(place.identifier) ?? place.identifier` (line 43011) -- a representative
// lookup with a fallback to the value itself. `reactivity.go` in this package already declares
// `ReactiveGapAliasing` for exactly that missing input, quoting `findDisjointMutableValues` by name.
// `Find` below is that lookup and `RepresentativeOf` is the fallback form, so the gap closes on the
// API rather than on a comment.
//
// # The input, measured rather than assumed, because an earlier attempt at this stage declined
//
// Stage 3 was dispatched once before and correctly refused to build. At that point the widening half
// of `InferMutableRanges` did not exist, so every mutable range had width exactly one, every
// `end > start + 1` was false for a structural reason, and the pass would have produced 23,256
// members in 23,256 singleton classes: an identity function that compiles, passes fixtures, and
// tells every consumer that nothing is entangled with anything.
//
// The gates this file reads are not the width gate Stage 2.5 measured, so the input was re-measured
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
// # Divergence from oxc: the loop-carried phi disjunct is not applied
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
// # Partial divergence: call-result types are projected from effect signatures
//
// Upstream's `mayAllocate` returns `lvalue.identifier.type.kind !== 'Primitive'` for
// `CallExpression`, `MethodCall` and `TaggedTemplateExpression`. `InferTypes` obtains that result
// type by unifying the call with the callee's function return type. An unresolved return remains a
// `Type`, so upstream treats an unknown call as allocating; only a proven primitive is excluded.
//
// This IR does not store that type lattice on identifiers, but its effect registry transcribes the
// same built-in signatures and records whether each result is primitive or mutable. The call arms
// therefore use `lookupSignature`: a represented primitive result does not allocate, while a known
// mutable or unknown result does. This preserves upstream's conservative default and distinguishes
// the built-ins already modelled here without maintaining a second return-kind table.
//
// The projection is not total. The effect registry is a measured subset of upstream's global
// shapes, so an omitted primitive signature is treated as allocating. It is also keyed by syntax
// name rather than receiver shape, so a same-named method on an unrelated object can inherit a
// built-in's primitive result. `DisjointGapPrimitiveCallResult` exposes those two residual error
// directions until identifier types or a shape-directed signature lookup lands.
//
// # enableForest is off, and that is upstream's default rather than a simplification
//
// React's phi branch has an `else if (fn.env.configuration.enableForest)` arm unioning each phi with each
// operand pairwise. `enableForest` is an experimental config flag defaulting to false, and this
// graph has no config surface at all, so the arm is not ported. It is named here because it is the
// one piece of upstream's phi handling that is absent rather than reproduced, and a reader diffing
// against the bundle would otherwise find it missing with no explanation.
//
// # Termination is not a fixpoint, and the proof is structural
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
// # Iteration order
//
// `Phi.Operands` is kept sorted by predecessor block id, and it is read through
// `PhiOperandsInOrder` here exactly as `reactivity.go` reads it. Union is order-independent in its
// final partition -- the classes are the same whichever order the merges happen in -- but the chosen
// representative is not, and the representative is what a consumer keys on. A pass whose answer is
// stable but whose key varies between runs produces a table that differs between runs, which is the
// shape that makes a cache non-deterministic. Reading in a fixed order removes it.
package high_level_intermediate_representation

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// DisjointGap names a union rule this pass cannot apply, for a caller that needs to know.
//
// Modelled on `RangeGap` and `ReactiveGap` deliberately, and for the same reason: a gap answerable
// through the API is one a consumer can decline on, while a gap living only in a comment is one the
// next reader inherits by accident.
type DisjointGap uint8

const (
	// DisjointGapPrimitiveCallResult records that call-result types are projected from the partial,
	// name-keyed effect signature registry rather than read from an inferred identifier type.
	//
	// Upstream's `mayAllocate` treats a call as allocating unless its lvalue's type is `Primitive`.
	// This pass recognizes primitive results present in `lookupSignature` and treats every unknown
	// result as allocating. Missing primitive signatures can therefore add a spurious merge, while a
	// name collision with a built-in method can omit a merge upstream's shape lookup would add.
	// See the package comment for the source of both residual directions.
	DisjointGapPrimitiveCallResult DisjointGap = iota
)

// DisjointGaps are the union rules FindDisjointMutableValues does not apply. See DisjointGap.
//
// Returned as a value rather than documented alone so a test can assert on it, which makes closing a
// gap a visible event rather than a silent improvement.
func DisjointGaps() []DisjointGap {
	return []DisjointGap{DisjointGapPrimitiveCallResult}
}

// FindDisjointMutableValues groups the values of function that must be treated as one unit.
//
// Requires evaluation order and single-assignment form: call `Construct` first, or take a function
// from `ForFunction`, which does. Infers ranges for itself; a caller holding a range table already
// passes it to `FindDisjointMutableValuesWithRanges` and avoids the second inference.
//
// Nested functions are not included, for the same reason `InferMutableRanges` excludes them: an
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
func FindDisjointMutableValuesWithRanges(function *Function, ranges *mutation_aliasing.MutableRanges) *DisjointSet {
	scopeIdentifiers := &DisjointSet{}
	if function == nil {
		return scopeIdentifiers
	}

	// declarations maps a source binding to the first value it took, which is what lets a phi reach
	// back to the declaration that introduced the variable. Upstream keys it on `declarationId` and
	// writes only when absent (`declareIdentifier`, line 32381); `DeclarationId` is this graph's
	// counterpart and carries the same meaning, one entry per source-level binding.
	declarations := map[static_single_assignment.DeclarationId]static_single_assignment.IdentifierId{}
	producers := newCalleeProducers(function)
	declareIdentifier := func(place Place) {
		declaration := declarationOf(function, place.Identifier)
		if _, present := declarations[declaration]; !present {
			declarations[declaration] = place.Identifier
		}
	}

	for _, block := range function.Blocks {
		firstOrder := mutation_aliasing.BlockFirstOrder(rangeGraph{}, function, block)

		for _, phi := range block.Phis {
			phiRange := ranges.Get(phi.Place.Identifier)

			// React's two-term test, line 32387. The first term asks whether the range is wider than
			// a single instruction, and it is written as `start + 1 !== end` rather than
			// `end > start + 1` because upstream means "not exactly one wide" -- on an unset range,
			// both fields zero, `start + 1 !== end` is true while `end > start + 1` is false. The
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

			operands := []static_single_assignment.IdentifierId{phi.Place.Identifier}
			if declaration, present := declarations[declarationOf(function, phi.Place.Identifier)]; present {
				operands = append(operands, declaration)
			}
			// Read through PhiOperandsInOrder, ascending predecessor order, the order
			// `Phi.Operands` keeps.
			//
			// # The order is defensive rather than load-bearing
			//
			// Neither the partition nor the representative depends on this loop's order, a property
			// of `Union`: the class representative is taken from `items[0]`, which is the phi, and
			// every operand is adopted under that root regardless of the order they are offered in.
			//
			// Verified directly rather than reasoned about: `Union([phi, decl, opA, opB])` and
			// `Union([phi, decl, opB, opA])` produce the same class and the same root. The order
			// that can move a representative is the order of separate `Union` calls -- `{1,2}` then
			// `{3,2}` roots at 3, while `{3,2}` then `{1,2}` roots at 1 -- and that ordering is the
			// block and instruction walk, which is deterministic already.
			//
			// A fixed order is kept because it is what `reactivity.go` does too, and because the
			// equivalence rests on `Union` keeping its current representative rule. A future change
			// there would make the order load-bearing with nothing to announce it.
			for _, blockId := range PhiOperandsInOrder(phi) {
				operands = append(operands, phi.Operands.At(blockId).Identifier)
			}
			scopeIdentifiers.Union(operands)
		}

		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}

			var operands []static_single_assignment.IdentifierId

			lvalueRange := ranges.Get(instruction.LValue.Identifier)
			if lvalueRange.End > lvalueRange.Start+1 || mayAllocate(function, producers, instruction) {
				operands = append(operands, instruction.LValue.Identifier)
			}

			switch instructionValue := instruction.Value.(type) {
			case *DeclareLocal:
				declareIdentifier(instructionValue.LValue)
			case *DeclareContext:
				declareIdentifier(instructionValue.LValue)
			case *StoreLocal:
				operands = appendStore(
					function,
					ranges,
					instruction,
					declareIdentifier,
					operands,
					instructionValue.LValue,
					instructionValue.Value,
				)
			case *StoreContext:
				operands = appendStore(
					function,
					ranges,
					instruction,
					declareIdentifier,
					operands,
					instructionValue.LValue,
					instructionValue.Value,
				)
			case *Destructure:
				// Upstream walks `eachPatternOperand` of the pattern, declaring each bound place and
				// adding the ones whose range is wider than one. `Destructure` here carries both a
				// `LValue` and a `Pattern` field; `Pattern` is the one lowering fills, and
				// `eachPatternPlace` is the package's own walk over it.
				//
				// # The width test below is live
				//
				// Destructure now emits CreateFrom for every ordinary binding. A later mutation
				// through a LoadLocal therefore reaches the bound place, giving this width test the
				// same input upstream reads rather than leaving every pattern range one instruction.
				eachPatternPlace(instructionValue.Pattern, func(place Place, role PlaceRole) {
					if role != PlaceRoleDefine {
						return
					}
					declareIdentifier(place)
					placeRange := ranges.Get(place.Identifier)
					if placeRange.End > placeRange.Start+1 {
						operands = append(operands, place.Identifier)
					}
				})
				operands = appendMutableOperand(ranges, instruction, operands, instructionValue.Value)
			case *MethodCall:
				operands = appendMutableOperands(instruction, ranges, operands)
				// The property is added unconditionally, with no range test and no mutability test
				// (line 32411). It is the only place in the whole pass where an operand joins the
				// class on its identity alone. Upstream's reason is that a method call's receiver
				// and the function it resolves to are inseparable: `o.f()` can mutate through `f`,
				// so the property cannot be memoized apart from the call that reaches it.
				operands = append(operands, instructionValue.Property.Identifier)
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
	ranges *mutation_aliasing.MutableRanges,
	instruction *Instruction,
	declareIdentifier func(Place),
	operands []static_single_assignment.IdentifierId,
	lvalue Place,
	value Place,
) []static_single_assignment.IdentifierId {
	declareIdentifier(lvalue)
	extended := operands
	lvalueRange := ranges.Get(lvalue.Identifier)
	if lvalueRange.End > lvalueRange.Start+1 {
		extended = append(extended, lvalue.Identifier)
	}
	return appendMutableOperand(ranges, instruction, extended, value)
}

// appendMutableOperand adds one operand when it is still being written at this instruction.
//
// This is upstream's paired test, `isMutable(instr, operand) && operand.mutableRange.start > 0`.
// The two halves are not redundant: `isMutable` is `inRange`, which is already false for an unset
// range, but a range legitimately starting at zero would pass `inRange` while naming a value with no
// definition point. Upstream carries both and so does this.
func appendMutableOperand(
	ranges *mutation_aliasing.MutableRanges,
	instruction *Instruction,
	operands []static_single_assignment.IdentifierId,
	operand Place,
) []static_single_assignment.IdentifierId {
	operandRange := ranges.Get(operand.Identifier)
	if operandRange.Contains(instruction.Order) && operandRange.Start > 0 {
		return append(operands, operand.Identifier)
	}
	return operands
}

// appendMutableOperands adds every non-defining operand of the instruction that is still mutable.
//
// This is upstream's `eachInstructionOperand` loop, which yields the value's operands and not the
// instruction's own lvalue -- the lvalue is handled by the range test above it and adding it here
// would double-count. `PlaceRoleDefine` is the role that names an lvalue in this graph, so the two
// remaining roles, Use and Receiver, are exactly upstream's operand set.
func appendMutableOperands(
	instruction *Instruction,
	ranges *mutation_aliasing.MutableRanges,
	operands []static_single_assignment.IdentifierId,
) []static_single_assignment.IdentifierId {
	extended := operands
	EachPlace(instruction.Value, func(place Place, role PlaceRole) {
		if role == PlaceRoleDefine {
			return
		}
		extended = appendMutableOperand(ranges, instruction, extended, place)
	})
	return extended
}

// declarationOf returns the source binding a value belongs to.
//
// One `let` reassigned three times is three IdentifierIds and one DeclarationId, which is the
// grouping upstream's `declarationId` names. A value whose identifier is missing from the table
// answers zero, which groups every such value together -- acceptable because the only consumer is
// the `declarations` map, whose entries are written from real places and read for phis of real
// bindings.
func declarationOf(
	function *Function,
	id static_single_assignment.IdentifierId,
) static_single_assignment.DeclarationId {
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
// The three call arms project upstream's inferred result-kind check through this package's effect
// signature registry. The remaining incompleteness is recorded as
// `DisjointGapPrimitiveCallResult`; see the package comment.
//
// Written as an explicit type switch with every upstream arm named, including the ones returning
// false, rather than as a short list of the true cases. Upstream's own switch is exhaustive and
// ends in `assertExhaustive`, so an unlisted variant there is a build error; the equivalent
// discipline here is that a reader can diff this switch against the bundle's line by line. The
// default returns false, which is the safe answer for a variant nobody has classified.
func mayAllocate(function *Function, producers *calleeProducers, instruction *Instruction) bool {
	if instruction == nil {
		return false
	}
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

	// Upstream returns `lvalue.identifier.type.kind !== 'Primitive'` for these three. Call result
	// kinds come from the same signature lookup that drives aliasing effects; an unknown signature
	// leaves the result non-primitive, which is upstream's conservative default too.
	case *TaggedTemplateExpression, *CallExpression, *MethodCall:
		return !callResultIsPrimitive(function, producers, instruction)

	// Upstream returns true for all of these: each one constructs a value.
	case *RegExpLiteral, *PropertyStore, *ComputedStore, *ArrayExpression, *JsxExpression,
		*JsxFragment, *NewExpression, *ObjectExpression, *UnsupportedNode, *ObjectMethod,
		*FunctionExpression:
		return true

	default:
		return false
	}
}

// callResultIsPrimitive projects the result kind from the call signature used by effect inference.
// The lookup is deliberately shared: maintaining a second name table here would let scope
// allocation disagree with the mutation model for the same callee.
func callResultIsPrimitive(function *Function, producers *calleeProducers, instruction *Instruction) bool {
	var callee Place
	switch value := instruction.Value.(type) {
	case *CallExpression:
		callee = value.Callee
	case *MethodCall:
		callee = value.Property
	case *TaggedTemplateExpression:
		callee = value.Tag
	default:
		return false
	}
	name := calleeName(function, instruction, callee)
	if name == "" {
		name = taggedTemplateCalleeSyntaxName(instruction)
	}
	signature, known := lookupSignature(function, producers, instruction, name)
	return known && signature.Result == mutation_aliasing.EffectValuePrimitive
}

// taggedTemplateCalleeSyntaxName covers the call form `calleeSyntaxName` does not inspect.
// Lowering stores a global tag in an unnamed temporary, so the syntax is the only surviving name
// for a direct built-in such as String. Property tags are left unknown: `lookupSignature` cannot
// validate their receiver shape and treating a property name as a global would be less accurate.
func taggedTemplateCalleeSyntaxName(instruction *Instruction) string {
	if instruction == nil || instruction.Node == nil ||
		instruction.Node.Kind != ast.KindTaggedTemplateExpression {
		return ""
	}
	tag := instruction.Node.AsTaggedTemplateExpression().Tag
	if tag == nil || tag.Kind != ast.KindIdentifier {
		return ""
	}
	return tag.Text()
}

// patternContainsSpread reports whether a destructuring pattern binds a rest element.
//
// Upstream's `doesPatternContainSpreadElement`. A rest binding allocates a fresh object or array to
// hold the remainder, which is what makes the destructure an allocation site rather than a pure
// rebinding. Nested patterns are walked, because `const {a: {...rest}} = o` allocates just as much
// as the flat form does.
func patternContainsSpread(pattern Pattern) bool {
	switch typed := pattern.(type) {
	case *ObjectPattern:
		if typed.Rest != nil {
			return true
		}
		for _, property := range typed.Properties {
			if patternContainsSpread(property.Value) {
				return true
			}
		}
	case *ArrayPattern:
		if typed.Rest != nil {
			return true
		}
		for _, element := range typed.Elements {
			if patternContainsSpread(element.Value) {
				return true
			}
		}
	}
	return false
}
