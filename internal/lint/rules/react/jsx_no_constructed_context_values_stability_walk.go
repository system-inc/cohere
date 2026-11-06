package react

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	type_checking "github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// jsxNoConstructedContextValuesStabilityWalk carries the state one provider's analysis shares.
//
// `active` is an in-progress set rather than a visited set: a declaration, callee or memo is marked
// on entry and cleared on exit, so it guards a cycle without making a second, independent encounter
// of the same name answer differently from the first.
type jsxNoConstructedContextValuesStabilityWalk struct {
	ruleContext rule.Context
	active      map[*ast.Node]bool

	// render is the component whose provider is being judged.
	render *ast.Node
}

// isPrimitive reports whether every part of a node's type compares by value, so a fresh computation
// of it is still equal to the last one.
func (walk *jsxNoConstructedContextValuesStabilityWalk) isPrimitive(node *ast.Node) bool {
	subjectType := walk.ruleContext.TypeChecker.GetTypeAtLocation(node)
	if subjectType == nil {
		return false
	}
	const primitive = checker.TypeFlagsStringLike | checker.TypeFlagsNumberLike |
		checker.TypeFlagsBigIntLike | checker.TypeFlagsBooleanLike | checker.TypeFlagsEnumLike |
		checker.TypeFlagsESSymbolLike | checker.TypeFlagsNull | checker.TypeFlagsUndefined |
		checker.TypeFlagsVoid
	for part := range type_checking.UnionTypePartsSequence(subjectType) {
		if !type_checking.IsTypeFlagSet(part, primitive) {
			return false
		}
	}
	return true
}

// memoFinding explains why a memo's result is a new identity on a render, or returns nil.
//
// Both halves are required. The memo has to recompute (a dependency proven unstable, or no
// dependency list at all), and what it recomputes has to be new: `useCallback` always hands back the
// callback it was given, which is a new function on every render, but a `useMemo` factory that
// returns an object that already existed hands back that same object however often it runs. The
// returned `other` is the factory's, for the caller's escape check.
//
// A dependency list has to be an array literal to be judged. A list passed by name is a shape this
// analysis cannot see into, so it is stable by the one-sided rule. The returned finding's
// `dependency` is nil when the list is missing.
func (walk *jsxNoConstructedContextValuesStabilityWalk) memoFinding(
	call *ast.Node,
	scope *ast.Node,
	depth int,
) (*jsxNoConstructedContextValuesInstability, bool) {
	if depth > jsxNoConstructedContextValuesStabilityDepthLimit || walk.active[call] {
		return nil, true
	}
	walk.active[call] = true
	defer delete(walk.active, call)

	arguments := call.AsCallExpression().Arguments
	if arguments == nil || len(arguments.Nodes) == 0 {
		return nil, true
	}
	var finding *jsxNoConstructedContextValuesInstability
	if len(arguments.Nodes) == 1 {
		finding = &jsxNoConstructedContextValuesInstability{origin: call}
	} else {
		list := jsxNoConstructedContextValuesUnwrap(arguments.Nodes[1])
		if list == nil || list.Kind != ast.KindArrayLiteralExpression {
			return nil, true
		}
		for _, element := range list.AsArrayLiteralExpression().Elements.Nodes {
			if found := walk.dependencyFinding(element, scope, depth+1); found != nil {
				finding = found
				break
			}
		}
		if finding == nil {
			return nil, true
		}
	}

	if jsxNoConstructedContextValuesMemoHookName(call) == "useCallback" {
		return finding, false
	}
	factory := jsxNoConstructedContextValuesUnwrap(arguments.Nodes[0])
	if factory == nil || (factory.Kind != ast.KindArrowFunction && factory.Kind != ast.KindFunctionExpression) {
		return nil, true
	}
	result := walk.functionEvaluation(factory, depth+1)
	if result.fresh == nil {
		return nil, true
	}
	return finding, result.other
}

// dependencyFinding explains why one entry of a memo's dependency list is a new identity on a
// render, or returns nil. This is where a render-scoped holder's escape is checked.
func (walk *jsxNoConstructedContextValuesStabilityWalk) dependencyFinding(
	element *ast.Node,
	scope *ast.Node,
	depth int,
) *jsxNoConstructedContextValuesInstability {
	result := walk.evaluate(element, scope, depth)
	if result.fresh == nil {
		return nil
	}
	if result.other && walk.anyEscapes(result.holders, true) {
		return nil
	}
	finding := *result.fresh
	finding.dependency = element
	return &finding
}

// evaluate says what an expression evaluated in `scope` can be: proven new, possibly an existing
// object, or both.
func (walk *jsxNoConstructedContextValuesStabilityWalk) evaluate(
	node *ast.Node,
	scope *ast.Node,
	depth int,
) jsxNoConstructedContextValuesEvaluation {
	unwrapped := jsxNoConstructedContextValuesUnwrap(node)
	if unwrapped == nil {
		return jsxNoConstructedContextValuesEvaluation{}
	}
	if depth > jsxNoConstructedContextValuesStabilityDepthLimit {
		return jsxNoConstructedContextValuesUnknown
	}
	if walk.isPrimitive(unwrapped) {
		return jsxNoConstructedContextValuesEvaluation{}
	}

	if kind := jsxNoConstructedContextValuesConstructionKind(unwrapped); kind != "" {
		return jsxNoConstructedContextValuesEvaluation{fresh: &jsxNoConstructedContextValuesInstability{
			origin: unwrapped,
			reason: fmt.Sprintf("is a new %s built during render", kind),
		}}
	}

	switch unwrapped.Kind {
	case ast.KindConditionalExpression:
		conditional := unwrapped.AsConditionalExpression()
		return walk.evaluate(conditional.WhenTrue, scope, depth+1).
			combine(walk.evaluate(conditional.WhenFalse, scope, depth+1))

	case ast.KindBinaryExpression:
		// A logical operator evaluates to either side. A comma evaluates to its right side, and so
		// does a plain assignment, but an assignment is also a store: into a local it adds a holder,
		// and into anything else (a property, an element, an outer variable) it is an escape, which
		// proves nothing. Every other binary operator produces a primitive, which the type check
		// above has already let through.
		binary := unwrapped.AsBinaryExpression()
		switch binary.OperatorToken.Kind {
		case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken, ast.KindQuestionQuestionToken:
			return walk.evaluate(binary.Left, scope, depth+1).combine(walk.evaluate(binary.Right, scope, depth+1))
		case ast.KindCommaToken:
			return walk.evaluate(binary.Right, scope, depth+1)
		case ast.KindEqualsToken:
			target := jsxNoConstructedContextValuesUnwrap(binary.Left)
			local := jsxNoConstructedContextValuesLocalDeclaration(walk.ruleContext, target, scope)
			if local == nil {
				return jsxNoConstructedContextValuesUnknown
			}
			result := walk.evaluate(binary.Right, scope, depth+1)
			if result.fresh != nil {
				result.holders = append(result.holders, local)
			}
			return result
		}
		return jsxNoConstructedContextValuesUnknown

	case ast.KindCallExpression:
		if name := jsxNoConstructedContextValuesMemoHookName(unwrapped); name != "" {
			inner, other := walk.memoFinding(unwrapped, scope, depth+1)
			if inner == nil {
				return jsxNoConstructedContextValuesUnknown
			}
			reason := fmt.Sprintf("comes from a %s with no dependency list", name)
			if inner.dependency != nil {
				reason = fmt.Sprintf("comes from a %s whose own dependency %q %s",
					name, jsxNoConstructedContextValuesDependencyText(walk.ruleContext, inner.dependency), inner.reason)
			}
			return jsxNoConstructedContextValuesEvaluation{
				fresh: &jsxNoConstructedContextValuesInstability{origin: inner.origin, reason: reason},
				other: other,
			}
		}
		return walk.callEvaluation(unwrapped, depth+1)

	case ast.KindIdentifier:
		declaration := jsxNoConstructedContextValuesRenderConst(walk.ruleContext, unwrapped, scope, walk.render)
		if declaration == nil || walk.active[declaration] {
			return jsxNoConstructedContextValuesUnknown
		}
		if declaration.Kind == ast.KindFunctionDeclaration {
			return jsxNoConstructedContextValuesEvaluation{
				fresh: &jsxNoConstructedContextValuesInstability{
					origin: declaration,
					reason: "is a function declared during render",
				},
				holders: []*ast.Node{declaration},
			}
		}
		walk.active[declaration] = true
		result := walk.evaluate(declaration.AsVariableDeclaration().Initializer, scope, depth+1)
		delete(walk.active, declaration)
		if result.fresh != nil {
			result.holders = append(result.holders, declaration)
		}
		return result
	}
	return jsxNoConstructedContextValuesUnknown
}

// callEvaluation says what a call can return, by reading the body of the function the checker
// resolves it to.
//
// The callee has to be source the program holds: every signature that can be read has a body (a
// function declaration, method, function expression or arrow), and every one that cannot (an
// interface member, a function type, an overload, anything in a declaration file, so every library
// and every `declare function`) is unknown. An async callee or a generator is new outright.
func (walk *jsxNoConstructedContextValuesStabilityWalk) callEvaluation(
	call *ast.Node,
	depth int,
) jsxNoConstructedContextValuesEvaluation {
	signature := walk.ruleContext.TypeChecker.GetResolvedSignature(call)
	if signature == nil {
		return jsxNoConstructedContextValuesUnknown
	}
	callee := signature.Declaration()
	if callee == nil || callee.Body() == nil {
		return jsxNoConstructedContextValuesUnknown
	}

	name := jsxNoConstructedContextValuesCalleeName(call)
	// An async function returns a new promise from every call, and a generator a new iterator,
	// whatever their bodies return. That is the strongest proof this walk has.
	if ast.HasSyntacticModifier(callee, ast.ModifierFlagsAsync) {
		return jsxNoConstructedContextValuesEvaluation{fresh: &jsxNoConstructedContextValuesInstability{
			origin: callee,
			reason: fmt.Sprintf(
				"is the result of `%s(...)`, an async function, which returns a new promise on every call",
				name,
			),
		}}
	}
	if jsxNoConstructedContextValuesIsGenerator(callee) {
		return jsxNoConstructedContextValuesEvaluation{fresh: &jsxNoConstructedContextValuesInstability{
			origin: callee,
			reason: fmt.Sprintf(
				"is the result of `%s(...)`, a generator, which returns a new iterator on every call",
				name,
			),
		}}
	}

	result := walk.functionEvaluation(callee, depth+1)
	if result.fresh != nil {
		result.fresh = &jsxNoConstructedContextValuesInstability{
			origin: result.fresh.origin,
			reason: fmt.Sprintf("is the result of `%s(...)`, which can return a new value on every call", name),
		}
	}
	return result
}

// functionEvaluation says what a function body returns, across every `return` it owns (not those of
// nested functions), or its expression body.
//
// "Some path returns a new value" proves a new identity on that path when no other path can return
// an existing object. When one can, the new value proves nothing if it escapes before it is
// returned: passed to a call, stored into a property, an element or anything outside the function,
// captured by a closure, aliased, or used as a method receiver. That is the caching helper, whose
// first call builds and stores and whose later calls read the stored one back, and it is checked
// here, in the function's own scope, before the holders are dropped for the caller.
func (walk *jsxNoConstructedContextValuesStabilityWalk) functionEvaluation(
	function *ast.Node,
	depth int,
) jsxNoConstructedContextValuesEvaluation {
	if depth > jsxNoConstructedContextValuesStabilityDepthLimit || walk.active[function] {
		return jsxNoConstructedContextValuesUnknown
	}
	body := function.Body()
	if body == nil {
		return jsxNoConstructedContextValuesUnknown
	}
	walk.active[function] = true
	defer delete(walk.active, function)

	var result jsxNoConstructedContextValuesEvaluation
	if body.Kind != ast.KindBlock {
		result = walk.evaluate(body, function, depth+1)
	} else {
		var visit func(node *ast.Node) bool
		visit = func(node *ast.Node) bool {
			if node.Kind == ast.KindReturnStatement {
				result = result.combine(walk.evaluate(node.AsReturnStatement().Expression, function, depth+1))
				return false
			}
			if ast.IsFunctionLike(node) || ast.IsClassLike(node) {
				return false
			}
			return node.ForEachChild(visit)
		}
		body.ForEachChild(visit)
	}

	// Only the holders this function declared are judged here, against this function's body. A holder
	// from an enclosing function (a render `const` a memo factory or a nested helper returns) travels
	// on to the dependency, where it is judged against the body that declared it.
	var own []*ast.Node
	var enclosing []*ast.Node
	for _, holder := range result.holders {
		if jsxNoConstructedContextValuesNearestFunction(holder) == function {
			own = append(own, holder)
		} else {
			enclosing = append(enclosing, holder)
		}
	}
	if result.fresh != nil && result.other && walk.anyEscapes(own, false) {
		result.fresh = nil
	}
	result.holders = enclosing
	return result
}

// anyEscapes reports whether any holder is referenced anywhere in the function that declared it,
// nested functions included, in a way that could let the value outlive the call.
func (walk *jsxNoConstructedContextValuesStabilityWalk) anyEscapes(holders []*ast.Node, render bool) bool {
	for _, holder := range holders {
		scope := jsxNoConstructedContextValuesNearestFunction(holder)
		if scope == nil || scope.Body() == nil {
			return true
		}
		body := scope.Body()
		name := holder.Name()
		if name == nil {
			return true
		}
		symbol := walk.ruleContext.TypeChecker.GetSymbolAtLocation(name)
		if symbol == nil {
			return true
		}
		escaped := false
		var visit func(node *ast.Node) bool
		visit = func(node *ast.Node) bool {
			if escaped {
				return true
			}
			if node.Kind == ast.KindIdentifier && node != name {
				reference := walk.ruleContext.TypeChecker.GetSymbolAtLocation(node)
				if node.Parent != nil && node.Parent.Kind == ast.KindShorthandPropertyAssignment {
					reference = walk.ruleContext.TypeChecker.GetShorthandAssignmentValueSymbol(node.Parent)
				}
				if reference == symbol && !jsxNoConstructedContextValuesStaysHome(node, scope, render) {
					escaped = true
					return true
				}
			}
			return node.ForEachChild(visit)
		}
		body.ForEachChild(visit)
		if escaped {
			return true
		}
	}
	return false
}
