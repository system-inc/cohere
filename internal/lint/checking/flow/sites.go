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
	"slices"

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
//	[a, { b }] = e            each target of the pattern, with the part of e it receives
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
		// A literal on the left is a destructuring pattern: each target in it is a slot of its own.
		if left.Kind == ast.KindObjectLiteralExpression || left.Kind == ast.KindArrayLiteralExpression {
			w.destructured(left, binary.Right, w.typeChecker.GetTypeAtLocation(binary.Right))
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
	if expression == nil || target == nil || !w.hasObjectPart(target) {
		return
	}
	source := w.typeChecker.GetTypeAtLocation(expression)
	if source == nil || source == target {
		return
	}
	w.sites = append(w.sites, Site{Node: expression, Source: source, Target: target, Fresh: isFresh(expression),
		NewContainer: isNewContainer(expression)})
}

// offerPart adds the site of a part of node's value flowing into target, when the part has no expression of its
// own: `[animals] = pair` puts pair's first element into animals, reported at pair.
func (w *Walker) offerPart(node *ast.Node, source *checker.Type, target *checker.Type) {
	if node == nil || source == nil || target == nil || source == target || !w.hasObjectPart(target) {
		return
	}
	w.sites = append(w.sites, Site{Node: node, Source: source, Target: target})
}

/*
 * destructured offers each target of an assignment pattern with the part of the value it receives (#gvzdft9):
 * `[all] = pair` puts pair's first element into all as surely as `all = pair[0]` does, and pushing a cat through
 * all then fills the dogs pair held (a TypeError in Node). An element pairs by position and a property by name,
 * read off the value's type, and the site is reported at the value.
 *
 * Only a value that is no literal. A literal's own elements and properties are contextually typed by the pattern,
 * so the ArrayLiteralExpression and PropertyAssignment finders already offer each of them, with everything below
 * it and past a spread too: `[all] = [dogs]` was always reported. A rest element or spread takes an array or
 * object of what is left, no one part, and is not paired. A default (`[a = d] = ...`) is an assignment of its
 * own, which the BinaryExpression finder offers.
 */
func (w *Walker) destructured(pattern *ast.Node, value *ast.Node, valueType *checker.Type) {
	if kind := ast.SkipParentheses(value).Kind; kind == ast.KindArrayLiteralExpression || kind == ast.KindObjectLiteralExpression {
		return
	}
	w.destructuredParts(pattern, value, valueType)
}

// destructuredParts offers the targets of pattern with the parts of valueType, reported at value.
func (w *Walker) destructuredParts(pattern *ast.Node, value *ast.Node, valueType *checker.Type) {
	pattern = ast.SkipParentheses(pattern)
	switch pattern.Kind {
	case ast.KindArrayLiteralExpression:
		for index, element := range pattern.AsArrayLiteralExpression().Elements.Nodes {
			if element.Kind == ast.KindSpreadElement {
				return
			}
			if element.Kind != ast.KindOmittedExpression {
				w.destructuredTarget(element, value, w.destructuredElement(valueType, index))
			}
		}
	case ast.KindObjectLiteralExpression:
		for _, property := range pattern.AsObjectLiteralExpression().Properties.Nodes {
			var name, target *ast.Node
			switch property.Kind {
			case ast.KindPropertyAssignment:
				name, target = property.Name(), property.AsPropertyAssignment().Initializer
			case ast.KindShorthandPropertyAssignment:
				name, target = property.Name(), property.Name()
			default:
				// A spread assignment takes the rest, no one property.
				continue
			}
			if name == nil || name.Kind != ast.KindIdentifier && name.Kind != ast.KindStringLiteral {
				continue
			}
			w.destructuredTarget(target, value, w.destructuredProperty(valueType, name.Text()))
		}
	}
}

// destructuredTarget offers one target of a pattern: a nested pattern is destructured again with its part's
// type, and anything else is a slot the part is put into.
func (w *Walker) destructuredTarget(target *ast.Node, value *ast.Node, partType *checker.Type) {
	target = ast.SkipParentheses(target)
	if target.Kind == ast.KindBinaryExpression && target.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken {
		target = ast.SkipParentheses(target.AsBinaryExpression().Left)
	}
	if target.Kind == ast.KindArrayLiteralExpression || target.Kind == ast.KindObjectLiteralExpression {
		w.destructuredParts(target, value, partType)
		return
	}
	w.offerPart(value, partType, w.typeChecker.GetTypeAtLocation(target))
}

// destructuredElement is the element at index of an array or tuple value's type.
func (w *Walker) destructuredElement(valueType *checker.Type, index int) *checker.Type {
	switch {
	case valueType == nil:
		return nil
	case checker.IsTupleType(valueType):
		return w.typeArgument(valueType, index)
	case checker.Checker_isArrayType(w.typeChecker, valueType):
		return w.typeArgument(valueType, 0)
	}
	return nil
}

// destructuredProperty is the type of the property name of an object value's type.
func (w *Walker) destructuredProperty(valueType *checker.Type, name string) *checker.Type {
	if valueType == nil || valueType.Flags()&checker.TypeFlagsObject == 0 {
		return nil
	}
	property := checker.Checker_getPropertyOfType(w.typeChecker, valueType, name)
	if property == nil {
		return nil
	}
	return checker.Checker_getTypeOfSymbol(w.typeChecker, property)
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
	if target == nil || !w.hasObjectPart(target) {
		return
	}
	source := w.typeChecker.GetTypeAtLocation(expression)
	if source == nil || !w.IsAssignable(source, target) {
		return
	}
	w.offer(expression, target)
}

// hasObjectPart is a type the walk can relate parts of: an object type, a union with one among its
// members, an intersection with an array, tuple or container member, or a type parameter whose constraint
// has one (#53w68gt). A union's members are never
// unions, and a type parameter's base constraint is never a type parameter, so this goes at most two deep.
func (w *Walker) hasObjectPart(t *checker.Type) bool {
	switch {
	case t.Flags()&checker.TypeFlagsObject != 0:
		return true
	case t.Flags()&checker.TypeFlagsUnion != 0:
		return slices.ContainsFunc(t.Types(), w.hasObjectPart)
	case t.Flags()&checker.TypeFlagsIntersection != 0:
		// Only the members a walk pairs; see relate. An object member counts unless a primitive one makes it
		// a brand (#b9a0wgy).
		return slices.ContainsFunc(t.Types(), w.isSlotContainer) ||
			(!hasPrimitiveMember(t) && slices.ContainsFunc(t.Types(), func(member *checker.Type) bool {
				return member.Flags()&checker.TypeFlagsObject != 0
			}))
	case t.Flags()&checker.TypeFlagsTypeParameter != 0:
		constraint := checker.Checker_getBaseConstraintOfType(w.typeChecker, t)
		return constraint != nil && constraint != t && w.hasObjectPart(constraint)
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
// or nested inside another literal that is. Its properties and elements are targets a value is put into, not
// values of their own, so destructured offers them rather than the literal finders.
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
