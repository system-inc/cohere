package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// unstableNestedState carries what every judgment needs, so the helpers below read as upstream's
// free functions rather than as a chain of parameters.
type unstableNestedState struct {
	ruleContext rule.Context
	isComponent map[*ast.Node]bool

	allowAsProperties   bool
	propertyNameMatches func(string) bool
}

// validate reproduces upstream's function of the same name.
//
// The order of the exceptions is upstream's order and it is load bearing: several of them overlap,
// and `isDeclaredInsideProps` is computed before the component test because the message depends on
// it even when component-hood is established by a different route.
func (state *unstableNestedState) validate(node *ast.Node) {
	if node == nil || node.Parent == nil {
		return
	}

	isDeclaredInsideProperties := state.isComponentInProperty(node)

	if !state.isComponent[node] &&
		!state.isFunctionComponentInsideClassComponent(node) &&
		!isDeclaredInsideProperties {
		return
	}

	switch {
	// The `allowAsProps` option, and the render-prop exemption that applies regardless of it.
	case isDeclaredInsideProperties && (state.allowAsProperties || state.isComponentInRenderProperty(node)):
		return

	// A component created inside `items.map(...)` is a callback rather than a nested component.
	// Upstream tests the node and its parent, which is what catches both the arrow itself and a
	// function expression sitting under one.
	case isMapCallExpression(node), isMapCallExpression(semanticParentOf(node)):
		return

	// A component returned from a hook is the hook's product, not the render's. This also covers
	// the `() => null` cleanup an effect returns.
	case state.isReturnStatementOfHook(node):
		return

	// An object whose key is a render prop holds a renderer rather than a nested component.
	case state.isDirectValueOfRenderProperty(node):
		return

	// A class component nested in a render method is reported through the class itself, so the
	// render method must not report it a second time.
	case state.isInsideRenderMethod(node):
		return

	// A detected "component" that returns no JSX is a handler that happened to be capitalized.
	case state.isStatelessComponentReturningNull(node):
		return
	}

	parentComponentNode := state.closestComponentAncestor(node)
	if parentComponentNode == nil {
		return
	}

	parentName := resolveNestedComponentName(parentComponentNode)

	// A lowercase parent is not a component React will render, so nothing nested inside it is
	// remounted by this mechanism. Upstream names `function createTestComponent()` as the case.
	if parentName != "" && !startsUppercase(parentName) {
		return
	}

	// No deduplication guard here, and that is measured rather than assumed. An earlier draft
	// carried a map keyed on the reported node, on the theory that a wrapping `memo(...)` anchor
	// could be reached from two different nodes. It cannot: this rule's walk visits each node once
	// and `validate` keys on the node it was handed, not on the anchor. Probed over three wrapper
	// shapes, including a doubly-nested `memo(forwardRef(...))`, and no node was validated more
	// than once in any of them. A mutation removing the guard survived the whole suite, which is
	// what sent me to check.
	//
	// Upstream cannot report twice either, for the same structural reason: its five listeners are
	// keyed on disjoint node types.
	//
	// The enumerated callers are this file's own `KindSourceFile` walk and nothing else. If a
	// second entry point is ever added, this verdict is void.
	message := unstableNestedMessage(parentName)
	if isDeclaredInsideProperties && !state.allowAsProperties {
		message += componentAsPropsInfo
	}

	state.ruleContext.ReportNode(node, rule.Message{
		Id:          "unstableNestedComponent",
		Description: message,
	})
}

// closestComponentAncestor is upstream's `getClosestMatchingParent(node, context, components.get)`.
//
// Strictly an ancestor search: upstream starts at `node.parent` and never considers the node
// itself, which matters because the node being judged is usually a component too and would
// otherwise be its own parent.
//
// Upstream also stops at `Program`, so a component at file scope has no parent component and is
// never reported. That is the whole reason this rule is quiet on ordinary code.
// The explicit file stop is equivalent to letting the loop run out, and a mutation removing it
// survives: `SourceFile.Parent` is nil, so the walk terminates there either way, and nothing above
// a file can be in the component map. Measured in a throwaway probe on 2026-08-27, with a control
// that asserted the opposite and failed: a parsed source file's `Parent` is nil. The probe is
// deleted; the measurement is here, which is where the next reader will look.
//
// It is kept because it states upstream's `node.parent.type === 'Program'` stop at the line, which
// is a readability claim rather than a behavioural one.
func (state *unstableNestedState) closestComponentAncestor(node *ast.Node) *ast.Node {
	for current := semanticParentOf(node); current != nil; current = semanticParentOf(current) {
		if current.Kind == ast.KindSourceFile {
			return nil
		}
		if state.isComponent[current] {
			return current
		}
	}
	return nil
}

// isReturnStatementOfHook is upstream's function of the same name.
//
// Two conditions, and the second is looser than it reads. The node must sit directly under a return
// statement, and the closest enclosing call must have a callee whose name matches `use[A-Z0-9]`.
// Upstream reads `callExpression.callee.name`, which is undefined for a member callee, so
// `React.useCallback(() => ...)` does not satisfy this and is exempted by a different route.
func (state *unstableNestedState) isReturnStatementOfHook(node *ast.Node) bool {
	parent := semanticParentOf(node)
	if parent == nil || parent.Kind != ast.KindReturnStatement {
		return false
	}

	call := closestMatchingAncestor(node, func(candidate *ast.Node) bool {
		return candidate.Kind == ast.KindCallExpression
	})
	if call == nil {
		return false
	}
	callee := skipParenthesesOptional(call.AsCallExpression().Expression)
	if callee == nil || callee.Kind != ast.KindIdentifier {
		return false
	}
	return hookNamePattern.MatchString(callee.Text())
}

// isComponentInRenderProperty is upstream's function of the same name.
//
// Three routes, checked in upstream's order.
func (state *unstableNestedState) isComponentInRenderProperty(node *ast.Node) bool {
	parent := semanticParentOf(node)

	// `{ renderFooter: () => <div /> }`, matched on the object key.
	if parent != nil && parent.Kind == ast.KindPropertyAssignment {
		if key := parent.AsPropertyAssignment().Name(); key != nil &&
			key.Kind == ast.KindIdentifier && state.propertyNameMatches(key.Text()) {
			return true
		}
	}

	// `<Component>{() => <div />}</Component>`, a render prop passed as children. Upstream tests
	// `node.parent.type === 'JSXExpressionContainer'` with a `JSXElement` grandparent. Ours spells
	// the container `KindJsxExpression`, and the chain is JsxExpression > JsxElement with no
	// intervening node, matching estree exactly for this shape.
	//
	// # This branch is inert, in upstream as well as here, and it is kept anyway
	//
	// A mutation disabling it survives the sweep. That is not a fixture gap: `validate` returns at
	// its first gate for every node this branch can answer true for, because a bare arrow passed as
	// children is not a detected component and is not in a prop, so the exceptions below it are
	// never consulted.
	//
	// Measured here across four shapes, a bare arrow, an arrow with a block body, an arrow
	// returning a nested component, and a named function expression: every node for which this
	// branch answers true has `isComponent=false` and `isComponentInProp=false`.
	//
	// Measured at the authority too, rather than concluding our port had narrowed something.
	// Instrumenting the installed 7.37.5 build over the whole corpus: 185 function-like nodes
	// reach the rule, upstream's own version of this branch answers true for 2 of them, and neither
	// passes `components.get`. So the exception is dead in the original as well.
	//
	// Kept rather than deleted because the brief's standing instruction is fidelity to the
	// decision: upstream states that children-as-render-prop is exempt, and a future change to the
	// detection pass could make that statement load bearing. Deleting it would silently drop a
	// judgment upstream made. The measurement is here so the next reader does not spend the sweep
	// round I spent.
	if parent != nil && parent.Kind == ast.KindJsxExpression {
		if grandparent := semanticParentOf(parent); grandparent != nil &&
			grandparent.Kind == ast.KindJsxElement {
			return true
		}
	}

	// `<Component renderFooter={() => <div />} />`, matched on the attribute name, plus the
	// explicit `children` prop.
	//
	// Ours puts a `KindJsxAttributes` list between the attribute and the element, which estree does
	// not have. That extra level does not matter here because the walk goes upward from the
	// container to the attribute, which is one step in both trees.
	container := closestMatchingAncestor(node, func(candidate *ast.Node) bool {
		return candidate.Kind == ast.KindJsxExpression
	})
	if container == nil {
		return false
	}
	attribute := semanticParentOf(container)
	if attribute == nil || attribute.Kind != ast.KindJsxAttribute {
		return false
	}
	name := attribute.AsJsxAttribute().Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return false
	}
	propertyName := name.Text()
	return state.propertyNameMatches(propertyName) || propertyName == "children"
}

// isDirectValueOfRenderProperty is upstream's function of the same name.
//
// Narrower than `isComponentInRenderProp`'s first route despite looking identical: this one is not
// gated on the node being in a prop at all, so it exempts a bare `const rows = { render: () =>
// <div /> }` written inside a component.
func (state *unstableNestedState) isDirectValueOfRenderProperty(node *ast.Node) bool {
	parent := semanticParentOf(node)
	if parent == nil || parent.Kind != ast.KindPropertyAssignment {
		return false
	}
	key := parent.AsPropertyAssignment().Name()
	if key == nil || key.Kind != ast.KindIdentifier {
		return false
	}
	return state.propertyNameMatches(key.Text())
}

// isComponentInProperty is upstream's function of the same name.
//
// Upstream's first branch is `isPropertyOfObjectExpressionMatcher(node)`, whose body is
// `node && node.parent && node.parent.type === 'Property'`. Despite the name, which reads as though
// it were about the property rather than about its value, that is the node's direct parent, so it
// asks whether the function is the value of an object property.
//
// I first read it as a grandparent test, on the strength of the helper's name, and wrote a doc
// comment asserting the intuitive reading was wrong. The comment was the thing that was wrong.
// Measured against the installed 7.37.5 build on 2026-08-27 by evaluating the condition at every
// validated node for upstream's invalid cases 23, 30 and 32: every reported arrow has
// `parent=Property` with the condition true, and each of those findings carries the as-props
// suffix, which is only appended when this branch answers true. Under the grandparent reading all
// five of those cases went silent.
//
// That is the "never document a limit you did not measure" failure, arriving as a confident
// paragraph rather than as a missing check, and it is recorded here because a future reader
// comparing the helper's name against its body will have the same instinct.
func (state *unstableNestedState) isComponentInProperty(node *ast.Node) bool {
	if parent := semanticParentOf(node); parent != nil &&
		parent.Kind == ast.KindPropertyAssignment {
		return returnsJsx(state.ruleContext, node)
	}

	attribute := closestMatchingAncestor(node, isJsxAttributeOfExpressionContainer)
	if attribute == nil {
		return state.isComponentInsideCreateElementProperties(node)
	}
	return returnsJsx(state.ruleContext, node)
}

// isComponentInsideCreateElementProperties is upstream's `isComponentInsideCreateElementsProp`.
//
// Upstream gates on `components.get(node)` first, so a function that is not already a detected
// component takes no part in this. Then it requires the closest enclosing object literal to be
// exactly the second argument of the closest enclosing `createElement` call, which is the props
// position. Object identity is the test upstream uses and node identity is the same test here.
func (state *unstableNestedState) isComponentInsideCreateElementProperties(node *ast.Node) bool {
	if !state.isComponent[node] {
		return false
	}

	// The kind guard is not an optimization. `createElementCallCounts` forwards to
	// `isPragmaCreateElementCall`, which opens with an unchecked `node.AsCallExpression()`; its
	// existing callers all reach it from a `KindCallExpression` listener, so it has never needed
	// one. An ancestor walk hands it every kind on the way up, and the first `VariableDeclaration`
	// above a nested component panics. Caught by upstream's own passing case 34 on the first run.
	//
	// The cost of getting this wrong is not this rule's verdict: the walk recovers per file, so a
	// panic here takes that file away from every rule in the tree.
	call := closestMatchingAncestor(node, func(candidate *ast.Node) bool {
		return candidate.Kind == ast.KindCallExpression && createElementCallCounts(state.ruleContext, candidate)
	})
	if call == nil {
		return false
	}
	arguments := call.AsCallExpression().Arguments
	if arguments == nil || len(arguments.Nodes) < 2 {
		return false
	}

	// Argument index 1 is the props position, which is upstream's `arguments[1]`.
	//
	// A mutation changing it to 0 survives. Instrumented over all 77 corpus cases plus probe
	// shapes: this route is reached 92 times once the paths that short-circuit in
	// `isComponentInProp`'s property branch are excluded, exactly one of those reaches a
	// `createElement` call carrying two or more arguments, and for that one both indices answer
	// false. Every other reach is a direct property value, which an earlier branch claims first.
	//
	// A distinguishing input needs a detected component inside a props object that is not itself a
	// direct property value; three attempts to build one produced source upstream does not detect
	// as a component at all. Recorded as unresolved rather than as equivalence.
	object := closestMatchingAncestor(node, func(candidate *ast.Node) bool {
		return candidate.Kind == ast.KindObjectLiteralExpression
	})
	return object != nil && skipParenthesesOptional(arguments.Nodes[1]) == object
}

// isStatelessComponentReturningNull is upstream's function of the same name.
//
// It asks `getStatelessComponent(node)` again rather than reading the registry, because the
// registry holds the anchor a component reports on and this question is about the node itself. A
// `memo(() => null)` is in the registry under the call while this asks about the arrow.
func (state *unstableNestedState) isStatelessComponentReturningNull(node *ast.Node) bool {
	component, isStateless := statelessComponentFor(state.ruleContext, node, nil)
	if !isStateless {
		return false
	}
	return !returnsJsx(state.ruleContext, component)
}

// isInsideRenderMethod is upstream's function of the same name, and here it is structurally
// unreachable rather than merely narrow.
//
// # What upstream's guard actually protects against
//
// Its test is `node.parent.type === 'MethodDefinition' && node.parent.key.name === 'render'`, which
// is the node's direct parent. In estree a class method is a `MethodDefinition` whose `value` is a
// separate `FunctionExpression`, so exactly one node in a class satisfies this: the render method's
// own function expression. Upstream's comment calls it "prevent reporting nested class components
// twice", and that is what it does: the render method's function expression is itself detected as a
// component sitting inside a class component, so without this guard the render method would report
// alongside whatever is nested in it.
//
// Measured against the installed 7.37.5 build on 2026-08-27, by evaluating upstream's own two
// conditions at every node the rule validates, for a class whose render nests a class component:
//
//	ClassDeclaration@2   parent=Program          isInsideRenderMethod=false
//	FunctionExpression@3 parent=MethodDefinition isInsideRenderMethod=true
//	ClassDeclaration@4   parent=BlockStatement   isInsideRenderMethod=false
//	FunctionExpression@4 parent=MethodDefinition isInsideRenderMethod=true
//
// The nested class, which is the thing that reports, answers false. Only the two method function
// expressions answer true.
//
// # Why there is nothing to reproduce
//
// Our parser has no separate node for a method's value: a `KindMethodDeclaration` carries its own
// parameters and body, so the node estree gates on does not exist here and is never handed to a
// listener. This rule's own walk anchors on five kinds and `KindMethodDeclaration` is not among
// them, matching upstream's five listeners, which name `FunctionExpression` rather than the method.
//
// So the guard has no input in this tree. It is written as a predicate that always answers false
// rather than omitted, because omitting it silently would leave the next reader comparing this file
// against upstream and finding a missing exception with no explanation.
//
// # What I got wrong, recorded because the wrong version passed forty cases
//
// The first version asked "is this node somewhere inside a render method", walking up through the
// enclosing block to the method. That reads as the same question and is not: it exempted every
// component nested in a render method, which is precisely the population this rule exists to
// report. It made four of upstream's failing cases silent while all forty passing cases stayed
// green, and only the corpus caught it.
func (state *unstableNestedState) isInsideRenderMethod(node *ast.Node) bool {
	// Upstream additionally requires the parent component to be a class, but the parent test above
	// can never pass here, so the class test is not consulted. Both halves are named in the doc
	// comment rather than written as dead code.
	//
	// A method's value has no node of its own in this parser, so nothing can satisfy upstream's
	// `node.parent.type === 'MethodDefinition'`. `TestNoUnstableNestedComponentsRenderMethodGuard`
	// pins that: it walks a class whose render nests a component and asserts no validated node has
	// a method declaration as its semantic parent.
	return false
}

// isFunctionComponentInsideClassComponent is upstream's function of the same name.
//
// Upstream's own comment says why it exists: its detection pass fails to find a function component
// declared inside a class component, so this recovers those. All four conjuncts are required, and
// the third is what confines it to classes.
//
// # A mutation making this always false survives, and the reason is a parser difference
//
// Measured against the installed 7.37.5 build on 2026-08-27 by evaluating upstream's four conjuncts
// beside `components.get` at every validated node across the whole corpus: 196 nodes, the recovery
// fires for 8 of them, and it is decisive, meaning `components.get` was false, for exactly 3.
//
// Those 3 are in upstream's invalid cases 10, 11 and 29, and in every one the decisive node is the
// class component's render method function expression rather than the nested component. Upstream
// recovers that node into the rule, and then `isInsideRenderMethod` immediately drops it again, so
// the pair is a no-op that exists because estree gives a method's value a node of its own.
//
// Our parser gives it none, so neither half has any input here, which is the same reason
// `isInsideRenderMethod` above is a constant. All three of those cases report correctly without the
// recovery contributing anything.
//
// Kept rather than deleted, on the same reasoning as the children-render-prop branch: it states
// upstream's judgment, and a change to `collectDetectedComponents` could make it load bearing.
// Three probe shapes were built to find an input where it decides something here, a function
// component in a render method, one in an ordinary method, and an arrow in a render method, and our
// detection pass answers true for all three before this is consulted.
func (state *unstableNestedState) isFunctionComponentInsideClassComponent(node *ast.Node) bool {
	parentComponentNode := state.parentComponentOf(node)
	if parentComponentNode == nil {
		return false
	}
	if parentComponentNode.Kind != ast.KindClassDeclaration &&
		parentComponentNode.Kind != ast.KindClassExpression {
		return false
	}

	parentStateless := state.parentStatelessComponentOf(node)
	if parentStateless == nil {
		return false
	}
	if _, isStateless := statelessComponentFor(state.ruleContext, parentStateless, nil); !isStateless {
		return false
	}
	return returnsJsx(state.ruleContext, node)
}

// parentComponentOf is upstream's `getParentComponent`, which tries the class forms and then falls
// back to the stateless walk.
//
// # This walk includes the node itself, matching upstream
//
// Upstream's three components all start from `getScope(context, node)`, and ESLint's scope for a
// class or a function-like node is the scope that node creates, not the one it sits in. So
// `getParentES6Component(theClass)` returns that class, and `getParentStatelessComponent(theFn)`
// returns that function.
//
// Measured against the installed 7.37.5 build on 2026-08-27: over the whole corpus, 268 function-
// like nodes reached `getParentStatelessComponent` and `getScope(node).block === node` for all 268,
// with zero exceptions. Instrumenting the real helpers on upstream's invalid case 14 gives
// `ClassDeclaration@2: es6=ClassDeclaration@2` and
// `FunctionDeclaration@4: stateless=FunctionDeclaration@4`, both naming themselves.
//
// An exclusive walk reads as the obviously correct one, because "parent" is in the name and because
// `closestComponentAncestor` below genuinely is exclusive. They are not the same walk:
// `getClosestMatchingParent` starts at `node.parent` explicitly in its own source, while these
// three start at a scope. Two walks in one rule, opposite on this point, and only one of them says
// so in its name.
//
// # What is not established: whether the difference is observable here
//
// An earlier revision of this comment claimed the exclusive version cost nine of upstream's failing
// cases. That was wrong, and it is recorded rather than deleted because it is the exact shape the
// brief warns about, a confident divergence written from attribution rather than from measurement.
// Both under-report groups were fixed by `isInsideRenderMethod` and `isComponentInProp`; running
// the whole corpus with both walks made exclusive again passes all 77.
//
// A mutation flipping this walk back to exclusive therefore survives the sweep, and no fixture was
// added for it, because none could be justified: three candidate shapes were built to separate the
// two, a function component directly in a render method, one behind a plain helper function, and an
// arrow inside an arrow, and all three produce one finding under both versions and match upstream.
//
// This is deliberately not filed as an equivalence verdict. The only caller requires the result to
// be a class, and a node being validated is never the class enclosing it, so inclusivity cannot
// change that half; the stateless half then asks `statelessComponentFor` about whatever it found,
// and for the inclusive case that is the node itself, which is already a component by construction.
// Both routes reach the same verdict, which is the "same answer by a different path" shape the
// brief names, and I could not prove no input separates them.
//
// The inclusive spelling is kept because it is what upstream does and the measurement above is
// solid, not because the difference was shown to matter.
func (state *unstableNestedState) parentComponentOf(node *ast.Node) *ast.Node {
	for current := node; current != nil; current = semanticParentOf(current) {
		if isComponentClass(current) {
			return current
		}
		if isCreateReactClassCall(current) {
			return current
		}
	}
	return state.parentStatelessComponentOf(node)
}

// parentStatelessComponentOf is upstream's `getParentStatelessComponent`, written as an AST walk.
//
// Upstream walks `scope.upper` calling `getStatelessComponent(scope.block)`. The equivalence of
// that chain to this one is measured rather than assumed; the measurement and its control are
// recorded in this file's header comment.
//
// The node itself is included, matching upstream. See `parentComponentOf` for the measurement:
// ESLint's scope for a function-like node is the scope it creates, so the first thing upstream's
// loop asks about is the node itself.
func (state *unstableNestedState) parentStatelessComponentOf(node *ast.Node) *ast.Node {
	for current := node; current != nil; current = semanticParentOf(current) {
		switch current.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
			if component, isStateless := statelessComponentFor(state.ruleContext, current, nil); isStateless {
				return component
			}
		}
	}
	return nil
}
