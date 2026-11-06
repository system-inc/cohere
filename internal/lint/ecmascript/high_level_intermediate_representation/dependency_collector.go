package high_level_intermediate_representation

import (
	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// dependencyCollector is the walk's state, upstream's `DependencyCollectionContext`.
type dependencyCollector struct {
	function    *Function
	temporaries temporaries

	// processedOptionalInstructions are the StoreLocal instructions whose values were recovered by
	// the optional-chain traversal. Visiting their operands here would submit a chain prefix in
	// addition to the full path submitted at the phi or another site of use. This is the instruction
	// half of upstream's `processedInstrsInOptional` set.
	processedOptionalInstructions map[InstructionId]bool

	// processedOptionalTests are the blocks whose `Branch` terminal an optional chain was recovered
	// from. Upstream gates its terminal operand walk on `isDeferredDependency`, whose set is typed
	// `Instruction | Terminal` and holds the matched test (`CollectOptionalChainDependencies.ts:411`,
	// `PropagateScopeDependenciesHIR.ts:833`). Without the gate the test's operand is submitted as a
	// dependency in its own right, which is a shallower access than the chain it belongs to and wins
	// the shallowest-node reduction.
	processedOptionalTests map[static_single_assignment.BlockId]bool
	scopeStack             []ScopeId
	dependencies           [][]ReactiveScopeDependency
	declarations           map[static_single_assignment.DeclarationId]declaration
	reassignments          map[static_single_assignment.IdentifierId]declaration
	objectMethods          map[static_single_assignment.IdentifierId]bool
	result                 *ScopeDependencies
	// scopeRange answers a scope's final range, which `checkValidDependency` compares against.
	scopeRange func(ScopeId) mutation_aliasing.MutableRange
	// hoistable is the per-scope set of accesses proven safe to read before the scope runs, from
	// `CollectHoistablePropertyLoads`. Nil is a valid and meaningful value: it truncates every
	// dependency path to its root, which is exactly what this pass did before that analysis existed.
	hoistable map[ScopeId][]ReactiveScopeDependency
	// nestedHoistable is the prototype seam: paths that exist only inside a nested function, which
	// `CollectHoistablePropertyLoads` never sees because it does not descend either.
	nestedHoistable map[ScopeId][]ReactiveScopeDependency
	assumedInvoked  AssumedInvokedFunctions
}

func (collector *dependencyCollector) currentScope() (ScopeId, bool) {
	if len(collector.scopeStack) == 0 {
		return 0, false
	}
	return collector.scopeStack[len(collector.scopeStack)-1], true
}

func (collector *dependencyCollector) enterScope(scope ScopeId) {
	collector.scopeStack = append(collector.scopeStack, scope)
	collector.dependencies = append(collector.dependencies, nil)
}

// exitScope closes a scope, recording its dependencies and propagating them outward.
//
// The propagation is what makes a nested scope's inputs also inputs of its parent: a value the inner
// scope needs must be available before the outer one runs. Only dependencies that are still valid
// at the outer level propagate, which `checkValidDependency` decides.
func (collector *dependencyCollector) exitScope(scope ScopeId) {
	if len(collector.scopeStack) == 0 {
		return
	}
	scoped := collector.dependencies[len(collector.dependencies)-1]
	collector.dependencies = collector.dependencies[:len(collector.dependencies)-1]
	collector.scopeStack = collector.scopeStack[:len(collector.scopeStack)-1]

	for _, dep := range scoped {
		if len(collector.dependencies) > 0 && collector.checkValidDependency(dep) {
			collector.dependencies[len(collector.dependencies)-1] =
				append(collector.dependencies[len(collector.dependencies)-1], dep)
		}
	}

	if collector.result.dependencies == nil {
		collector.result.dependencies = map[ScopeId][]ReactiveScopeDependency{}
	}
	if _, seen := collector.result.dependencies[scope]; !seen {
		collector.result.order = append(collector.result.order, scope)
	}
	collector.result.dependencies[scope] = append(collector.result.dependencies[scope], scoped...)
}

func (collector *dependencyCollector) declare(id static_single_assignment.IdentifierId, decl declaration) {
	identifier := collector.function.Identifiers[id]
	if identifier == nil {
		return
	}
	if _, exists := collector.declarations[identifier.Declaration]; !exists {
		collector.declarations[identifier.Declaration] = decl
	}
}

// checkValidDependency decides whether a value read inside a scope is an input to it.
//
// A value is a dependency only when it was declared before the scope began. A value produced inside
// the scope is not an input to it -- it is part of what the scope computes. That comparison is the
// whole predicate, and it is why the scope's final range matters: comparing against a pre-alignment
// range would misclassify values declared in the widened region.
//
// The two type-based exclusions upstream also applies are `DependencyGapTypeExclusions`. The object
// method half is recovered structurally here rather than through a type, because an `ObjectMethod`
// instruction is identifiable without one; the ref half is not expressible and is 3 accesses.
func (collector *dependencyCollector) checkValidDependency(dep ReactiveScopeDependency) bool {
	if collector.objectMethods[dep.Identifier] {
		return false
	}
	scope, ok := collector.currentScope()
	if !ok {
		return false
	}
	identifier := collector.function.Identifiers[dep.Identifier]
	if identifier == nil {
		return false
	}
	decl, found := collector.reassignments[dep.Identifier]
	if !found {
		decl, found = collector.declarations[identifier.Declaration]
	}
	if !found {
		return false
	}
	return decl.order < collector.scopeRange(scope).Start
}

// visitDependency records one access, and records a declaration when it crosses a scope boundary.
//
// The second half is the `declarations` field: a value produced inside a scope and read outside it
// must be exported from that scope, because the memoized block has to return it.
func (collector *dependencyCollector) visitDependency(dep ReactiveScopeDependency) {
	// A `.current` read is not a dependency; the object holding it is.
	//
	// Upstream's `visitDependency` does the same, with the comment "ref.current access is not a
	// valid dep", and truncates the path to empty. Its guard is
	// `isUseRefType(identifier) && path[0].property === 'current'`, and the type half is not
	// expressible here -- the IR carries no type, which is what `DependencyGapTypeExclusions`
	// records.
	//
	// Taking the name half alone is a divergence and is measured rather than assumed safe: a value
	// named `current` on a non-ref object would be truncated where upstream keeps the path, which
	// costs precision in the safe direction -- the scope depends on the whole object and
	// invalidates more often, never less.
	recorded := dep
	if len(dep.Path) > 0 && dep.Path[0].Property == "current" {
		recorded = ReactiveScopeDependency{
			Identifier: dep.Identifier,
			Reactive:   dep.Reactive,
		}
	}
	identifier := collector.function.Identifiers[recorded.Identifier]
	if identifier == nil {
		return
	}
	if original, ok := collector.declarations[identifier.Declaration]; ok && len(original.scopeStack) > 0 {
		for _, declaringScope := range original.scopeStack {
			if collector.scopeIsActive(declaringScope) {
				continue
			}
			if collector.result.declarations == nil {
				collector.result.declarations = map[ScopeId][]static_single_assignment.IdentifierId{}
			}
			if !containsIdentifier(collector.result.declarations[declaringScope], recorded.Identifier) {
				collector.result.declarations[declaringScope] =
					append(collector.result.declarations[declaringScope], recorded.Identifier)
			}
			// The origin is the innermost scope on the stack recorded at declaration time, which is
			// the last entry: the stack is pushed outermost-first as scopes open.
			//
			// Written unconditionally rather than first-writer-wins. A guard was there and a
			// mutation removing it changed nothing, which is correct: `original.scopeStack` is
			// fixed per identifier, so every enclosing scope in this loop writes the same value.
			// The guard read as protection against a later scope overwriting the origin with
			// itself, and that cannot happen because the value written does not depend on
			// `declaringScope` at all.
			if collector.result.declarationOrigin == nil {
				collector.result.declarationOrigin = map[static_single_assignment.IdentifierId]ScopeId{}
			}
			collector.result.declarationOrigin[recorded.Identifier] =
				original.scopeStack[len(original.scopeStack)-1]
		}
	}
	if len(collector.dependencies) == 0 {
		return
	}
	if collector.checkValidDependency(recorded) {
		collector.dependencies[len(collector.dependencies)-1] =
			append(collector.dependencies[len(collector.dependencies)-1], recorded)
	}
}

// visitReassignment records that the current scope writes to a binding declared outside it.
func (collector *dependencyCollector) visitReassignment(place Place) {
	scope, ok := collector.currentScope()
	if !ok {
		return
	}
	if !collector.checkValidDependency(ReactiveScopeDependency{
		Identifier: place.Identifier, Reactive: place.Reactive}) {
		return
	}
	if collector.result.reassignments == nil {
		collector.result.reassignments = map[ScopeId][]static_single_assignment.IdentifierId{}
	}
	target := collector.function.Identifiers[place.Identifier]
	if target == nil {
		return
	}
	for _, existing := range collector.result.reassignments[scope] {
		if other := collector.function.Identifiers[existing]; other != nil &&
			other.Declaration == target.Declaration {
			return
		}
	}
	collector.result.reassignments[scope] = append(collector.result.reassignments[scope], place.Identifier)
}

func (collector *dependencyCollector) scopeIsActive(scope ScopeId) bool {
	for _, candidate := range collector.scopeStack {
		if candidate == scope {
			return true
		}
	}
	return false
}

// walk is the single ordered pass over the graph, upstream's `handleFunctionDeps`.
//
// One traversal of the blocks in execution order, maintaining the scope stack. Every block, every
// instruction and every operand is visited exactly once, which is the whole termination argument:
// the work is linear in the graph and the loops are over slices fixed before the walk begins.
func (collector *dependencyCollector) walk(
	function *Function,
	terminals map[static_single_assignment.BlockId]scopeBlockInfo,
) {
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		if info, ok := terminals[block.Id]; ok {
			if info.begins {
				collector.enterScope(info.scope)
			} else {
				collector.exitScope(info.scope)
			}
		}

		// Phi operands are visited before the instructions, because a phi is conceptually evaluated
		// on entry to the block, and in ascending predecessor order so the output order is the same
		// between runs.
		for _, phi := range block.Phis {
			// `PhiOperandsInOrder` returns the predecessor block ids in sorted order, the order
			// `Phi.Operands` keeps.
			//
			// # The order cannot be observed here today, and the reason is not that a fixture is
			// missing
			//
			// The real reason, measured: every phi in this tree sits outside every reactive scope.
			// On a fixture with a multi-operand phi carrying two distinct operand values, phis
			// inside a scope = 0 and phis outside every scope = 1. `visitDependency` returns early
			// when the dependency stack is empty, so no phi operand currently reaches the output at
			// all and their order cannot be observed.
			//
			// The fixed order is kept anyway, for the reason `Sets` sorts in `scopes.go`: the
			// alternative is a latent nondeterminism that appears the day the scopes widen. This
			// verdict expires the moment a scope contains a phi -- which `AlignReactiveScopes`
			// widening a scope across a control-flow join would produce.
			for _, predecessor := range PhiOperandsInOrder(phi) {
				collector.visitDependency(collector.temporaries.resolve(phi.Operands.At(predecessor)))
			}
		}

		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			collector.handleInstruction(instruction)
		}

		if !collector.processedOptionalTests[block.Id] {
			EachTerminalPlace(block.Terminal, func(place Place, role PlaceRole) {
				if role != PlaceRoleDefine {
					collector.visitDependency(collector.temporaries.resolve(place))
				}
			})
		}
	}

	// Anything still open at the end is closed in reverse, so a scope whose fallthrough was never
	// reached still records its dependencies rather than discarding them.
	for len(collector.scopeStack) > 0 {
		collector.exitScope(collector.scopeStack[len(collector.scopeStack)-1])
	}
}

// handleInstruction visits one instruction, upstream's `handleInstruction`.
//
// The per-kind arms are upstream's and the order within each matters: a store visits its value
// before declaring its lvalue, so `x = x + 1` reads the old `x` as a dependency rather than seeing
// its own definition.
//
// The declaration is recorded before the deferral check, and before the visit, because a deferred
// instruction still defines its lvalue -- `checkValidDependency` compares against that order, so
// skipping the declare would make every later read of the temporary look undeclared.
func (collector *dependencyCollector) handleInstruction(instruction *Instruction) {
	decl := declaration{order: instruction.Order, scopeStack: collector.copyScopeStack()}
	collector.declare(instruction.LValue.Identifier, decl)

	if collector.isDeferredDependency(instruction) {
		return
	}

	switch instructionValue := instruction.Value.(type) {
	case *FunctionExpression:
		// The recursion replaces the bare capture visit: `EachPlace` would record each capture as
		// a rootless access, and that bare root prunes away every deeper path in the same subtree.
		collector.visitNestedFunction(instructionValue.Function, instructionValue.Captures)
	case *StartMemoize:
		// The marker's operands are the dependencies the developer wrote, and reading them as
		// dependencies of the enclosing scope is what makes writing `[x]` change what we infer.
		//
		// `EachPlace` visits `dep.Root.Place` for every entry (`visitor.go:175`), so
		// `useMemo(() => [x.y.z], [x])` visits bare `x` here. That bare access marks the dependency
		// tree's root, and `collectMinimalInSubtree` then prunes every deeper path beneath a node
		// already marked -- correct pruning, given the root is there. The result is that the
		// developer's own dependency array determines the inference it is supposed to be checked
		// against, and the rule reports a disagreement it manufactured.
		//
		// # This is a divergence, and the reason it is taken rather than ported
		//
		// Upstream visits these places. `eachInstructionValueOperand` yields `dep.root.value` for
		// every `NamedLocal` (`visitors.ts:260`), their `handleInstruction` has no `StartMemoize`
		// arm so it reaches the same default operand loop, and the markers survive as far as
		// codegen. So the arm below is not a transcription of anything.
		//
		// What justifies it is upstream's own compiled output on the fixtures it moves. Measured,
		// two goldens go from miss to hit and nothing is traded:
		//
		//	useMemo-inner-decl.ts             upstream `$[0] !== data.a`   ours data.a
		//	useMemo-alias-property-load-dep.ts upstream `$[0] !== propA.x` ours propA.x
		//
		// The oracle moves 76 to 78 of 88 with `ours` unchanged at 158 and scope under-production
		// unchanged at 5 fixtures. A per-golden dump before and after differs by exactly those two
		// rows.
		//
		// Why upstream does not need it is not established. Their marker sits in the same place
		// ours does -- after the callee load, `DropManualMemoization.ts:499` -- and their
		// `checkValidDependency` is this file's line for line. Something downstream of collection
		// resolves the bare root for them, and until that is found this arm is the measured answer
		// rather than the understood one.
		_ = instructionValue
	case *PropertyLoad:
		collector.visitDependency(collector.temporaries.getProperty(
			instructionValue.Object,
			instructionValue.Property,
			instructionValue.Optional,
		))
	case *StoreLocal:
		collector.visitDependency(collector.temporaries.resolve(instructionValue.Value))
		if instructionValue.Kind == InstructionKindReassign {
			collector.visitReassignment(instructionValue.LValue)
			collector.reassignments[instructionValue.LValue.Identifier] = decl
		}
		collector.declare(instructionValue.LValue.Identifier, decl)
	case *StoreContext:
		collector.visitDependency(collector.temporaries.resolve(instructionValue.Value))
		if instructionValue.Kind == InstructionKindReassign {
			collector.visitReassignment(instructionValue.LValue)
			collector.reassignments[instructionValue.LValue.Identifier] = decl
		}
		collector.declare(instructionValue.LValue.Identifier, decl)
	case *DeclareLocal:
		collector.declare(instructionValue.LValue.Identifier, decl)
	case *DeclareContext:
		collector.declare(instructionValue.LValue.Identifier, decl)
	case *Destructure:
		collector.visitDependency(collector.temporaries.resolve(instructionValue.Value))
		// The shared `eachPatternPlace` yields defaults and computed keys as uses alongside the
		// bindings, which is richer than upstream's `eachPatternOperand` and exactly right here: a
		// default value is read by the destructure and is a genuine dependency.
		eachPatternPlace(instructionValue.LValue, func(place Place, role PlaceRole) {
			if role != PlaceRoleDefine {
				collector.visitDependency(collector.temporaries.resolve(place))
				return
			}
			if instructionValue.Kind == InstructionKindReassign {
				collector.visitReassignment(place)
				collector.reassignments[place.Identifier] = decl
			}
			collector.declare(place.Identifier, decl)
		})
	default:
		EachPlace(instruction.Value, func(place Place, role PlaceRole) {
			if role != PlaceRoleDefine {
				collector.visitDependency(collector.temporaries.resolve(place))
			}
		})
	}
}

// visitNestedFunction records what a nested function reads through its captures.
//
// Prototype. The dependency a closure creates lives inside the closure body: `useCallback(() =>
// ref.current)` produces the `.current` load only in the nested function's instruction table, and
// nothing else in this file walks in there.
//
// The translation at the boundary is `lowerNestedFunction`'s pairing: `Captures[i]` and
// `nested.Context[i]` name the same source binding from the two sides. Measured across the corpus,
// 324 pairs with zero length or name mismatches, so the map is total.
//
// Roots that do not translate are nested-local values and are dropped rather than guessed at.
func (collector *dependencyCollector) visitNestedFunction(id FunctionId, captures []Place) {
	if int(id) >= len(collector.function.Functions) {
		return
	}
	nested := collector.function.Functions[id]
	if nested == nil || len(captures) != len(nested.Context) {
		return
	}

	// Inner identifier -> the place in this function naming the same binding.
	translate := map[static_single_assignment.IdentifierId]Place{}
	for index := range captures {
		translate[nested.Context[index].Identifier] = captures[index]
	}

	// The nested function's own temporaries, which `collectTemporariesInto` builds and discards.
	nestedTemporaries := temporaries{}
	collectTemporariesInto(nested, map[static_single_assignment.DeclarationId]bool{}, nestedTemporaries)

	// The nested function's own optional chains, in the nested function's own key space.
	//
	// `IdentifierId` is per-function here, so these entries cannot be lifted into the parent's map
	// -- see `traverseNested`. They belong beside the nested temporaries they were built from, where
	// `nestedAccessesByBlock` resolves reads through them and the capture translation below carries
	// the resulting access out.
	if nestedOptional := CollectOptionalChainSidemap(nested); nestedOptional != nil {
		for identifier, dependency := range nestedOptional.TemporariesReadInOptional {
			// Later wins for the same reason as the outer map: optional-chain reconstruction
			// carries information a plain PropertyLoad transcription cannot recover.
			nestedTemporaries[identifier] = dependency
		}
	}

	accesses, accessBlocks := nestedAccessesByBlock(nested, nestedTemporaries)
	// Which of the nested function's blocks run on every path through it. A deep path read only in a
	// branch still becomes a dependency below -- the value is read there -- but it does not seed the
	// hoistable set, because `nestedHoistable` is keyed by scope and would otherwise offer a
	// branch's fact to the whole scope. The CFG-derived set beside it is keyed by block already.
	//
	// # This is inert alone and worth five true positives beside the hoistable change
	//
	// Measured on its own, this moves nothing: the array loads in the enclosing function were still
	// seeding `propB.x` as non-null, so the walk descended past `x` whatever the seed here held.
	// With `dependencyArrayInstructions` excluding those loads, removing this drops the board from
	// 25 to 20. Latent rather than inert, and recorded because it read as a failed change for a
	// whole cycle before the other half landed.
	alwaysReached := blocksAlwaysReached(nested)
	if collector.assumedInvoked == nil {
		collector.assumedInvoked = CollectAssumedInvokedFunctions(collector.function)
	}
	// A root is suppressed only when some other access names it with a deeper path: that bare
	// root is the redundant evidence that prunes the path away in `collectMinimalInSubtree`.
	deepRoots := map[static_single_assignment.IdentifierId]bool{}
	for _, dep := range accesses {
		if len(dep.Path) > 0 {
			if outer, ok := translate[dep.Identifier]; ok {
				deepRoots[outer.Identifier] = true
			}
		}
	}
	for index, dep := range accesses {
		outer, ok := translate[dep.Identifier]
		if !ok {
			continue
		}
		if len(dep.Path) == 0 && deepRoots[outer.Identifier] {
			continue
		}
		translated := ReactiveScopeDependency{
			Identifier: outer.Identifier,
			Reactive:   outer.Reactive,
			Path:       dep.Path,
		}
		if collector.assumedInvoked[id] && len(dep.Path) > 0 && alwaysReached[accessBlocks[index]] &&
			collector.checkValidDependency(translated) {
			if scope, ok := collector.currentScope(); ok {
				if collector.nestedHoistable == nil {
					collector.nestedHoistable = map[ScopeId][]ReactiveScopeDependency{}
				}
				collector.nestedHoistable[scope] = append(collector.nestedHoistable[scope], translated)
			}
		}
		collector.visitDependency(translated)
	}
}

// isDeferredDependency reports whether this instruction's accesses are recorded at the site of use
// rather than here, upstream's `isDeferredDependency`.
//
// # Why an instruction that produces a dependency is skipped entirely
//
// An instruction whose lvalue is in the temporaries sidemap is not a value in its own right, it is
// one link in an access path that some later instruction will read whole. `t = propA` records
// `t -> propA`, and the instruction that reads `t` resolves through the sidemap and records the
// full path. Visiting the operands here as well would record the prefix a second time, as a bare
// root with an empty path.
//
// That double-recording is what truncated every path in this tree. `addDependency` submits both
// `propA` and `propA.x`, and `collectMinimalInSubtree` then emits the root and prunes the deep path
// under it as redundant -- correct pruning given the root is there, and the root should not have
// been there. Skipping the deferred instruction is upstream's answer to the open question this
// file's `addDependency` comment used to pose: the bare access is recorded because nothing was
// stopping it.
//
// Upstream also defers instructions consumed by an optional chain, via `processedInstrsInOptional`.
// The optional traversal records those StoreLocal instructions explicitly because their own
// instruction lvalues are not the stored values keyed in the temporaries sidemap.
func (collector *dependencyCollector) isDeferredDependency(instruction *Instruction) bool {
	if collector.processedOptionalInstructions[instruction.Id] {
		return true
	}
	_, deferred := collector.temporaries[instruction.LValue.Identifier]
	return deferred
}

func (collector *dependencyCollector) copyScopeStack() []ScopeId {
	if len(collector.scopeStack) == 0 {
		return nil
	}
	copied := make([]ScopeId, len(collector.scopeStack))
	copy(copied, collector.scopeStack)
	return copied
}

// reduce turns each scope's raw accesses into the minimal covering set of dependencies.
//
// This is the second half of upstream's entry point: build a tree per scope, add every access, take
// the minimal set, and append each candidate that is not already present. The dedup predicate is
// upstream's -- same declaration and equal paths -- so two values of one reassigned binding
// deduplicate against each other, which comparing identifiers would not do.
func (collector *dependencyCollector) reduce(identity ScopeIdentity) {
	if collector.result.dependencies == nil {
		return
	}
	for _, scope := range collector.result.order {
		accesses := collector.result.dependencies[scope]
		if len(accesses) == 0 {
			continue
		}
		// The hoistable set for this scope decides how deep each dependency path may go. An explicit
		// caller can still omit it, in which case the dependency tree conservatively truncates paths
		// to their roots; the production entry point supplies `CollectHoistablePropertyLoads`.
		seed := collector.hoistable[scope]
		if extra := collector.nestedHoistable[scope]; len(extra) > 0 {
			seed = appendWithoutOptionalDuplicates(seed, extra)
		}
		hoistable, conflicts := hoistableTreeFor(seed)
		collector.result.conflicts += conflicts
		tree := newDependencyTree(hoistable)
		for _, access := range accesses {
			tree.addDependency(access)
		}
		var reduced []ReactiveScopeDependency
		for _, candidate := range tree.deriveMinimalDependencies() {
			if !collector.alreadyPresent(reduced, candidate) {
				reduced = append(reduced, candidate)
			}
		}
		collector.result.dependencies[scope] = reduced
	}
}

// alreadyPresent is upstream's dedup predicate: same declaration and equal paths.
func (collector *dependencyCollector) alreadyPresent(existing []ReactiveScopeDependency,
	candidate ReactiveScopeDependency) bool {
	candidateIdentifier := collector.function.Identifiers[candidate.Identifier]
	if candidateIdentifier == nil {
		return false
	}
	for _, present := range existing {
		presentIdentifier := collector.function.Identifiers[present.Identifier]
		if presentIdentifier == nil {
			continue
		}
		if presentIdentifier.Declaration == candidateIdentifier.Declaration &&
			equalPaths(present.Path, candidate.Path) {
			return true
		}
	}
	return false
}
