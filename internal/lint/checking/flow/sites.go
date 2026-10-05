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

// Listeners returns the listeners that find every site in a file and hand each to visit.
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
func Listeners(ctx rule.Context, visit func(Site)) rule.Listeners {
	typeChecker := ctx.TypeChecker
	if typeChecker == nil {
		return nil
	}
	offer := func(expression *ast.Node, target *checker.Type) {
		if expression == nil || target == nil {
			return
		}
		source := typeChecker.GetTypeAtLocation(expression)
		if source == nil || source == target {
			return
		}
		visit(Site{Node: expression, Source: source, Target: target, Fresh: isFresh(expression),
			NewContainer: isNewContainer(expression)})
	}
	annotated := func(typeNode *ast.Node, expression *ast.Node) {
		if typeNode == nil || expression == nil {
			return
		}
		offer(expression, checker.Checker_getTypeFromTypeNode(typeChecker, typeNode))
	}
	contextual := func(expression *ast.Node) {
		offer(expression, checker.Checker_getContextualType(typeChecker, expression, checker.ContextFlagsNone))
	}
	returned := func(function *ast.Node, expression *ast.Node) {
		if function == nil || function.Type() == nil {
			return
		}
		if ast.GetFunctionFlags(function)&(ast.FunctionFlagsAsync|ast.FunctionFlagsGenerator) != 0 {
			return
		}
		offer(expression, checker.Checker_getReturnTypeFromAnnotation(typeChecker, function))
	}
	arguments := func(call *ast.Node) {
		for index, argument := range call.Arguments() {
			if argument.Kind == ast.KindSpreadElement {
				continue
			}
			offer(argument, checker.Checker_getContextualTypeForArgumentAtIndex(typeChecker, call, index))
		}
	}
	return rule.Listeners{
		ast.KindVariableDeclaration: func(node *ast.Node) {
			declaration := node.AsVariableDeclaration()
			annotated(declaration.Type, declaration.Initializer)
		},
		ast.KindPropertyDeclaration: func(node *ast.Node) {
			declaration := node.AsPropertyDeclaration()
			annotated(declaration.Type, declaration.Initializer)
		},
		ast.KindParameter: func(node *ast.Node) {
			parameter := node.AsParameterDeclaration()
			annotated(parameter.Type, parameter.Initializer)
		},
		ast.KindBinaryExpression: func(node *ast.Node) {
			binary := node.AsBinaryExpression()
			if binary.OperatorToken == nil || binary.OperatorToken.Kind != ast.KindEqualsToken || binary.Left == nil {
				return
			}
			left := ast.SkipParentheses(binary.Left)
			// A literal on the left is a destructuring pattern, whose parts are bindings, not slots.
			if left.Kind == ast.KindObjectLiteralExpression || left.Kind == ast.KindArrayLiteralExpression {
				return
			}
			offer(binary.Right, typeChecker.GetTypeAtLocation(binary.Left))
		},
		ast.KindCallExpression: arguments,
		ast.KindNewExpression: func(node *ast.Node) {
			if node.AsNewExpression().Arguments == nil {
				return
			}
			arguments(node)
		},
		ast.KindReturnStatement: func(node *ast.Node) {
			expression := node.AsReturnStatement().Expression
			if expression == nil {
				return
			}
			returned(type_checking.GetParentFunctionNode(node), expression)
		},
		ast.KindArrowFunction: func(node *ast.Node) {
			body := node.AsArrowFunction().Body
			if body == nil || ast.IsBlock(body) {
				return
			}
			returned(node, body)
		},
		ast.KindPropertyAssignment: func(node *ast.Node) {
			if IsDestructuringTarget(node.Parent) {
				return
			}
			contextual(node.AsPropertyAssignment().Initializer)
		},
		ast.KindShorthandPropertyAssignment: func(node *ast.Node) {
			if IsDestructuringTarget(node.Parent) || node.AsShorthandPropertyAssignment().ObjectAssignmentInitializer != nil {
				return
			}
			name := node.Name()
			if name == nil {
				return
			}
			// The key and the value are one identifier: its contextual type is the property's slot and
			// its type is the value put there, which no-unsafe-assignment reads the same way.
			offer(name, checker.Checker_getContextualType(typeChecker, name, checker.ContextFlagsNone))
		},
		ast.KindArrayLiteralExpression: func(node *ast.Node) {
			if IsDestructuringTarget(node) {
				return
			}
			for _, element := range node.AsArrayLiteralExpression().Elements.Nodes {
				if element.Kind == ast.KindSpreadElement || element.Kind == ast.KindOmittedExpression {
					continue
				}
				contextual(element)
			}
		},
		ast.KindAsExpression:            func(node *ast.Node) { offerUpcast(typeChecker, node, offer) },
		ast.KindTypeAssertionExpression: func(node *ast.Node) { offerUpcast(typeChecker, node, offer) },
	}
}

// offerUpcast hands over a cast that only widens. A downcast is no flow at all, since the value is not
// being put anywhere wider, and adamic/no-unchecked-cast judges it. `as const` names no slot.
func offerUpcast(typeChecker *checker.Checker, node *ast.Node, offer func(*ast.Node, *checker.Type)) {
	typeNode := node.Type()
	if typeNode == nil || ast.IsConstTypeReference(typeNode) {
		return
	}
	expression := node.Expression()
	target := checker.Checker_getTypeFromTypeNode(typeChecker, typeNode)
	source := typeChecker.GetTypeAtLocation(expression)
	if target == nil || source == nil || !checker.Checker_isTypeAssignableTo(typeChecker, source, target) {
		return
	}
	offer(expression, target)
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
