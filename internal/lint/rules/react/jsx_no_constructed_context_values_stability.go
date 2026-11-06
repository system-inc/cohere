package react

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// This file is the half of `react/jsx-no-constructed-context-values` that upstream does not have:
// a memoized provider value is stable only if every input to the memo is stable.
//
// Upstream stops at the wrapper. It sees `useMemo` and is satisfied, so
//
//	const theme = mergeTheme(Base, override);              // a new object every render
//	const value = useMemo(() => ({ open, theme }), [open, theme]);
//	return <Ctx.Provider value={value}>...
//
// is clean upstream while the memo recomputes on every render and every consumer re-renders exactly
// as if the wrapper were not there. That is the shape the review found at DialogRoot, DrawerRoot,
// TableRoot and Field: wrapping the value, which is upstream's own remedy, would have done nothing.
//
// The judgment is one-sided on purpose. A dependency is reported only when the code proves it can be
// a new identity on a render: it is built by an object, array, function, class, `new`, JSX or
// regular-expression expression evaluated during render, or it is the result of a call whose body
// returns such a construction on some path (or of any async function or generator, which return a new
// promise or iterator every time), or it is another memo that is itself unstable. Anything the
// analysis cannot see into is treated as stable: parameters, props, imports, module-scope constants,
// destructured hook results, `let` bindings (which can be reassigned), property paths, and calls into
// declarations with no body. Primitive-typed dependencies compare by value, so they are stable however
// they were computed.
//
// Kirk's ruling, 2026-10-01, on the cohere rule review: "go for C right out of the gate". Recorded as
// a deliberate divergence in `jsx_no_constructed_context_values.md`.

// jsxNoConstructedContextValuesStabilityDepthLimit bounds the walk through identifiers, memos and
// callee bodies. Real chains measured in ahra are at most three hops; the limit exists for cycles
// the visited set does not catch, such as a recursive function reached through two names.
const jsxNoConstructedContextValuesStabilityDepthLimit = 8

// jsxNoConstructedContextValuesInstability is why a memo's dependency changes every render.
type jsxNoConstructedContextValuesInstability struct {
	// dependency is the entry in the provider memo's dependency list, and where the finding points.
	dependency *ast.Node

	// origin is where the new identity is made: the construction, or the `return` in a callee.
	origin *ast.Node

	// reason completes the sentence "the `x` dependency ...", rendered without the line, which
	// the message appends from origin.
	reason string
}

// jsxNoConstructedContextValuesMemoHookName names the memo hook a call invokes, or "".
//
// Name-based, matching how the React rules in this package identify hooks: the bare name or the
// `React.` qualified one. A project defining its own unrelated `useMemo` is not a shape that exists
// in this tree.
func jsxNoConstructedContextValuesMemoHookName(call *ast.Node) string {
	if call == nil || call.Kind != ast.KindCallExpression {
		return ""
	}
	callee := ast.SkipParentheses(call.AsCallExpression().Expression)
	name := ""
	switch callee.Kind {
	case ast.KindIdentifier:
		name = callee.Text()
	case ast.KindPropertyAccessExpression:
		access := callee.AsPropertyAccessExpression()
		receiver := ast.SkipParentheses(access.Expression)
		if receiver.Kind != ast.KindIdentifier || receiver.Text() != "React" {
			return ""
		}
		if access.Name() == nil {
			return ""
		}
		name = access.Name().Text()
	}
	if name == "useMemo" || name == "useCallback" {
		return name
	}
	return ""
}

// jsxNoConstructedContextValuesUnwrap strips the wrappers that do not change identity: parentheses,
// `as`, `satisfies` and a non-null assertion.
func jsxNoConstructedContextValuesUnwrap(node *ast.Node) *ast.Node {
	unwrapped := node
	for unwrapped != nil {
		switch unwrapped.Kind {
		case ast.KindParenthesizedExpression:
			unwrapped = unwrapped.AsParenthesizedExpression().Expression
		case ast.KindAsExpression:
			unwrapped = unwrapped.AsAsExpression().Expression
		case ast.KindSatisfiesExpression:
			unwrapped = unwrapped.AsSatisfiesExpression().Expression
		case ast.KindNonNullExpression:
			unwrapped = unwrapped.AsNonNullExpression().Expression
		default:
			return unwrapped
		}
	}
	return nil
}

// jsxNoConstructedContextValuesNearestFunction is the closest function-like ancestor of a node.
func jsxNoConstructedContextValuesNearestFunction(node *ast.Node) *ast.Node {
	for current := node.Parent; current != nil; current = current.Parent {
		if ast.IsFunctionLike(current) {
			return current
		}
	}
	return nil
}

// jsxNoConstructedContextValuesRenderConst resolves an identifier to the `const` declaration it names
// when that declaration runs on every call of `scope`: declared directly in that function rather
// than in a nested one, bound to a plain name rather than a pattern.
//
// Only `const`, because a `let` can be reassigned to something stable after its initializer, and
// following the initializer alone would report a value the code later replaced.
func jsxNoConstructedContextValuesRenderConst(
	ruleContext rule.Context,
	identifier *ast.Node,
	scope *ast.Node,
	render *ast.Node,
) *ast.Node {
	symbol := ruleContext.TypeChecker.GetSymbolAtLocation(identifier)
	if symbol == nil || len(symbol.Declarations) == 0 {
		return nil
	}
	// The first declaration. A `const` cannot merge with anything, and a function declared with
	// overloads in a render body is a function declared during render whichever signature answers.
	declaration := symbol.Declarations[0]
	if !jsxNoConstructedContextValuesRunsWith(
		jsxNoConstructedContextValuesNearestFunction(declaration),
		scope,
		render,
	) {
		return nil
	}
	switch declaration.Kind {
	case ast.KindFunctionDeclaration:
		return declaration
	case ast.KindVariableDeclaration:
		// A destructured name declares a binding element rather than a variable declaration, so
		// it never reaches this arm: what a pattern pulls out of a value is not that value.
		if !ast.IsVarConst(declaration) {
			return nil
		}
		return declaration
	}
	return nil
}

// jsxNoConstructedContextValuesRunsWith reports whether a declaration made in `owner` is evaluated
// afresh each time `scope` runs, for the component `render`.
//
// `owner` being `scope` itself always qualifies. Beyond that, only a function nested inside the
// render body runs per render, so a memo factory or a helper declared in the component sees the
// component's own declarations as per-render values. Nothing above `render` qualifies: a component
// declared inside another function re-renders on its own state while the outer declarations keep
// their identity. And a helper outside the render (a module-level function, or one returned by a
// factory) sees only its own body, because its enclosing function ran once, not once per call.
func jsxNoConstructedContextValuesRunsWith(owner *ast.Node, scope *ast.Node, render *ast.Node) bool {
	if owner == nil {
		return false
	}
	if owner == scope {
		return true
	}
	var chain []*ast.Node
	for current := scope; current != nil; current = jsxNoConstructedContextValuesNearestFunction(current) {
		chain = append(chain, current)
		if current == render {
			break
		}
	}
	if chain[len(chain)-1] != render {
		return false
	}
	for _, function := range chain {
		if function == owner {
			return true
		}
	}
	return false
}

// jsxNoConstructedContextValuesMemoOf finds the memo call behind a provider's value expression: the
// call written inline, or the call a render-scoped `const` was initialized with.
func jsxNoConstructedContextValuesMemoOf(ruleContext rule.Context, value *ast.Node, scope *ast.Node) *ast.Node {
	unwrapped := jsxNoConstructedContextValuesUnwrap(value)
	if unwrapped == nil {
		return nil
	}
	if jsxNoConstructedContextValuesMemoHookName(unwrapped) != "" {
		return unwrapped
	}
	if unwrapped.Kind != ast.KindIdentifier {
		return nil
	}
	declaration := jsxNoConstructedContextValuesRenderConst(ruleContext, unwrapped, scope, scope)
	if declaration == nil || declaration.Kind != ast.KindVariableDeclaration {
		return nil
	}
	initializer := jsxNoConstructedContextValuesUnwrap(declaration.AsVariableDeclaration().Initializer)
	if jsxNoConstructedContextValuesMemoHookName(initializer) == "" {
		return nil
	}
	return initializer
}

// jsxNoConstructedContextValuesConstructionKind names a node that evaluates to a new identity every
// time it runs, in upstream's own vocabulary, or returns "".
func jsxNoConstructedContextValuesConstructionKind(node *ast.Node) string {
	switch node.Kind {
	case ast.KindObjectLiteralExpression:
		return "object"
	case ast.KindArrayLiteralExpression:
		return "array"
	case ast.KindArrowFunction, ast.KindFunctionExpression:
		return "function expression"
	case ast.KindClassExpression:
		return "class expression"
	case ast.KindNewExpression:
		return "new expression"
	case ast.KindRegularExpressionLiteral:
		return "regular expression"
	case ast.KindJsxElement, ast.KindJsxSelfClosingElement:
		return "JSX element"
	case ast.KindJsxFragment:
		return "JSX fragment"
	}
	return ""
}

// jsxNoConstructedContextValuesUnknown is an evaluation the walk cannot see into.
var jsxNoConstructedContextValuesUnknown = jsxNoConstructedContextValuesEvaluation{other: true}

// jsxNoConstructedContextValuesLocalDeclaration resolves an identifier to a variable declared
// directly in `scope`, of any kind, or nil. It is what an assignment target has to be for the
// assignment to be a holder rather than an escape.
func jsxNoConstructedContextValuesLocalDeclaration(
	ruleContext rule.Context,
	target *ast.Node,
	scope *ast.Node,
) *ast.Node {
	if target == nil || target.Kind != ast.KindIdentifier {
		return nil
	}
	symbol := ruleContext.TypeChecker.GetSymbolAtLocation(target)
	if symbol == nil || len(symbol.Declarations) == 0 {
		return nil
	}
	declaration := symbol.Declarations[0]
	if declaration.Kind != ast.KindVariableDeclaration ||
		jsxNoConstructedContextValuesNearestFunction(declaration) != scope {
		return nil
	}
	return declaration
}

// jsxNoConstructedContextValuesStaysHome reports whether one reference to a holder leaves the value
// where it was: returned by its own function, read through (a field read or write, not a method
// call), compared, tested, or reassigned. In a render scope two more places are home: the dependency
// list of a memo, and the object a memo factory returns, which becomes the provider value itself.
// Everything else is treated as an escape, including a reference this list never thought of.
func jsxNoConstructedContextValuesStaysHome(reference *ast.Node, scope *ast.Node, render bool) bool {
	expression := reference
	for expression.Parent != nil {
		switch expression.Parent.Kind {
		case ast.KindParenthesizedExpression, ast.KindAsExpression, ast.KindSatisfiesExpression,
			ast.KindNonNullExpression:
			expression = expression.Parent
			continue
		}
		break
	}
	parent := expression.Parent
	if parent == nil {
		return false
	}
	ownFunction := func(function *ast.Node) bool {
		return function == scope || (render && jsxNoConstructedContextValuesIsMemoFactory(function))
	}

	switch parent.Kind {
	case ast.KindReturnStatement:
		return ownFunction(jsxNoConstructedContextValuesNearestFunction(expression))
	case ast.KindArrowFunction:
		return parent.Body() == expression && ownFunction(parent)
	case ast.KindPropertyAccessExpression:
		return parent.AsPropertyAccessExpression().Expression == expression &&
			!jsxNoConstructedContextValuesIsCallee(parent)
	case ast.KindElementAccessExpression:
		return parent.AsElementAccessExpression().Expression == expression &&
			!jsxNoConstructedContextValuesIsCallee(parent)
	case ast.KindBinaryExpression:
		binary := parent.AsBinaryExpression()
		switch binary.OperatorToken.Kind {
		case ast.KindEqualsToken:
			return binary.Left == expression
		case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken,
			ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken, ast.KindInstanceOfKeyword:
			return true
		}
		return false
	case ast.KindPrefixUnaryExpression:
		return parent.AsPrefixUnaryExpression().Operator == ast.KindExclamationToken
	case ast.KindTypeOfExpression:
		return true
	case ast.KindIfStatement:
		return parent.AsIfStatement().Expression == expression
	case ast.KindArrayLiteralExpression:
		return render && jsxNoConstructedContextValuesIsDependencyList(parent)
	case ast.KindShorthandPropertyAssignment:
		return render && jsxNoConstructedContextValuesIsMemoResult(parent.Parent)
	case ast.KindPropertyAssignment:
		return render && parent.AsPropertyAssignment().Initializer == expression &&
			jsxNoConstructedContextValuesIsMemoResult(parent.Parent)
	}
	return false
}

// jsxNoConstructedContextValuesIsCallee reports whether an access is the callee of a call, which
// hands the object to the method as its receiver.
func jsxNoConstructedContextValuesIsCallee(access *ast.Node) bool {
	parent := access.Parent
	return parent != nil && parent.Kind == ast.KindCallExpression && parent.AsCallExpression().Expression == access
}

// jsxNoConstructedContextValuesIsMemoFactory reports whether a function is the factory of a memo.
func jsxNoConstructedContextValuesIsMemoFactory(function *ast.Node) bool {
	if function == nil || function.Parent == nil || jsxNoConstructedContextValuesMemoHookName(function.Parent) == "" {
		return false
	}
	arguments := function.Parent.AsCallExpression().Arguments
	return arguments != nil && len(arguments.Nodes) > 0 && arguments.Nodes[0] == function
}

// jsxNoConstructedContextValuesIsDependencyList reports whether an array literal is a memo's list.
func jsxNoConstructedContextValuesIsDependencyList(list *ast.Node) bool {
	if list.Parent == nil || jsxNoConstructedContextValuesMemoHookName(list.Parent) == "" {
		return false
	}
	arguments := list.Parent.AsCallExpression().Arguments
	return len(arguments.Nodes) > 1 && arguments.Nodes[1] == list
}

// jsxNoConstructedContextValuesIsMemoResult reports whether an object literal is what a memo factory
// returns, as its expression body or as the operand of one of its own returns.
func jsxNoConstructedContextValuesIsMemoResult(object *ast.Node) bool {
	expression := object
	for expression.Parent != nil && expression.Parent.Kind == ast.KindParenthesizedExpression {
		expression = expression.Parent
	}
	parent := expression.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindArrowFunction:
		return parent.Body() == expression && jsxNoConstructedContextValuesIsMemoFactory(parent)
	case ast.KindReturnStatement:
		return jsxNoConstructedContextValuesIsMemoFactory(jsxNoConstructedContextValuesNearestFunction(parent))
	}
	return false
}

// jsxNoConstructedContextValuesIsGenerator reports whether a function-like node carries `*`.
func jsxNoConstructedContextValuesIsGenerator(function *ast.Node) bool {
	switch function.Kind {
	case ast.KindFunctionDeclaration:
		return function.AsFunctionDeclaration().AsteriskToken != nil
	case ast.KindFunctionExpression:
		return function.AsFunctionExpression().AsteriskToken != nil
	case ast.KindMethodDeclaration:
		return function.AsMethodDeclaration().AsteriskToken != nil
	}
	return false
}

// jsxNoConstructedContextValuesCalleeName is the callee as the reader wrote it, for the message.
func jsxNoConstructedContextValuesCalleeName(call *ast.Node) string {
	callee := jsxNoConstructedContextValuesUnwrap(call.AsCallExpression().Expression)
	switch callee.Kind {
	case ast.KindIdentifier:
		return callee.Text()
	case ast.KindPropertyAccessExpression:
		if name := callee.AsPropertyAccessExpression().Name(); name != nil {
			return name.Text()
		}
	}
	return "the call"
}

// jsxNoConstructedContextValuesDependencyText is a dependency's source text, trimmed of trivia.
func jsxNoConstructedContextValuesDependencyText(ruleContext rule.Context, dependency *ast.Node) string {
	span := rule.TokenRange(ruleContext.SourceFile, dependency)
	return ruleContext.SourceFile.Text()[span.Pos():span.End()]
}

// jsxNoConstructedContextValuesCheckMemo reports a provider value that is memoized over an input
// that changes every render, or memoized with no dependency list at all.
func jsxNoConstructedContextValuesCheckMemo(ruleContext rule.Context, value *ast.Node, provider *ast.Node) {
	// The caller has already established the provider sits in a component, so there is a function.
	scope := jsxNoConstructedContextValuesNearestFunction(provider)
	memo := jsxNoConstructedContextValuesMemoOf(ruleContext, value, scope)
	if memo == nil {
		return
	}
	hook := jsxNoConstructedContextValuesMemoHookName(memo)
	lineOf := func(node *ast.Node) int {
		line, _ := scanner.GetLineAndCharacterOfPosition(
			ruleContext.SourceFile,
			rule.TokenRange(ruleContext.SourceFile, node).Pos(),
		)
		return line + 1
	}

	walk := &jsxNoConstructedContextValuesStabilityWalk{ctx: ruleContext, active: map[*ast.Node]bool{}, render: scope}
	found, _ := walk.memoFinding(memo, scope, 0)
	if found == nil {
		return
	}
	if found.dependency == nil {
		ruleContext.ReportNode(memo.AsCallExpression().Expression, rule.Message{
			Id: "memoWithoutDependenciesMsg",
			Description: fmt.Sprintf(
				"The %s that builds the Context provider's value (at line %d) has no dependency list, "+
					"so it recomputes on every render and every consumer of this context re-renders on "+
					"every render of this component, exactly as if it were not memoized. Pass the "+
					"dependency list.",
				hook, lineOf(memo),
			),
		})
		return
	}

	dependencyText := jsxNoConstructedContextValuesDependencyText(ruleContext, found.dependency)
	location := ""
	if ast.GetSourceFileOfNode(found.origin) == ruleContext.SourceFile {
		location = fmt.Sprintf(" (at line %d)", lineOf(found.origin))
	}
	ruleContext.ReportNode(found.dependency, rule.Message{
		Id: "unstableDependencyMsg",
		Description: fmt.Sprintf(
			"The %q dependency of the %s that builds the Context provider's value (at line %d) %s%s, "+
				"so the %s recomputes on every render and every consumer of this context re-renders "+
				"on every render of this component, exactly as if it were not memoized. A memoized "+
				"value is only stable when every input is: make %q stable too, by memoizing it or by "+
				"building it outside the component.",
			dependencyText, hook, lineOf(memo), found.reason, location, hook, dependencyText,
		),
	})
}
