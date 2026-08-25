// Checking that a developer's own memoization survived compilation.
//
// This is React's `validatePreservedManualMemoization`
// (`Validation/ValidatePreservedManualMemoization.ts` at `reactconformance.UpstreamSha`), and it is
// the rule the whole of phase 6 exists to enable.
//
// # What it reports
//
// A developer who wrote `useMemo` or `useCallback` made a claim: this value is stable across renders
// unless these dependencies change. The compiler is free to rewrite that, and normally does it
// better -- but if the rewrite ends up not memoizing the value, the developer's guarantee silently
// disappears. Code that relied on referential stability then breaks in a way nothing else catches.
//
// So the rule compares what was written against what survived, and reports where they disagree.
//
// # Two firing conditions, both about scopes rather than about dependencies
//
// At `StartMemoize`, an operand belonging to a scope that neither survived nor was pruned means the
// dependency may be mutated after the memo block, so the memoization cannot be trusted.
//
// At `FinishMemoize`, a memoized value belonging to a scope that did not survive means the value was
// memoized in source and is not in output. That is the headline case.
//
// Both read `identifier.scope`. Upstream keeps that on the identifier; this tree keeps it in the
// `ReactiveScopes` side table, which answers the same question -- 15,715 of 61,738 corpus
// identifiers carry one.
package hir

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
)

// PreserveManualMemoizationFinding is one disagreement between source and output.
type PreserveManualMemoizationFinding struct {
	// Identifier is the value whose memoization was lost.
	Identifier IdentifierId
	// Scope is the scope it belonged to, which did not survive.
	Scope ScopeId
	// Order is where in the function the finding sits, for reporting.
	Order EvaluationOrder
	// Kind separates the two conditions, which carry different messages upstream.
	Kind PreserveManualMemoizationKind
}

// PreserveManualMemoizationKind is which of the two conditions fired.
type PreserveManualMemoizationKind uint8

const (
	// PreserveManualMemoizationDependencyMutable is the `StartMemoize` condition: a dependency
	// belonging to a scope that neither survived nor was pruned, so it may be modified later.
	PreserveManualMemoizationDependencyMutable PreserveManualMemoizationKind = iota
	// PreserveManualMemoizationValueUnmemoized is the `FinishMemoize` condition: a value memoized in
	// source and not in the compilation output.
	PreserveManualMemoizationValueUnmemoized
)

func (kind PreserveManualMemoizationKind) String() string {
	switch kind {
	case PreserveManualMemoizationDependencyMutable:
		return "this dependency may be modified later"
	case PreserveManualMemoizationValueUnmemoized:
		return "could not preserve existing memoization"
	default:
		return "<unknown preserve-manual-memoization kind>"
	}
}

// ValidatePreservedManualMemoization reports where a developer's memoization did not survive.
//
// The scopes that survived are collected by the same walk that checks, which is upstream's shape and
// is load-bearing: a scope is only known to have survived once the walk reaches it, so a memo block
// is checked against the scopes closed before it rather than against every scope in the function.
func ValidatePreservedManualMemoization(tree *ReactiveFunction, function *Function,
	scopes *ReactiveScopes) []PreserveManualMemoizationFinding {
	if tree == nil || function == nil {
		return nil
	}
	validator := manualMemoValidator{
		function:       function,
		scopes:         scopes,
		liveScopes:     map[ScopeId]bool{},
		prunedScopes:   map[ScopeId]bool{},
		openMemoBlocks: map[int]bool{},
	}
	validator.walk(tree.Body)
	return validator.findings
}

type manualMemoValidator struct {
	function *Function
	scopes   *ReactiveScopes
	// liveScopes are scopes the walk has passed that survived, plus everything they absorbed.
	liveScopes map[ScopeId]bool
	// prunedScopes are scopes the walk has passed that were pruned.
	prunedScopes map[ScopeId]bool
	// openMemoBlocks are the `ManualMemoId`s of memo blocks opened and not yet closed.
	//
	// A set keyed by id rather than a boolean, because `ManualMemoId` exists precisely to make
	// pairing possible: memo calls nest -- a `useMemo` whose callback body holds another -- and once
	// the callbacks are inlined the markers do not form a simple stack in instruction order. A
	// boolean would let an inner `FinishMemoize` close an outer block, and the outer block's own
	// finish would then be dropped as unopened.
	openMemoBlocks map[int]bool
	findings       []PreserveManualMemoizationFinding
}

func (v *manualMemoValidator) walk(block ReactiveBlock) {
	for _, statement := range block {
		switch shape := statement.(type) {
		case *ReactiveInstructionStatement:
			v.visitInstruction(shape.Instruction)

		case *ReactiveScopeBlock:
			v.walk(shape.Instructions)
			// Recorded after the body, matching upstream: a scope is known to have survived only
			// once the walk has left it, so a memo block inside it is checked against the scopes
			// that closed before it rather than against its own enclosing scope.
			if shape.Pruned {
				v.prunedScopes[shape.Scope] = true
				continue
			}
			v.liveScopes[shape.Scope] = true
			for _, absorbed := range shape.Merged {
				// A scope absorbed by a merge survived under its survivor's identity, so the ids it
				// carried are live too. This is the only consumer of `Merged`, and the reason the
				// merge pass was owed.
				v.liveScopes[absorbed] = true
			}

		case *ReactiveTerminalStatement:
			v.walkTerminal(shape)
		}
	}
}

func (v *manualMemoValidator) visitInstruction(instruction *ReactiveInstruction) {
	if instruction == nil || instruction.Value == nil {
		return
	}
	// A sequence carries instructions inside a value; a memo marker can sit in one.
	if sequence, isSequence := instruction.Value.(*ReactiveSequenceValue); isSequence {
		for _, nested := range sequence.Instructions {
			v.visitInstruction(nested)
		}
	}
	plain, isPlain := instruction.Value.(*ReactiveInstructionValue)
	if !isPlain || plain.Value == nil {
		return
	}

	switch marker := plain.Value.(type) {
	case *StartMemoize:
		v.openMemoBlocks[marker.ManualMemoId] = true
		// A dependency belonging to a scope that neither survived nor was pruned may be mutated
		// after this point, so the memoization cannot be trusted.
		for _, dependency := range marker.Deps {
			if dependency.Root.IsGlobal {
				continue
			}
			v.check(dependency.Root.Place.Identifier, instruction.Order,
				PreserveManualMemoizationDependencyMutable)
		}

	case *FinishMemoize:
		if !v.openMemoBlocks[marker.ManualMemoId] {
			// Upstream returns early: a StartMemoize with invalid deps records no state, so its
			// FinishMemoize has nothing to close and validating it would report against a block
			// that was never opened.
			return
		}
		delete(v.openMemoBlocks, marker.ManualMemoId)
		if marker.Pruned {
			// A pruned memo block was deliberately discarded, so there is nothing to preserve.
			return
		}
		v.check(marker.Value.Identifier, instruction.Order,
			PreserveManualMemoizationValueUnmemoized)
	}
}

// check reports the identifier if it belongs to a scope that did not survive.
//
// Upstream's `isUnmemoized`: an identifier with no scope is fine -- that is its proxy for a
// primitive or global, which needs no memoization -- and one whose scope survived is fine. Only a
// scoped identifier whose scope is absent from the live set is a lost memoization.
//
// The `StartMemoize` condition additionally accepts a pruned scope, because a pruned scope was a
// deliberate decision rather than a lost one. The `FinishMemoize` condition does not.
func (v *manualMemoValidator) check(identifier IdentifierId, order EvaluationOrder,
	kind PreserveManualMemoizationKind) {
	if v.scopes == nil {
		return
	}
	scope := v.scopes.ScopeOf(identifier)
	if scope == 0 {
		return
	}
	if v.liveScopes[scope] {
		return
	}
	if kind == PreserveManualMemoizationDependencyMutable && v.prunedScopes[scope] {
		return
	}
	v.findings = append(v.findings, PreserveManualMemoizationFinding{
		Identifier: identifier,
		Scope:      scope,
		Order:      order,
		Kind:       kind,
	})
}

// walkTerminal descends into every block a terminal contains.
func (v *manualMemoValidator) walkTerminal(statement *ReactiveTerminalStatement) {
	switch shape := statement.Terminal.(type) {
	case *ReactiveIf:
		v.walk(shape.Consequent)
		if shape.Alternate != nil {
			v.walk(*shape.Alternate)
		}
	case *ReactiveSwitch:
		for index := range shape.Cases {
			if shape.Cases[index].Block != nil {
				v.walk(*shape.Cases[index].Block)
			}
		}
	case *ReactiveFor:
		v.walk(shape.Loop)
	case *ReactiveForOf:
		v.walk(shape.Loop)
	case *ReactiveForIn:
		v.walk(shape.Loop)
	case *ReactiveWhile:
		v.walk(shape.Loop)
	case *ReactiveDoWhile:
		v.walk(shape.Loop)
	case *ReactiveLabelTerminal:
		v.walk(shape.Block)
	case *ReactiveTry:
		v.walk(shape.Block)
		v.walk(shape.Handler)
	}
}

// AnalyzePreservedManualMemoization runs the reactive-scope pipeline over one function and validates.
//
// Exported so the rule surface does not have to know the pass order. That order is upstream's and is
// load-bearing rather than incidental: the validation reads which scopes survived, which were pruned
// and which absorbed others in a merge, and each of those facts is written by a different pass. A
// caller running them in a different order gets a validation against a half-built world, which
// reports plausibly and is wrong.
//
// Expects `Lower` and `Construct` to have run already, since a caller that has a `*Function` has
// done both by definition.
func AnalyzePreservedManualMemoization(function *Function,
	typeChecker *shimchecker.Checker) []PreserveManualMemoizationFinding {
	if function == nil {
		return nil
	}
	InferReactive(function, typeChecker)
	DropManualMemoization(function)

	ranges := InferMutableRanges(function)
	set := FindDisjointMutableValuesWithRanges(function, ranges)
	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
	BuildReactiveScopeTerminals(function, scopes, identity)
	dependencies := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)

	tree, _ := BuildReactiveFunction(function)
	if tree == nil {
		return nil
	}

	MergeReactiveScopesThatInvalidateTogether(tree, function, dependencies, typeChecker)
	PruneNonEscapingScopes(tree, function, dependencies, typeChecker)
	PruneUnusedScopes(tree, dependencies)
	PruneAlwaysInvalidatingScopes(tree, function, dependencies)
	PruneNonReactiveDependencies(tree, function, dependencies)

	return ValidatePreservedManualMemoization(tree, function, scopes)
}

// ForEachFunctionLike calls visit for every outermost function-like node under root.
//
// Exported for the rule surface. Outermost only, which is the same denominator every measurement in
// this package uses: a nested function is reached through `Function.Functions` rather than by
// walking into it, because an IdentifierId names one value in this function and a different value in
// a nested one.
func ForEachFunctionLike(root *ast.Node, visit func(*ast.Node)) {
	if root == nil {
		return
	}
	root.ForEachChild(func(node *ast.Node) bool {
		if ast.IsFunctionLike(node) {
			visit(node)
			return false
		}
		ForEachFunctionLike(node, visit)
		return false
	})
}
