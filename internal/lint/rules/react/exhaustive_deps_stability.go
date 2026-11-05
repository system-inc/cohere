package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// isStableValue reports whether a binding's value is the same object on every render, so leaving it
// out of a dependency array cannot cause a stale read.
//
// Recognized by the name of the Hook that produced it rather than by its type, which is upstream's
// `isStableKnownHookValue`. The rule's doc comment records why the type checker is deliberately not
// used here even though it could answer more.
//
// Four shapes are stable:
//
//	const r = useRef(...)                  the container never changes, only `r.current` does
//	const [, setS] = useState(...)         the setter is guaranteed stable by React
//	const [, dispatch] = useReducer(...)   same guarantee
//	const [, start] = useTransition()      same guarantee
//	const x = useEffectEvent(...)          stable by construction, that is its purpose
//	const N = 1                            a const bound to a primitive literal cannot change
//
// The array-destructuring shapes are stable only in their second position. `const [s, setS] =
// useState()` makes `setS` stable and `s` emphatically not: `s` is the value that changes, and
// treating it as stable would silence the most common real finding this rule makes.
//
// A function declared in the component body is stable when everything it reads is itself stable,
// which is upstream's `isFunctionWithoutCapturedValues`. That recursion is deliberately one level
// deep, matching React: oxc recurses without limit and treats a cycle as stable, which makes two
// mutually recursive component-body functions look stable when neither is. The measurement is in
// the rule's doc comment.
//
// Mutates: visiting holds symbol while its function's reads are judged and drops it on return, so a cycle stops
func isStableValue(ruleContext rule.Context, symbol *ast.Symbol, callback *ast.Node, componentScope *ast.Node,
	visiting map[*ast.Symbol]bool) bool {

	if visiting[symbol] {
		// Re-entry means a cycle, and a cycle is not stable: two functions that call each other are
		// both rebuilt on every render. This is the opposite of oxc's answer and it is the
		// divergence the doc comment records.
		return false
	}

	declaration := stableDeclarationOf(symbol)
	if declaration == nil {
		return false
	}

	switch declaration.Kind {
	case ast.KindVariableDeclaration:
		return isStableVariableDeclaration(ruleContext, declaration, symbol, callback, componentScope, visiting)

	case ast.KindBindingElement:
		return isStableDestructuredHookResult(ruleContext, declaration, componentScope)

	case ast.KindFunctionDeclaration:
		visiting[symbol] = true
		defer delete(visiting, symbol)
		return functionReadsOnlyStableValues(ruleContext, declaration, componentScope, visiting)
	}
	return false
}

// stableDeclarationOf picks the declaration stability is decided from.
//
// The earliest by source position rather than index zero, because a symbol can carry more than one
// declaration and the order `Declarations` arrives in is not something this rule should depend on.
// Upstream reads `defs[0]`, which for the shapes this rule sees is the value declaration, and the
// earliest-by-position choice reproduces that without inheriting the indexing assumption. A merged
// type declaration cannot be a stable Hook result, so a shape with several declarations is answered
// the same way either way.
func stableDeclarationOf(symbol *ast.Symbol) *ast.Node {
	var earliest *ast.Node
	for _, declaration := range symbol.Declarations {
		if earliest == nil || declaration.Pos() < earliest.Pos() {
			earliest = declaration
		}
	}
	return earliest
}

// isStableVariableDeclaration answers isStableValue for a symbol bound by a variable declaration.
//
// Mutates: visiting holds symbol while its function's reads are judged and drops it on return, so a cycle stops
func isStableVariableDeclaration(ruleContext rule.Context, declaration *ast.Node, symbol *ast.Symbol,
	callback *ast.Node, componentScope *ast.Node, visiting map[*ast.Symbol]bool) bool {

	variable := declaration.AsVariableDeclaration()
	initializer := unwrapExpression(variable.Initializer)
	if initializer == nil {
		return false
	}

	// A function bound to a name in the component body is stable when everything it reads is.
	switch initializer.Kind {
	case ast.KindArrowFunction, ast.KindFunctionExpression:
		visiting[symbol] = true
		defer delete(visiting, symbol)
		return functionReadsOnlyStableValues(ruleContext, initializer, componentScope, visiting)
	}

	// A `const` bound to a primitive literal cannot change, so it never needs declaring. `let` is
	// excluded because it can be reassigned, and a regular expression literal is excluded because it
	// is a fresh object each evaluation.
	if isConstDeclaration(declaration) && isPrimitiveLiteral(initializer) {
		return true
	}

	if initializer.Kind != ast.KindCallExpression {
		return false
	}
	hookName := hookNameWithoutReactNamespace(initializer.AsCallExpression().Expression)
	return hookName == "useRef" || hookName == "useEffectEvent"
}

// isStableDestructuredHookResult answers the array-destructuring shapes, which are stable only in
// their second position.
//
// React's guarantee is about the value the Hook returns, not about the name it was bound to, so a
// setter that gets reassigned is no longer stable. Upstream tracks the binding's write count and
// gives up past one; this asks whether anything in the component assigns to the name, which is the
// same question for the shapes that can reach here: the destructuring itself is not an assignment
// expression, so any write found is a genuine reassignment. Upstream's own case for this is
// `let [count, setCount] = useState(0); setCount = unstableProp`, and without the check that reads
// as stable and the whole finding disappears.
func isStableDestructuredHookResult(ruleContext rule.Context, declaration *ast.Node,
	componentScope *ast.Node) bool {

	pattern := declaration.Parent
	if pattern == nil || pattern.Kind != ast.KindArrayBindingPattern {
		return false
	}
	elements := pattern.AsBindingPattern().Elements.Nodes
	if len(elements) != 2 || elements[1] != declaration {
		return false
	}

	variable := pattern.Parent
	if variable == nil || variable.Kind != ast.KindVariableDeclaration {
		return false
	}
	initializer := unwrapExpression(variable.AsVariableDeclaration().Initializer)
	if initializer == nil || initializer.Kind != ast.KindCallExpression {
		return false
	}
	switch hookNameWithoutReactNamespace(initializer.AsCallExpression().Expression) {
	case "useState", "useReducer", "useActionState", "useTransition":
		return !isReassignedInComponent(ruleContext, declaration, componentScope)
	}
	return false
}

// isReassignedInComponent reports whether anything in the component assigns to a binding.
//
// A walk of the component rather than a reference index, because the question is scoped to one
// function body and the binding is a local: every occurrence that could write to it is inside the
// node this rule already holds.
func isReassignedInComponent(ruleContext rule.Context, declaration *ast.Node, componentScope *ast.Node) bool {
	target := ruleContext.TypeChecker.GetSymbolAtLocation(declaration.Name())
	if target == nil {
		return false
	}
	reassigned := false
	var walk func(node *ast.Node)
	walk = func(node *ast.Node) {
		if node == nil || reassigned {
			return
		}
		if node.Kind == ast.KindBinaryExpression {
			binary := node.AsBinaryExpression()
			if binary.OperatorToken != nil && binary.OperatorToken.Kind == ast.KindEqualsToken {
				written := unwrapExpression(binary.Left)
				if written != nil && written.Kind == ast.KindIdentifier &&
					ruleContext.TypeChecker.GetSymbolAtLocation(written) == target {
					reassigned = true
					return
				}
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return false
		})
	}
	walk(componentScope)
	return reassigned
}

// functionReadsOnlyStableValues reports whether every component-scope value a function reads is
// itself stable, so the function's identity changing does not matter.
func functionReadsOnlyStableValues(ruleContext rule.Context, function *ast.Node, componentScope *ast.Node,
	visiting map[*ast.Symbol]bool) bool {

	stable := true
	var walk func(node *ast.Node)
	walk = func(node *ast.Node) {
		if node == nil || !stable {
			return
		}
		if node.Kind == ast.KindIdentifier && !isNonDependencyIdentifier(node) {
			symbol := resolveIdentifier(ruleContext, node)
			if symbol != nil && len(symbol.Declarations) > 0 {
				inFunction, inComponent := false, false
				for _, declaration := range symbol.Declarations {
					if nodeContains(function, declaration) {
						inFunction = true
						break
					}
					if nodeContains(componentScope, declaration) {
						inComponent = true
					}
				}
				if !inFunction && inComponent &&
					!isStableValue(ruleContext, symbol, function, componentScope, visiting) {
					stable = false
					return
				}
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return false
		})
	}
	walk(function)
	return stable
}
