// Package flow finds every place a value of one type is put into a slot of another, and relates the two
// part by part.
//
// Three soundness rules ask the same question at the same places (#drbrp8c): adamic/invariant-mutable,
// adamic/nominal-class and adamic/no-optional-widening. tsc accepts `Dog[]` where `Animal[]` is wanted,
// a `DogShelter` where an `AnimalShelter` is, and `{ x }` where `{ x; y?: number }` is, and each is a
// runtime TypeError once the wider name is written through or read from. The places are the ones
// no-unsafe-assignment, -argument and -return already enumerate, so they live here once rather than
// three times with three opinions about what a site is.
package flow

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// Site is one place a value flows into a slot: Node is the expression whose value it is, and where a
// finding is reported; Source is that expression's type and Target the slot's.
type Site struct {
	Node   *ast.Node
	Source *checker.Type
	Target *checker.Type

	// Fresh is a source nobody else holds: an object or array literal, written in place, or a conditional
	// choosing between them. Nothing can write into it through another name, so it is no hole at its own
	// level (probe h11), and its parts are sites of their own (each property and element), so a rule judges
	// its top and does not descend.
	Fresh bool

	// NewContainer is a source made by this expression whose parts were not: `new Map()`, `items.map(...)`,
	// `Object.values(...)`. Nobody else holds the container, so its own slots are no hole, and what it holds
	// may still be shared, so the walk goes on into it with the container's slots read-only. Measured on our
	// four consumers, most of what invariant-mutable reported before this was an array a `.map` had just
	// built, seen as a wider array (#drbrp8c).
	NewContainer bool
}

// Listeners returns the listeners that find every site in the walker's file and hand each to visit.
//
// The sites:
//
//	const x: T = e            the annotation
//	x = e                     the assigned-to expression's type
//	class { p: T = e }        the annotation
//	f(x: T = e)               the annotation, and a defaulted binding element likewise
//	f(e), new C(e)            the argument's contextual type: the resolved parameter, instantiated
//	return e, (): T => e      the function's annotated return type; an unannotated, async or
//	                          generator function has no slot to compare against
//	{ p: e }, { p }, [e]      the contextual type of the property or element
//	e as T, <T>e              an upcast: e's type is assignable to T and not T itself
//
// A site whose source and target are the same type is never handed over: nothing below can differ, and
// it is the overwhelmingly common case. JSX attributes are not sites yet, since JSX is no part of Adamic
// 0.1; their contextual type is the same question and the obvious next listener.
//
// Every rule listening shares the file's walker (WalkerFor), and every listener reaching a node is called
// before the walk moves on, so the first rule there finds the node's sites and the others read them. Each
// rule finding them again asked the checker for every source three times, and an object literal's type is
// built anew on every ask (#m6tyg79). One listener serves every kind, so a rule costs a file one closure.
func (w *Walker) Listeners(visit func(Site)) rule.Listeners {
	if w.typeChecker == nil {
		return nil
	}
	listener := func(node *ast.Node) {
		if w.sitesNode != node {
			w.sites = w.sites[:0]
			siteFinders[node.Kind](w, node)
			w.sitesNode = node
		}
		for _, site := range w.sites {
			visit(site)
		}
	}
	listeners := make(rule.Listeners, len(siteFinders))
	for kind := range siteFinders {
		listeners[kind] = listener
	}
	return listeners
}

// siteFinders is, by node kind, what offers the sites at a node of that kind into the walker's sites.
var siteFinders = map[ast.Kind]func(w *Walker, node *ast.Node){
	ast.KindVariableDeclaration: func(w *Walker, node *ast.Node) {
		declaration := node.AsVariableDeclaration()
		w.annotated(declaration.Type, declaration.Initializer)
	},
	ast.KindPropertyDeclaration: func(w *Walker, node *ast.Node) {
		declaration := node.AsPropertyDeclaration()
		w.annotated(declaration.Type, declaration.Initializer)
	},
	ast.KindParameter: func(w *Walker, node *ast.Node) {
		parameter := node.AsParameterDeclaration()
		w.annotated(parameter.Type, parameter.Initializer)
	},
	ast.KindBinaryExpression: func(w *Walker, node *ast.Node) {
		binary := node.AsBinaryExpression()
		if binary.OperatorToken == nil || binary.OperatorToken.Kind != ast.KindEqualsToken || binary.Left == nil {
			return
		}
		left := ast.SkipParentheses(binary.Left)
		// A literal on the left is a destructuring pattern, whose parts are bindings, not slots.
		if left.Kind == ast.KindObjectLiteralExpression || left.Kind == ast.KindArrayLiteralExpression {
			return
		}
		w.offer(binary.Right, w.typeChecker.GetTypeAtLocation(binary.Left))
	},
	ast.KindCallExpression: (*Walker).arguments,
	ast.KindNewExpression: func(w *Walker, node *ast.Node) {
		if node.AsNewExpression().Arguments == nil {
			return
		}
		w.arguments(node)
	},
	ast.KindReturnStatement: func(w *Walker, node *ast.Node) {
		expression := node.AsReturnStatement().Expression
		if expression == nil {
			return
		}
		w.returned(type_checking.GetParentFunctionNode(node), expression)
	},
	ast.KindArrowFunction: func(w *Walker, node *ast.Node) {
		body := node.AsArrowFunction().Body
		if body == nil || ast.IsBlock(body) {
			return
		}
		w.returned(node, body)
	},
	ast.KindPropertyAssignment: func(w *Walker, node *ast.Node) {
		if IsDestructuringTarget(node.Parent) {
			return
		}
		w.contextual(node.AsPropertyAssignment().Initializer)
	},
	ast.KindShorthandPropertyAssignment: func(w *Walker, node *ast.Node) {
		if IsDestructuringTarget(node.Parent) || node.AsShorthandPropertyAssignment().ObjectAssignmentInitializer != nil {
			return
		}
		name := node.Name()
		if name == nil {
			return
		}
		// The key and the value are one identifier: its contextual type is the property's slot and
		// its type is the value put there, which no-unsafe-assignment reads the same way.
		w.offer(name, checker.Checker_getContextualType(w.typeChecker, name, checker.ContextFlagsNone))
	},
	ast.KindArrayLiteralExpression: func(w *Walker, node *ast.Node) {
		if IsDestructuringTarget(node) {
			return
		}
		for _, element := range node.AsArrayLiteralExpression().Elements.Nodes {
			if element.Kind == ast.KindSpreadElement || element.Kind == ast.KindOmittedExpression {
				continue
			}
			w.contextual(element)
		}
	},
	ast.KindAsExpression:            (*Walker).offerUpcast,
	ast.KindTypeAssertionExpression: (*Walker).offerUpcast,
}

// offer adds the site of expression flowing into target, unless the two are one type or the target has no
// object part, where no judge can rule (see Judge). The target is read first so such a site never asks the
// checker for its source.
func (w *Walker) offer(expression *ast.Node, target *checker.Type) {
	if expression == nil || target == nil || !hasObjectPart(target) {
		return
	}
	source := w.typeChecker.GetTypeAtLocation(expression)
	if source == nil || source == target {
		return
	}
	w.sites = append(w.sites, Site{Node: expression, Source: source, Target: target, Fresh: isFresh(expression),
		NewContainer: isNewContainer(expression)})
}

func (w *Walker) annotated(typeNode *ast.Node, expression *ast.Node) {
	if typeNode == nil || expression == nil {
		return
	}
	w.offer(expression, checker.Checker_getTypeFromTypeNode(w.typeChecker, typeNode))
}

func (w *Walker) contextual(expression *ast.Node) {
	w.offer(expression, checker.Checker_getContextualType(w.typeChecker, expression, checker.ContextFlagsNone))
}

func (w *Walker) returned(function *ast.Node, expression *ast.Node) {
	if function == nil || function.Type() == nil {
		return
	}
	if ast.GetFunctionFlags(function)&(ast.FunctionFlagsAsync|ast.FunctionFlagsGenerator) != 0 {
		return
	}
	w.offer(expression, checker.Checker_getReturnTypeFromAnnotation(w.typeChecker, function))
}

func (w *Walker) arguments(call *ast.Node) {
	for index, argument := range call.Arguments() {
		if argument.Kind == ast.KindSpreadElement {
			continue
		}
		w.offer(argument, checker.Checker_getContextualTypeForArgumentAtIndex(w.typeChecker, call, index))
	}
}

// offerUpcast hands over a cast that only widens. A downcast is no flow at all, since the value is not
// being put anywhere wider, and adamic/no-unchecked-cast judges it. `as const` names no slot.
func (w *Walker) offerUpcast(node *ast.Node) {
	typeNode := node.Type()
	if typeNode == nil || ast.IsConstTypeReference(typeNode) {
		return
	}
	expression := node.Expression()
	target := checker.Checker_getTypeFromTypeNode(w.typeChecker, typeNode)
	if target == nil || !hasObjectPart(target) {
		return
	}
	source := w.typeChecker.GetTypeAtLocation(expression)
	if source == nil || !w.IsAssignable(source, target) {
		return
	}
	w.offer(expression, target)
}

// hasObjectPart is a type the walk can relate parts of: an object type, or a union with one among its
// members. Union members are never unions themselves.
func hasObjectPart(t *checker.Type) bool {
	if t.Flags()&checker.TypeFlagsObject != 0 {
		return true
	}
	if t.Flags()&checker.TypeFlagsUnion != 0 {
		for _, member := range t.Types() {
			if member.Flags()&checker.TypeFlagsObject != 0 {
				return true
			}
		}
	}
	return false
}

// isFresh is an expression whose value is made right here: an object or array literal, parenthesized or
// not, or a conditional whose branches each are one or are nothing (`undefined`, `null`).
func isFresh(expression *ast.Node) bool {
	expression = ast.SkipParentheses(expression)
	switch expression.Kind {
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression:
		return true
	case ast.KindConditionalExpression:
		conditional := expression.AsConditionalExpression()
		return isFreshOrNothing(conditional.WhenTrue) && isFreshOrNothing(conditional.WhenFalse)
	}
	return false
}

func isFreshOrNothing(expression *ast.Node) bool {
	expression = ast.SkipParentheses(expression)
	switch {
	case expression.Kind == ast.KindNullKeyword:
		return true
	case expression.Kind == ast.KindIdentifier && expression.Text() == "undefined":
		return true
	}
	return isFresh(expression)
}

// newArrayMethods are the library methods that return an array they just built, never the receiver or
// anything else already held. `sort` and `reverse` return the receiver and are not here.
var newArrayMethods = map[string]bool{
	"map": true, "filter": true, "slice": true, "concat": true, "flat": true, "flatMap": true,
	"toSorted": true, "toReversed": true, "toSpliced": true, "with": true,
}

// newArrayFunctions are the library calls on a global that return a container they just built.
var newArrayFunctions = map[string]map[string]bool{
	"Array":  {"from": true, "of": true},
	"Object": {"keys": true, "values": true, "entries": true, "fromEntries": true},
}

// isNewContainer is an expression that builds a container its parts were not built with: a `new`, or a
// call to a library method or function that returns a new array. Judged by name, so a user method named
// `map` that returns something it holds would read as new; the names are the library's, and a project
// that reuses them for a shared value is the rare case this accepts.
func isNewContainer(expression *ast.Node) bool {
	expression = ast.SkipParentheses(expression)
	switch expression.Kind {
	case ast.KindNewExpression:
		return true
	case ast.KindCallExpression:
		callee := ast.SkipParentheses(expression.Expression())
		if callee.Kind != ast.KindPropertyAccessExpression {
			return false
		}
		access := callee.AsPropertyAccessExpression()
		name := access.Name().Text()
		if receiver := ast.SkipParentheses(access.Expression); receiver.Kind == ast.KindIdentifier {
			if functions, isGlobal := newArrayFunctions[receiver.Text()]; isGlobal {
				return functions[name]
			}
		}
		return newArrayMethods[name]
	}
	return false
}

// IsDestructuringTarget is a literal standing for a destructuring pattern on the left of an `=`, directly
// or nested inside another literal that is. Its properties and elements are bindings, not slots.
func IsDestructuringTarget(node *ast.Node) bool {
	for current := node; current != nil; {
		parent := current.Parent
		if parent == nil {
			return false
		}
		switch parent.Kind {
		case ast.KindBinaryExpression:
			binary := parent.AsBinaryExpression()
			return binary.OperatorToken != nil && binary.OperatorToken.Kind == ast.KindEqualsToken &&
				binary.Left == current
		case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindParenthesizedExpression,
			ast.KindArrayLiteralExpression, ast.KindObjectLiteralExpression, ast.KindSpreadElement,
			ast.KindSpreadAssignment:
			current = parent
		case ast.KindForOfStatement, ast.KindForInStatement:
			return parent.Initializer() == current
		default:
			return false
		}
	}
	return false
}
