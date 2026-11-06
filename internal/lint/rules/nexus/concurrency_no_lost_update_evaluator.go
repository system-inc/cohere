package nexus

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"
)

// concurrencyNoLostUpdateEvaluator walks one expression in evaluation order for one target.
//
// anchor is where the expression starts in the control-flow graph: the write being judged, or the
// declaration of the const whose initializer is being judged. A const read inside the expression is
// stale when a suspension lies between the const's declaration and the anchor.
type concurrencyNoLostUpdateEvaluator struct {
	analysis *concurrencyNoLostUpdateRoot
	target   concurrencyNoLostUpdatePath
	anchor   *ast.Node
}

func (evaluator concurrencyNoLostUpdateEvaluator) leaf(
	dependencies concurrencyNoLostUpdateDependencies,
	suspended bool,
) []concurrencyNoLostUpdateOutcome {
	return []concurrencyNoLostUpdateOutcome{{dependencies: dependencies, suspended: suspended}}
}

// sequence evaluates nodes left to right and unions their values. A suspension in a later node
// stales what earlier nodes read.
func (evaluator concurrencyNoLostUpdateEvaluator) sequence(
	nodes []*ast.Node,
	suspended bool,
) []concurrencyNoLostUpdateOutcome {
	states := evaluator.leaf(0, suspended)
	for _, node := range nodes {
		if node == nil {
			continue
		}
		var next []concurrencyNoLostUpdateOutcome
		for _, state := range states {
			for _, result := range evaluator.evaluate(node, state.suspended) {
				dependencies := state.dependencies
				if result.during {
					dependencies = dependencies.staled()
				}
				next = append(next, concurrencyNoLostUpdateOutcome{
					dependencies: dependencies | result.dependencies,
					suspended:    result.suspended,
					during:       state.during || result.during,
				})
			}
		}
		states = concurrencyNoLostUpdateDeduplicate(next)
	}
	return states
}

// then evaluates second after each outcome of first, keeping only second's value.
func (evaluator concurrencyNoLostUpdateEvaluator) then(
	first []concurrencyNoLostUpdateOutcome,
	second *ast.Node,
) []concurrencyNoLostUpdateOutcome {
	var outcomes []concurrencyNoLostUpdateOutcome
	for _, state := range first {
		for _, result := range evaluator.evaluate(second, state.suspended) {
			result.during = result.during || state.during
			outcomes = append(outcomes, result)
		}
	}
	return outcomes
}

// evaluate returns every path through node, given whether a suspension already happened on the path.
func (evaluator concurrencyNoLostUpdateEvaluator) evaluate(
	node *ast.Node,
	suspended bool,
) []concurrencyNoLostUpdateOutcome {
	if node == nil {
		return evaluator.leaf(0, suspended)
	}
	if control_flow_graph.IsRoot(node) || node.Kind == ast.KindClassExpression {
		// A nested function's body runs later, if at all; its value carries nothing read now.
		return evaluator.leaf(0, suspended)
	}
	if ast.IsTypeNode(node) {
		return evaluator.leaf(0, suspended)
	}

	switch node.Kind {
	case ast.KindParenthesizedExpression, ast.KindNonNullExpression, ast.KindAsExpression,
		ast.KindSatisfiesExpression, ast.KindTypeAssertionExpression:
		return evaluator.evaluate(concurrencyNoLostUpdateSkipTransparent(node), suspended)

	case ast.KindIdentifier:
		return evaluator.leaf(evaluator.identifier(node, suspended), suspended)

	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		if path, isPath := evaluator.analysis.pathOf(node); isPath {
			if matched := evaluator.match(path); matched != 0 {
				return evaluator.leaf(matched, suspended)
			}
			// A static path that is not the target can still be a member read of a const carrying it.
			// Nothing in a static path can suspend, so only its root identifier needs evaluating.
			root := node
			for {
				root = concurrencyNoLostUpdateSkipTransparent(root)
				if root.Kind == ast.KindPropertyAccessExpression {
					root = root.AsPropertyAccessExpression().Expression
					continue
				}
				if root.Kind == ast.KindElementAccessExpression {
					root = root.AsElementAccessExpression().Expression
					continue
				}
				break
			}
			if root.Kind != ast.KindIdentifier {
				return evaluator.leaf(0, suspended)
			}
			return evaluator.leaf(evaluator.identifier(root, suspended).modified(), suspended)
		}
		if node.Kind == ast.KindPropertyAccessExpression {
			return concurrencyNoLostUpdateModifiedAll(
				evaluator.evaluate(node.AsPropertyAccessExpression().Expression, suspended))
		}
		access := node.AsElementAccessExpression()
		return concurrencyNoLostUpdateModifiedAll(
			evaluator.sequence([]*ast.Node{access.Expression, access.ArgumentExpression}, suspended))

	case ast.KindAwaitExpression:
		operand := node.AsAwaitExpression().Expression
		outcomes := evaluator.evaluate(operand, suspended)
		// An awaited call's value is the callee's answer, produced across the suspension, and not a
		// computation made here from what this function handed it. Measured on ahra: the OAuth refresh
		// in `modules/spotify/SpotifyApi.ts:91` and `modules/finance/connections/QuickBooksClient.ts:156`
		// sends the old refresh token in a `fetch` body and writes back the rotated one from the
		// response, and carrying the argument through the call reported both as computed from the old
		// token. Awaiting a value that is not a call (`await count`) still carries it.
		producedByCallee := false
		// An awaited read of the target store is the exception: the callee's answer is the entry. It
		// is read as the promise settles, so this await does not stale it; a later one does.
		readOfTarget := false
		switch skipped := concurrencyNoLostUpdateSkipTransparent(operand); {
		case skipped == nil:
		case evaluator.isTargetStoreRead(skipped):
			readOfTarget = true
		case skipped.Kind == ast.KindCallExpression, skipped.Kind == ast.KindNewExpression,
			skipped.Kind == ast.KindTaggedTemplateExpression:
			producedByCallee = true
		}
		for index := range outcomes {
			if producedByCallee {
				outcomes[index].dependencies = 0
			}
			if !readOfTarget {
				outcomes[index].dependencies = outcomes[index].dependencies.staled()
			}
			outcomes[index].suspended = true
			outcomes[index].during = true
		}
		return concurrencyNoLostUpdateDeduplicate(outcomes)

	case ast.KindYieldExpression:
		// The value of a yield is whatever the caller sends back, so nothing read here survives it.
		outcomes := evaluator.evaluate(node.AsYieldExpression().Expression, suspended)
		for index := range outcomes {
			outcomes[index].dependencies = 0
			outcomes[index].suspended = true
			outcomes[index].during = true
		}
		return concurrencyNoLostUpdateDeduplicate(outcomes)

	case ast.KindBinaryExpression:
		binary := node.AsBinaryExpression()
		operator := binary.OperatorToken.Kind
		switch {
		case operator == ast.KindAmpersandAmpersandToken || operator == ast.KindBarBarToken ||
			operator == ast.KindQuestionQuestionToken:
			left := evaluator.evaluate(binary.Left, suspended)
			outcomes := append([]concurrencyNoLostUpdateOutcome{}, left...)
			outcomes = append(outcomes, evaluator.then(left, binary.Right)...)
			return concurrencyNoLostUpdateDeduplicate(outcomes)
		case operator == ast.KindCommaToken:
			return concurrencyNoLostUpdateDeduplicate(
				evaluator.then(evaluator.evaluate(binary.Left, suspended), binary.Right))
		case operator == ast.KindEqualsToken:
			// A nested assignment's value is its right-hand side.
			return evaluator.evaluate(binary.Right, suspended)
		}
		return concurrencyNoLostUpdateModifiedAll(evaluator.sequence([]*ast.Node{binary.Left, binary.Right}, suspended))

	case ast.KindConditionalExpression:
		conditional := node.AsConditionalExpression()
		condition := evaluator.evaluate(conditional.Condition, suspended)
		outcomes := evaluator.then(condition, conditional.WhenTrue)
		outcomes = append(outcomes, evaluator.then(condition, conditional.WhenFalse)...)
		return concurrencyNoLostUpdateDeduplicate(outcomes)

	case ast.KindObjectLiteralExpression:
		var parts []*ast.Node
		for _, member := range node.AsObjectLiteralExpression().Properties.Nodes {
			switch member.Kind {
			case ast.KindPropertyAssignment:
				assignment := member.AsPropertyAssignment()
				if name := assignment.Name(); name != nil && name.Kind == ast.KindComputedPropertyName {
					parts = append(parts, name.AsComputedPropertyName().Expression)
				}
				parts = append(parts, assignment.Initializer)
			case ast.KindShorthandPropertyAssignment:
				parts = append(parts, member.AsShorthandPropertyAssignment().Name())
			case ast.KindSpreadAssignment:
				parts = append(parts, member.AsSpreadAssignment().Expression)
			}
		}
		return concurrencyNoLostUpdateModifiedAll(evaluator.sequence(parts, suspended))

	case ast.KindCallExpression:
		if evaluator.isTargetStoreRead(node) {
			// The arguments run first and carry nothing of the entry; the call's answer is the entry.
			var children []*ast.Node
			node.ForEachChild(func(child *ast.Node) bool {
				children = append(children, child)
				return false
			})
			outcomes := evaluator.sequence(children, suspended)
			for index := range outcomes {
				outcomes[index].dependencies = concurrencyNoLostUpdateFreshVerbatim
			}
			return concurrencyNoLostUpdateDeduplicate(outcomes)
		}
	}

	var children []*ast.Node
	node.ForEachChild(func(child *ast.Node) bool {
		children = append(children, child)
		return false
	})
	return concurrencyNoLostUpdateModifiedAll(evaluator.sequence(children, suspended))
}

// match compares a static path read against the target: the target itself is a verbatim read, a
// longer path through it is a read of the target that something is then computed from.
func (evaluator concurrencyNoLostUpdateEvaluator) match(
	path concurrencyNoLostUpdatePath,
) concurrencyNoLostUpdateDependencies {
	// A store entry is read only through its read call, never through a path.
	if evaluator.target.store != "" {
		return 0
	}
	if !path.sameRoot(evaluator.target) || len(path.names) < len(evaluator.target.names) {
		return 0
	}
	for index, name := range evaluator.target.names {
		if path.names[index] != name {
			return 0
		}
	}
	if len(path.names) == len(evaluator.target.names) {
		return concurrencyNoLostUpdateFreshVerbatim
	}
	return concurrencyNoLostUpdateFreshModified
}

// identifier evaluates one identifier read: the target variable itself, or a const carrying it.
func (evaluator concurrencyNoLostUpdateEvaluator) identifier(
	node *ast.Node,
	suspended bool,
) concurrencyNoLostUpdateDependencies {
	symbol := evaluator.analysis.resolve(node)
	if symbol == nil {
		return 0
	}
	if !evaluator.target.isThis && evaluator.target.store == "" && evaluator.target.root == symbol &&
		len(evaluator.target.names) == 0 {
		return concurrencyNoLostUpdateFreshVerbatim
	}
	local := evaluator.analysis.locals[symbol]
	if local == nil || local.name == node {
		return 0
	}
	dependencies := evaluator.analysis.localDependencies(symbol, local, evaluator.target)
	if dependencies == 0 {
		return 0
	}
	if suspended || evaluator.analysis.suspensionBetween(symbol, evaluator.anchor, evaluator.target) {
		dependencies = dependencies.staled()
	}
	return dependencies
}

// isTargetStoreRead reports whether a node is a read call of exactly the entry being judged.
func (evaluator concurrencyNoLostUpdateEvaluator) isTargetStoreRead(node *ast.Node) bool {
	if evaluator.target.store == "" || node.Kind != ast.KindCallExpression {
		return false
	}
	access := evaluator.analysis.storeAccessOf(node)
	return access != nil && !access.isWrite && access.target.sameStoreEntry(evaluator.target)
}
