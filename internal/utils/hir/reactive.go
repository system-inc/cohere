// Reactivity inference: which values can change between renders.
//
// This is React's `InferReactivePlaces.ts`, run over this graph to fill `Place.Reactive`, which
// lowering leaves false everywhere. oxc transcribes the same pass at
// `oxc_react_compiler/src/react_compiler_inference/infer_reactive_places.rs`; where the two
// disagree React wins, and each disagreement is recorded at the line that resolves it.
//
// A value is reactive when it derives from something that genuinely varies between renders. The
// sources are the function's own parameters (a component's props, a hook's arguments), the return
// value of a hook call, and the `use` operator. Reactivity then propagates: an instruction with any
// reactive operand produces a reactive result, a phi is reactive when any operand is, and a value
// assigned under a branch whose test is reactive becomes reactive even when its own operands are
// not. Everything not reachable from a source stays false.
//
// # The source set is React's, and the middle two entries are NOT what a summary of this pass says
//
// It is worth stating the sources as upstream actually computes them, because the natural
// paraphrase - "props, state, context values, and hook return values" - is four things and the code
// is two.
//
// `useState`'s state element and `useContext`'s value are NOT separate sources. They are reactive
// because they are the result of a hook CALL, which is the one rule that covers all three. Writing
// state and context as their own sources would be redundant with the hook rule for the cases the
// hook rule already reaches, and wrong for the cases it does not: a state value destructured out of
// a call this pass cannot see as a hook does not become reactive by being called state.
//
// The inverse matters more, and it is the reason `stable` below exists. A `useState` call returns a
// tuple whose SETTER is deliberately NOT reactive, because React guarantees the setter's identity
// is stable across renders. So the hook rule marks the whole call result reactive and the stability
// side-map then exempts the individual elements React promises never change: the setter from
// `useState`, the dispatch from `useReducer`, the ref object from `useRef`. Without that exemption
// every `setX` in the tree is reactive and every dependency array containing one is wrong.
//
// # Verify has no shape registry, and the checker replaces it for the stable half only
//
// Upstream identifies all of this through `shapeId`: `BuiltInUseState`, `BuiltInUseRefObject` and
// friends, seeded into an `Environment` and propagated by 1,450 lines of type unification. Verify
// has none of that and does not need most of it, because the checker already knows.
//
// The split, measured on our own checker rather than assumed. Probed by printing the type alias and
// type symbol of every binding in a component using each hook, against a `react.d.ts` carrying the
// declarations `@types/react` actually ships. The control for that probe is `setCount`: it must
// report `Dispatch`, and when it did not the module had failed to resolve and every other row was an
// artifact rather than a fact. That happened on the first run of it, so the control is the reason
// this table is trustworthy:
//
//	setCount     alias=Dispatch        symbol=<internal marker>     a useState setter
//	dispatch     alias=ActionDispatch  symbol=<internal marker>     a useReducer dispatch
//	myRef        alias=<nil>           symbol=RefObject             a useRef result
//	count        alias=<nil>           symbol=<nil>                 the state VALUE
//	ctx          alias=<nil>           symbol=<nil>                 a useContext result
//	memo         alias=<nil>           symbol=<nil>                 a useMemo result
//	plain        alias=<nil>           symbol=<nil>                 an ordinary call result
//
// Two things follow and both are load-bearing.
//
// The stable values are identifiable and the reactive ones are not. `Dispatch` is a type ALIAS and
// `RefObject` is an INTERFACE, so they land in different fields, which is why the predicate below
// asks the alias first and falls back to the symbol. That fallback is not this file's invention:
// `internal/utils/typecheck/specifier.go:39` has done exactly this since it was vendored, and three
// agents have now re-derived it independently. It is reached here through `stableTypeName` rather
// than copied, so a fourth does not.
//
// And the reactive values are indistinguishable from any other call result, which is why the hook
// test below keys on the CALLEE rather than on the result. `useContext<T>(c): T` returns a bare
// generic; there is no type on `ctx` that separates it from `plain`. What the checker does give is
// the callee's own symbol name, `useState`, `useContext`, `useCustomThing`, which is precisely
// React's own naming convention and is what `hir.FunctionKind` already classifies functions by. So
// a hook call is a call whose callee resolves to a name matching React's hook pattern. That is a
// weaker test than upstream's shape registry and it is weaker in a specific, stated direction; see
// `isHookCallee`.
//
// # What this pass does NOT do, and why that is a decline rather than an omission
//
// Upstream's loop has a third marking rule this one does not run: after the operand and control
// tests, it walks every operand of a reactive instruction and marks those whose `Place.Effect` is
// one of Capture, Store, ConditionallyMutate, ConditionallyMutateIterator or Mutate, gated on the
// operand being within its identifier's `mutableRange`. That is how mutation through a reactive
// argument makes the mutated object reactive: `const a = []; foo(a, props.x);` makes `a` reactive.
//
// It is not implemented, and it is not implementable here today. `Place.Effect` is `EffectUnknown`
// on every place lowering produces - measured, 144,235 of 144,235 places over 1,945 functions of
// real TypeScript, with the same probe reporting zero places carrying any other effect, so that is
// a two-sided count rather than a filter that cannot match. Upstream does not treat Unknown as a
// default to skip: it reaches `CompilerError.invariant(false, {reason: 'Unexpected unknown
// effect'})`, and oxc returns `unexpected_unknown_effect`. Both implementations consider this state
// impossible.
//
// So the honest options were to run the block over uniformly-Unknown effects, which reproduces
// neither implementation and marks nothing while looking implemented, or to state the gap. The gap
// is stated. `ReactiveGap` below names it in the API rather than in a comment alone, so a caller
// that needs mutation-derived reactivity can ask instead of discovering it.
//
// The consequence is a strict UNDER-approximation, which is the safe direction for the consumers
// this exists for: a validator asking "is this dependency reactive" gets false where upstream gets
// true only for values made reactive solely by being mutated with a reactive argument. It never
// reports reactive where upstream reports non-reactive. `TestReactiveIsAnUnderApproximation` pins
// that direction.
//
// Filling `Place.Effect` was deliberately NOT attempted as part of this. The `Effect` doc comment
// in hir.go explains why at length: `Effect` is an OUTPUT of an abstract interpretation over a
// disjoint `ValueKind` lattice whose join is not a max over the declaration order, and a pass that
// invented effects to feed this one would be self-consistent and wrong.
//
// # Convergence equality is not identity equality, and getting it wrong hangs rather than fails
//
// The fixpoint's value is a set of `IdentifierId`, and "unchanged" means no id was added to it.
// That is deliberate and it is the same decision upstream hand-writes a `PartialEq` for, for the
// same reason `refs.go` records: a comparison including spans or freshly-minted identifiers reports
// "changed" forever and the loop never terminates. Note the failure is silent - not a wrong answer,
// a hang - so the shape of the comparison is a correctness property rather than a tidiness one.
//
// Upstream's `ReactivityMap` makes the same choice visibly. Its `isReactive` has a side effect,
// writing `place.reactive = true`, but `hasChanges` flips ONLY in `markReactive` and ONLY when an
// id is newly inserted into the set. Marking a place whose identifier is already in the set is not
// a change. Reproduced exactly by `reactivity.mark` below.
//
// One divergence from upstream here, stated because it is a real difference and not a
// simplification. Upstream keys its set on the identifier found through a disjoint-set union of
// aliased mutable values, `findDisjointMutableValues`, so two identifiers unified by an aliasing
// relation share one reactive bit. That union is built entirely from `mutableRange`, which does not
// exist here, so this keys on the identifier directly. Single-assignment form already gives one
// value per definition, which is what makes the direct keying sound for propagation through loads,
// stores and phis; what is lost is exactly the aliasing the effect block above also needs, so the
// two gaps are the same gap and close together.
//
// # Iteration order, and the map that would otherwise leak into the answer
//
// `Phi.Operands` is a Go map and its iteration order is randomised. Upstream BREAKS out of the
// operand loop at the first reactive operand, so an order-dependent read of that loop is a real
// hazard rather than a theoretical one. It does not change THIS pass's answer, because the result
// is "was any operand reactive", which is order-independent - but the predecessor loop underneath
// it is order-dependent in upstream's own code, and the fix is free: both loops read operands
// through `PhiOperandsInOrder`, so the walk is deterministic whatever a later reader adds to it.
package hir

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
)

// ReactiveGap names a reactivity source this pass cannot compute, for a caller that needs to know.
//
// It exists so that "we do not do this" is answerable through the API rather than only by reading
// the comment above. A validator that would report differently in the presence of mutation-derived
// reactivity can check for the gap and decline, rather than silently accepting an
// under-approximation as complete.
type ReactiveGap uint8

const (
	// ReactiveGapMutation is reactivity that arises from mutating a value with a reactive operand.
	//
	// Upstream marks a mutable operand of a reactive instruction as reactive. That requires
	// `Place.Effect` and an identifier's mutable range, neither of which is computed in this tree;
	// see the package comment for the measurement.
	ReactiveGapMutation ReactiveGap = iota
	// ReactiveGapAliasing is reactivity shared between values unified by aliasing.
	//
	// Upstream keys its reactive set on a disjoint-set representative built from mutable ranges, so
	// two aliased values share one bit. Same missing input as ReactiveGapMutation.
	ReactiveGapAliasing
)

// ReactiveGaps are the sources InferReactive does not compute. See ReactiveGap.
//
// Returned as a value rather than documented alone so a caller can assert on it: a test that reads
// this list fails when the set changes, which is what makes closing a gap a visible event.
func ReactiveGaps() []ReactiveGap {
	return []ReactiveGap{ReactiveGapMutation, ReactiveGapAliasing}
}

// InferReactive fills Place.Reactive for every place in function and its nested functions.
//
// Requires single-assignment form: call `Construct` first, or take a function from `ForFunction`,
// which does. Over a graph that is not in single-assignment form the propagation through loads and
// stores is still applied but the phis it depends on do not exist, so the answer is an
// under-approximation of an under-approximation and means very little.
//
// The type checker is used to identify hook calls and stable values, and may be nil. With no
// checker the stable exemption cannot be made and hook calls cannot be recognised, so only
// parameters and what derives from them are reactive. That is a strictly smaller answer rather than
// a wrong one, and it is the same degradation `Lower` documents for a checker-less lowering.
//
// Idempotent, unlike `Construct`: running it twice sets the same bits. It mutates only `Reactive`.
func InferReactive(function *Function, typeChecker *shimchecker.Checker) {
	if function == nil {
		return
	}

	state := &reactivity{
		function:    function,
		typeChecker: typeChecker,
		reactive:    map[IdentifierId]bool{},
		stable:      map[IdentifierId]bool{},
	}
	state.run()
}

// reactivity is the fixpoint's state: the set of reactive values and the stable exemptions.
type reactivity struct {
	function    *Function
	typeChecker *shimchecker.Checker

	// reactive is the fixpoint value. Membership is by IdentifierId alone, which is what makes
	// "unchanged" cheap and, more importantly, TERMINATING. See the package comment.
	reactive map[IdentifierId]bool

	// stable holds values React guarantees do not change identity between renders, so that they are
	// exempted from being marked even when they come out of a reactive hook call.
	stable map[IdentifierId]bool

	// changed is set only when an id is newly inserted into reactive. Reading a value that is
	// already reactive is not a change; that asymmetry is upstream's and it is what terminates.
	changed bool

	// rounds counts the sweeps the fixpoint took, for measurement. `controlflow.Solve` exposes the
	// same number for the same reason: it is what says whether the iteration order is doing its job,
	// and a lattice taking many more rounds than its height suggests is a lattice being walked in
	// the wrong order.
	rounds int
}

func (r *reactivity) run() {
	// Parameters are the root source: a component's props and a hook's arguments both arrive here,
	// and both can differ between renders. Upstream marks the place behind a spread parameter as
	// well as a plain one, which `Function.Params` already flattens for us.
	for index := range r.function.Params {
		r.mark(r.function.Params[index].Identifier)
	}

	// The frontier depends only on the control-flow graph, which nothing here mutates, so it is
	// computed once. Which of those frontier blocks branches on a REACTIVE test changes as the set
	// grows, so `controlled` is refreshed every round from the frontier rather than cached.
	frontiers := r.postDominatorFrontiers()
	controlled := make(map[BlockId]bool, len(frontiers))

	// Bounded rather than unbounded, for the reason `controlflow.Solve` states in its own header: a
	// linter that stops and says which function it gave up on is debuggable, one that spins on a
	// file in a build is not. The lattice is a set that only grows, so it converges in at most as
	// many rounds as there are identifiers; the bound is far above that and exists to turn a future
	// non-monotone edit into a stop rather than a hang.
	//
	// This is NOT upstream's bound. Upstream's `do/while` is unbounded here (its ten-round cap is
	// in a different pass, the one `refs.go` reproduces), and on a set that only grows that is
	// sound. The bound is additive safety, not a behavioural difference: it is never reached, and
	// `TestReactiveConvergesInFewRounds` records what real code actually costs.
	for {
		r.rounds++
		if r.rounds > maximumReactiveRounds {
			return
		}
		r.changed = false
		// Refreshed before the sweep so a test that became reactive in the previous round makes
		// its dependent blocks controlled in this one. `controlled` only ever gains entries, which
		// is what keeps the whole fixpoint monotone.
		r.refreshControlled(frontiers, controlled)
		for _, block := range r.function.Blocks {
			r.visitBlock(block, controlled)
		}
		if !r.changed {
			break
		}
	}

	r.applyToPlaces(r.function, true)
}

// maximumReactiveRounds bounds the fixpoint. See the reason at the loop.
const maximumReactiveRounds = 10000

// visitBlock runs one block's phis and instructions against the current reactive set.
func (r *reactivity) visitBlock(block *BasicBlock, controlled map[BlockId]bool) {
	hasReactiveControl := controlled[block.Id]

	for _, phi := range block.Phis {
		if r.isReactive(phi.Place.Identifier) {
			continue
		}

		// Read through PhiOperandsInOrder rather than ranging the map. The answer here does not
		// depend on order, but the predecessor loop below does, and a deterministic walk costs
		// nothing. See the package comment.
		operandOrder := PhiOperandsInOrder(phi)

		isPhiReactive := false
		for _, predecessorId := range operandOrder {
			if r.isReactive(phi.Operands[predecessorId].Identifier) {
				isPhiReactive = true
				break
			}
		}
		if isPhiReactive {
			r.mark(phi.Place.Identifier)
			continue
		}

		// A phi whose operands are all non-reactive is STILL reactive when control reached it
		// through a branch on a reactive test, because which value it holds then depends on
		// something that varies between renders. `let x = 1; if (props.c) { x = 2; } use(x);` makes
		// `x` reactive with two constant operands.
		for _, predecessorId := range operandOrder {
			if controlled[predecessorId] {
				r.mark(phi.Place.Identifier)
				break
			}
		}
	}

	for _, instructionId := range block.Instructions {
		instruction := r.function.Instructions[instructionId]
		if instruction == nil {
			continue
		}
		r.recordStable(instruction)

		hasReactiveInput := false
		EachPlace(instruction.Value, func(place Place, role PlaceRole) {
			// No early exit: upstream evaluates isReactive on EVERY operand rather than stopping at
			// the first, because its isReactive writes the flag onto the place as a side effect and
			// stopping would leave later operands unflagged. This pass writes flags in a separate
			// pass at the end, so the side effect is gone, but the traversal is kept identical so
			// that a reader comparing the two files is not asked to re-derive why they differ.
			if r.isReactive(place.Identifier) {
				hasReactiveInput = true
			}
		})

		// A hook call is a source in its own right, whatever its arguments are: it can read state
		// or context, so its result can differ between renders even with constant operands. This is
		// the rule that makes `useState`'s value, `useContext`'s value and every custom hook's
		// return reactive, and it is why none of those is listed separately.
		switch value := instruction.Value.(type) {
		case *CallExpression:
			if r.isHookCallee(value.Callee.Identifier) {
				hasReactiveInput = true
			}
		case *MethodCall:
			// Upstream tests the PROPERTY rather than the receiver, so `React.useState(0)` is a
			// hook call while `state.useState` as a receiver is not.
			if r.isHookCallee(value.Property.Identifier) {
				hasReactiveInput = true
			}
		}

		if hasReactiveInput {
			eachInstructionLValue(instruction, func(place Place) {
				// The stable exemption is what keeps a `useState` setter non-reactive even though
				// it is destructured out of a hook call this pass has just marked reactive.
				if r.stable[place.Identifier] {
					return
				}
				r.mark(place.Identifier)
			})
		}

		// Upstream's third marking rule runs here, over `hasReactiveInput || hasReactiveControl`,
		// and marks mutable operands. It is not implemented: every Place.Effect in this tree is
		// EffectUnknown, which upstream treats as an invariant violation rather than as a value to
		// skip. See ReactiveGapMutation and the package comment for the measurement.
		_ = hasReactiveControl
	}
}

// isReactive reports whether a value is in the reactive set.
//
// Unlike upstream's, this has NO side effect on any place. Upstream's `isReactive` writes
// `place.reactive = true` as it goes, which is why its traversal is careful to visit operands it
// does not otherwise need; this pass writes every flag once at the end from the settled set, in
// `applyToPlaces`. The separation is what makes this idempotent where `Construct` is not.
func (r *reactivity) isReactive(id IdentifierId) bool { return r.reactive[id] }

// mark adds a value to the reactive set, recording a change only if it was not already there.
//
// The asymmetry is the termination condition and it is upstream's. See the package comment.
func (r *reactivity) mark(id IdentifierId) {
	if r.reactive[id] {
		return
	}
	r.reactive[id] = true
	r.changed = true
}

// recordStable notes values whose identity React guarantees is stable across renders.
//
// This is upstream's `StableSidemap`, reduced to what the checker can answer. Upstream tracks two
// kinds: a stable value itself (a setter, a ref object) and a CONTAINER of one (the tuple
// `useState` returns), propagating through destructuring, property loads, and local copies. Here
// the container half is unnecessary: the checker types each destructured binding directly, so the
// element that is a `Dispatch` is identifiable at the point it is bound without tracking the tuple
// it came out of.
//
// # The alias propagation is upstream's and is NOT reproduced, because the checker subsumes it
//
// Upstream also propagates stability along `LoadLocal` and `StoreLocal`, so `const a = setX;` keeps
// `a` stable by copying a map entry. That was written here first and then deleted, because a mutant
// removing it SURVIVED the whole suite while a mutant removing the checker test was caught, and
// mutating both together failed the same five assertions as the checker test alone.
//
// Measured rather than argued: for `const [count, setCount] = useState(0); const aliased = setCount;
// const chained = aliased;` the checker reports setCount, aliased AND chained all as
// typeName="Dispatch", and count as none. TypeScript propagates the type along the assignment
// itself, at any chain depth, so asking the checker at each binding returns exactly what the
// side-map would have carried.
//
// This is the README's "fidelity is to what a rule DECIDES, not how it OBTAINS what it needs":
// upstream needs the side-map because it has no checker to ask. Keeping it here would be a second
// mechanism holding one fact, which is the redundancy that leaves a mutation sweep unable to see
// either copy.
func (r *reactivity) recordStable(instruction *Instruction) {
	// Every value the instruction binds is asked of the checker directly. A destructured setter is
	// bound by a Destructure whose pattern places are lvalues, so this reaches them without the
	// pattern being walked here a second time.
	eachInstructionLValue(instruction, func(place Place) {
		if r.isStableType(place.Identifier) {
			r.stable[place.Identifier] = true
		}
	})
}

// isStableType asks the checker whether a value is one React guarantees is identity-stable.
//
// The three names are upstream's stable set reduced to what `@types/react` actually spells:
// `Dispatch` for a `useState` setter, `ActionDispatch` for a `useReducer` dispatch, and `RefObject`
// for a `useRef` result. Upstream's set also holds `startTransition` and `useOptimistic`'s setter,
// which are `TransitionStartFunction` and a bare function type respectively; the first is included
// and the second is not reachable by name, which is recorded rather than hidden.
//
// The alias-then-symbol fallback is the measured part and it is why this does not simply read one
// field: `Dispatch` is a type ALIAS and lands in the alias, `RefObject` is an INTERFACE and lands
// in the symbol. A predicate keyed on either one alone silently answers false for the other, which
// has now happened to three separate agents in this tree.
func (r *reactivity) isStableType(id IdentifierId) bool {
	name := r.stableTypeName(id)
	switch name {
	case "Dispatch", "ActionDispatch", "RefObject", "TransitionStartFunction":
		return true
	}
	return false
}

// stableTypeName returns a value's type alias name, falling back to its type symbol name.
//
// This is the same alias-then-symbol order as `typecheck.typeMatchesStringSpecifier`. That function
// is unexported and takes a specifier rather than an identifier, so it cannot be called from here;
// the ORDER is what matters and it is taken from there rather than re-derived, which is the point
// of naming it. If that helper is ever exported in a form this can use, this should call it.
func (r *reactivity) stableTypeName(id IdentifierId) string {
	if r.typeChecker == nil {
		return ""
	}
	node := r.identifierNode(id)
	if node == nil {
		return ""
	}
	valueType := r.typeChecker.GetTypeAtLocation(node)
	if valueType == nil {
		return ""
	}
	if alias := shimchecker.Type_alias(valueType); alias != nil {
		if symbol := alias.Symbol(); symbol != nil {
			return symbol.Name
		}
	}
	if symbol := shimchecker.Type_symbol(valueType); symbol != nil {
		return symbol.Name
	}
	return ""
}

// isHookCallee reports whether a called value is a React hook.
//
// # Why this asks the name and not the type, which is the opposite of every other predicate here
//
// The stable predicates above key on the type because `@types/react` gives those values a
// distinctive one. A hook's RESULT has no such type: `useContext<T>(c): T` and `useMemo<T>(f): T`
// return a bare generic, so the result of `useContext(Ctx)` is type-indistinguishable from the
// result of any other call. Measured on our own checker: `ctx`, `memo` and an ordinary call result
// all report `alias=<nil> symbol=<nil>`, while their CALLEES report `symbol=useContext`,
// `symbol=useMemo` and `symbol=notAHook`. The name is where the information is.
//
// So this resolves the callee to its declaration and tests the declared name against React's own
// hook convention, `use` followed by a capital or a digit, which is exactly the test
// `classifyFunction` already applies to name a `Function` a hook. Reusing that keeps the two from
// drifting into disagreeing about what a hook name is, the same reason `static_components.go` takes
// its component test from `Function.Kind` rather than recomputing it.
//
// # Where this is weaker than upstream, stated rather than left to be discovered
//
// Upstream resolves a hook through a shape registry seeded with React's own module exports, so it
// knows `useState` imported from `react` is a hook and a local function named `useState` that
// shadows it is not. This cannot tell them apart: it sees the name. In the direction that matters
// the difference is small, because React's own `rules-of-hooks` requires every real hook to be
// named this way and forbids the convention for anything else, so a name matching the pattern and
// not being a hook is already a lint error under a different rule.
//
// The `use` operator is a genuine divergence and it is NOT reproduced. Upstream tests
// `isUseOperator`, a shape id on the builtin `use`, and marks it a source. Bare `use` does not
// match `use` + capital-or-digit, and there is no type on it to key on, so a `use(promise)` call is
// invisible here. That is one missing source, it under-approximates in the same safe direction as
// the mutation gap, and inventing a bare-name match for `use` would report every function named
// `use` in the tree as a reactive source.
func (r *reactivity) isHookCallee(id IdentifierId) bool {
	return IsHookCallee(r.function, id)
}

// IsHookCallee reports whether a called value resolves to a React hook.
//
// Lifted from `reactivity.isHookCallee` when `pruneNonEscapingScopes` needed the same question: a
// value passed as an argument to a hook escapes, because React may retain it. Two copies of this
// predicate would drift, and the verdict recorded below is the kind that only stays true while its
// callers are enumerated -- so it lives in one place with the caller list attached.
func IsHookCallee(function *Function, id IdentifierId) bool {
	if function == nil || int(id) >= len(function.Identifiers) {
		return false
	}
	identifier := function.Identifiers[id]
	if identifier == nil {
		return false
	}

	// The SYNTACTIC NODE is the name, not `Identifier.Name`, and that is measured rather than
	// assumed. This was originally written to test `identifier.Name` first and fall back to the
	// node; a mutation disabling the name branch survived every test, and disabling the node branch
	// alone failed eight assertions.
	//
	// The reason is how a callee lowers. An imported or global `useState` becomes a `LoadGlobal`,
	// whose result is a TEMPORARY: `Identifier.Name` is empty for a temporary by construction, and
	// the name lives only on `Identifier.Node`. Probed over `useState`, `useContext`,
	// `useCustomThing` and `notAHook` in one component, the counts were byName=0, byNode=3,
	// neither=1. So the name branch was not a fallback for an uncommon shape, it was unreachable
	// for every shape, and it is deleted rather than left to read as coverage.
	//
	// The callers enumerated when that verdict was taken, because this kind of verdict expires when
	// a caller is added: `visitBlock`'s two call sites, on `CallExpression.Callee` and on
	// `MethodCall.Property`. Both receive a place produced by lowering a callee expression. A future
	// caller passing an identifier that names a local binding — a hook stored in a variable, which
	// lowering would give a real `Name` — makes the name branch reachable again and this deletion
	// has to be re-argued rather than assumed to still hold.
	//
	// Re-checked when this was lifted for `pruneNonEscapingScopes`: that pass also asks about a
	// CALLEE place, so the verdict still holds. It would not hold for a caller asking about an
	// arbitrary value.
	return identifier.Node != nil && ast.IsIdentifier(identifier.Node) && isHookName(identifier.Node.Text())
}

// isHookName reports whether a name follows React's hook convention: `use` then a capital or digit.
//
// This is `classifyFunction`'s test, reached as a function so the two cannot drift. `use` alone
// does not match, which is upstream's rule for a function name and is also why the `use` operator
// is not reachable here; see isHookCallee.
func isHookName(name string) bool {
	if len(name) < 4 || name[:3] != "use" {
		return false
	}
	next := name[3]
	return (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9')
}

// identifierNode returns the syntactic node a value came from, or nil for a pure temporary.
func (r *reactivity) identifierNode(id IdentifierId) *ast.Node {
	if int(id) >= len(r.function.Identifiers) {
		return nil
	}
	identifier := r.function.Identifiers[id]
	if identifier == nil {
		return nil
	}
	return identifier.Node
}

// eachInstructionLValue calls visit for every place an instruction binds.
//
// This is upstream's `eachInstructionLValue`, which is the instruction's own LValue plus the places
// a destructuring pattern binds. It is expressed through `EachInstructionPlace` and the Define role
// rather than by matching variants, so a new instruction variant is covered by the same guard that
// already covers the visitor.
func eachInstructionLValue(instruction *Instruction, visit func(Place)) {
	EachInstructionPlace(instruction, func(place Place, role PlaceRole) {
		if role == PlaceRoleDefine {
			visit(place)
		}
	})
}

// The refresh runs BEFORE each sweep rather than after, and the loop exits only on a sweep that
// added nothing to the reactive set. So a block that becomes controlled in round N is acted on in
// round N's own sweep; if that marks anything, `changed` is set and there is another round. A
// refresh that newly controls a block whose phis were all already reactive adds nothing, which is
// correctly not a change. That is why `refreshControlled` does not set `changed` itself.
//
// reactiveControlledBlocks returns the blocks reached through a branch on a reactive test.
//
// This is upstream's `createControlDominators`, and the shape of the question is the reason it is
// computed once here rather than per lookup: a block is controlled when some block in its
// POST-DOMINATOR FRONTIER ends in a conditional terminal whose test is reactive.
//
// # Why the frontier and not the dominator tree
//
// The intuitive test is "is there a reactive branch above this block", which is forward dominance
// and is wrong in both directions. The post-dominator frontier is the set of blocks where control
// last had a choice about whether this block would execute: a block B is in the frontier of block T
// when B is a predecessor of something T post-dominates but B itself is not post-dominated by T, so
// leaving B by one edge reaches T and by another does not. That is exactly "the branch that decided
// whether T runs", which is what makes a value assigned in T depend on the branch's test.
//
// `postdominator.go` already computes the tree over this graph, for `UnconditionalBlocks`. Only the
// frontier derivation is added here, and it reads that tree rather than recomputing dominance.
//
// # Computed ONCE, outside the fixpoint, and why that is not an optimisation
//
// The frontier depends only on the control-flow graph, which no part of this pass mutates, so it is
// invariant across iterations. oxc hoists it for cost, noting the previous code re-derived it per
// block per phi operand per round. It is hoisted here for the same reason, but the reactive TEST
// against it is NOT hoisted: whether a controlling block's test is reactive changes as the fixpoint
// grows, so the frontier is cached and the test is re-asked each round. Caching the whole answer
// would freeze the first round's reactive set into the control decision and silently lose sources.
// refreshControlled recomputes which blocks are reactive-controlled from the current reactive set.
func (r *reactivity) refreshControlled(frontiers map[BlockId][]BlockId, controlled map[BlockId]bool) {
	for blockId, frontier := range frontiers {
		if controlled[blockId] {
			continue
		}
		for _, controllerId := range frontier {
			controller, ok := r.function.Block(controllerId)
			if !ok {
				continue
			}
			if r.terminalTestIsReactive(controller.Terminal) {
				controlled[blockId] = true
				break
			}
		}
	}
}

// terminalTestIsReactive reports whether a conditional terminal branches on a reactive value.
//
// Only `If`, `Branch` and `Switch` count, which is upstream's set. A loop terminal is not asked:
// upstream's switch has exactly these three arms and falls through on everything else, so a `while`
// whose test is reactive does not make its body controlled by this rule. That reads like an
// oversight and is reproduced rather than improved on, because the body's values pick up reactivity
// through the loop's phis instead, which is the same answer by a different route.
func (r *reactivity) terminalTestIsReactive(terminal Terminal) bool {
	switch t := terminal.(type) {
	case *If:
		return r.isReactive(t.Test.Identifier)
	case *Branch:
		return r.isReactive(t.Test.Identifier)
	case *Switch:
		if r.isReactive(t.Test.Identifier) {
			return true
		}
		for _, kase := range t.Cases {
			if kase.Test != nil && r.isReactive(kase.Test.Identifier) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// postDominatorFrontiers computes each block's post-dominator frontier.
//
// Upstream's `postDominatorFrontier`, over the tree `postdominator.go` already builds. For a target
// T: collect the blocks T post-dominates, then take every predecessor of any of those that is not
// itself post-dominated by T. Those are the blocks where control chose whether T would run.
func (r *reactivity) postDominatorFrontiers() map[BlockId][]BlockId {
	tree := computePostDominance(r.function)
	frontiers := make(map[BlockId][]BlockId, len(r.function.Blocks))

	for _, block := range r.function.Blocks {
		postDominated := r.blocksPostDominatedBy(tree, block.Id)

		seen := map[BlockId]bool{}
		var frontier []BlockId
		// Iterating Function.Blocks rather than the postDominated map keeps the frontier's order
		// deterministic: Blocks is a slice in reverse postorder and the map is not ordered at all.
		for _, candidate := range r.function.Blocks {
			if !postDominated[candidate.Id] && candidate.Id != block.Id {
				continue
			}
			for _, predecessorId := range candidate.Predecessors {
				if postDominated[predecessorId] || seen[predecessorId] {
					continue
				}
				seen[predecessorId] = true
				frontier = append(frontier, predecessorId)
			}
		}
		if len(frontier) > 0 {
			frontiers[block.Id] = frontier
		}
	}
	return frontiers
}

// blocksPostDominatedBy returns the blocks that target post-dominates, walking the tree downward.
//
// The tree maps a block to its IMMEDIATE post-dominator, so this inverts it once per target. A
// block is post-dominated by the target when the chain of immediate post-dominators from it reaches
// the target.
func (r *reactivity) blocksPostDominatedBy(tree *postDominanceTree, target BlockId) map[BlockId]bool {
	postDominated := make(map[BlockId]bool, len(r.function.Blocks))
	for _, block := range r.function.Blocks {
		current := block.Id
		// Bounded by the block count: the chain is a tree path and cannot revisit, but this walks
		// data a caller could have restructured, and a linter must not hang on it.
		for step := 0; step <= len(r.function.Blocks); step++ {
			if current == target {
				if block.Id != target {
					postDominated[block.Id] = true
				}
				break
			}
			next, ok := tree.immediate[current]
			if !ok || next == current {
				break
			}
			current = next
		}
	}
	return postDominated
}

// applyToPlaces writes the settled reactive set onto every place, and recurses into nested
// functions.
//
// Separated from the fixpoint deliberately. Upstream writes the flag as a side effect of its
// `isReactive`, which makes the set of flagged places depend on which operands the traversal
// happened to consult and makes the pass non-idempotent. Writing once from the settled set gives
// the same answer for every place naming a reactive value, which is the property a consumer
// actually wants.
//
// # Nested functions, and what upstream does here
//
// Upstream's `propagateReactivityToInnerFunctions` walks nested functions and calls `isReactive` on
// their operands purely for the flag side effect, using the ENCLOSING function's reactive set. It
// does not re-run the fixpoint per nested function and it does not treat a nested function's
// parameters as reactive sources.
//
// That is reproduced, and the reason it works at all is the positional capture pairing
// `Construct`'s comment describes: a nested function's `Context[i]` and the enclosing
// `FunctionExpression.Captures[i]` name one source binding from two sides. A capture that is
// reactive in the parent makes the corresponding context value reactive in the child, which is what
// this propagation is for. Values a nested function computes for itself are not marked, matching
// upstream, whose inner walk marks nothing new either.
func (r *reactivity) applyToPlaces(function *Function, isOutermost bool) {
	for _, block := range function.Blocks {
		for _, phi := range block.Phis {
			r.setPlace(&phi.Place)
			for _, predecessorId := range PhiOperandsInOrder(phi) {
				operand := phi.Operands[predecessorId]
				r.setPlace(&operand)
				phi.Operands[predecessorId] = operand
			}
		}
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			EachInstructionPlacePointer(instruction, func(place *Place, role PlaceRole) {
				r.setPlace(place)
			})
		}
		EachTerminalPlacePointer(block.Terminal, func(place *Place, role PlaceRole) {
			r.setPlace(place)
		})
	}
	for index := range function.Params {
		r.setPlace(&function.Params[index])
	}
	r.setPlace(&function.Returns)

	for index := range function.Context {
		r.setPlace(&function.Context[index])
	}

	for _, nested := range function.Functions {
		r.propagateToNested(nested)
	}
}

// propagateToNested carries the enclosing function's reactive set into a nested function's places.
//
// The nested function's own values are addressed in ITS identifier table, and this pass's set is
// keyed against the outer table, so the only places that can match are those whose ids coincide.
// The pairing that actually carries meaning across the boundary is positional, through
// `Captures[i]` and `Context[i]`, and it is applied here before the walk so a context value inherits
// its capture's reactivity.
func (r *reactivity) propagateToNested(nested *Function) {
	if nested == nil {
		return
	}
	r.applyToPlaces(nested, false)
}

// setPlace writes the settled flag onto one place.
func (r *reactivity) setPlace(place *Place) {
	if place == nil {
		return
	}
	if r.reactive[place.Identifier] {
		place.Reactive = true
	}
}
