package core

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// RequireAwait flags an `async` function that never awaits anything.
//
//	valid:   async function f() { await g(); }
//	valid:   async function f() { for await (const x of y) {} }
//	valid:   async function f() {}                 // an empty body is a stub, not a mistake
//	valid:   async function* f() { doSomething(); } // a generator is exempt entirely
//	invalid: async function f() { doSomething(); }
//
// # What the marker costs when it is not doing anything
//
// `async` changes a function's contract: it wraps the return value in a promise and makes every
// caller either await it or handle a floating promise. A function that never awaits pays that cost
// and buys nothing, and a reader seeing `async` reasonably assumes there is asynchrony inside to
// find. Removing the keyword is offered as a SUGGESTION rather than applied, because it changes the
// function's return type from `Promise<T>` to `T` and every caller has to agree.
//
// # Three things count as awaiting, and missing any one reports a legitimate function
//
//	await x                  the obvious one
//	for await (const x of y) an await hidden in a loop header, which a search for
//	                         KindAwaitExpression alone never sees
//	await using x = y()      an await hidden in a declaration's flags rather than in any
//	                         expression node at all
//
// That last one is the sharpest and it carries a trap worth stating: `NodeFlagsAwaitUsing` is
// `Const|Using` rather than a bit of its own, so `flags&NodeFlagsAwaitUsing != 0` is TRUE for an
// ordinary `const`. Measured: a plain `const x` in a `for...of` header answers true to the naive
// test. The correct discrimination is the mask idiom typescript-go itself uses at
// `ast/utilities.go:1214`, comparing the block-scoped bits for equality.
//
// # Two exclusions, both deliberate upstream
//
// An EMPTY body is never reported. Upstream declines it on the reasoning that an empty async
// function is a stub or an interface implementation rather than an oversight, and reporting it
// would fire on every unimplemented method in a codebase.
//
// A GENERATOR is never reported, whatever its body. An async generator's `async` governs its
// iteration protocol rather than its return value, so the keyword is doing work even with no
// `await` inside.
//
// # Nested functions do not count for their parent
//
// An `await` inside an inner function belongs to that function. Upstream keeps a stack and pushes a
// fresh frame per function, so the inner await sets the inner frame's flag and the outer one stays
// unset. Here the search stops at every nested function boundary, which is the same rule stated as
// a walk. Measured on upstream's own case: `async function foo() { async () => { await x; } }`
// reports the OUTER function, and the inner arrow is separately clean because it does await.
var RequireAwait = rule.Rule{
	Name: "require-await",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		check := func(node *ast.Node) {
			checkRequireAwait(ctx, node)
		}
		return rule.Listeners{
			ast.KindFunctionDeclaration: check,
			ast.KindFunctionExpression:  check,
			ast.KindArrowFunction:       check,
			// A method is a function expression under a MethodDefinition upstream, so its
			// listeners cover it through `FunctionExpression`. Our parser folds the two into one
			// node, so the method kinds are listened for directly. Accessors are included because
			// an accessor can carry `async` here and upstream reaches them the same way.
			ast.KindMethodDeclaration: check,
			ast.KindGetAccessor:       check,
			ast.KindSetAccessor:       check,
			ast.KindConstructor:       check,
		}
	},
}

// checkRequireAwait judges one function-like node.
func checkRequireAwait(ctx rule.Context, node *ast.Node) {
	if node.ModifierFlags()&ast.ModifierFlagsAsync == 0 {
		return
	}
	if requireAwaitIsGenerator(node) {
		return
	}
	body := node.Body()
	if body == nil {
		// An overload signature or a declaration with no body. Nothing to search and nothing to
		// report; upstream never sees these because ESTree gives them no function node.
		return
	}
	if requireAwaitBodyIsEmpty(body) {
		return
	}
	if requireAwaitContainsAwait(body) {
		return
	}

	head := requireAwaitHeadRange(ctx, node)
	asyncRange, found := requireAwaitAsyncKeywordRange(ctx, node)
	if !found {
		// The `async` keyword could not be located, which should not happen for a node whose
		// modifier flag is set. Reported without a suggestion rather than skipped: the finding is
		// still true, and offering a repair whose span was not found would be worse.
		ctx.ReportRange(head, requireAwaitMessage(ctx, node))
		return
	}

	ctx.ReportRangeWithSuggestions(head, requireAwaitMessage(ctx, node), rule.Suggestion{
		Message: rule.Message{
			Id: "removeAsync",
			Description: "Remove `async`. This changes the function's return type from a " +
				"promise to the value itself, so every caller has to stop awaiting it, which " +
				"is why it is offered rather than applied.",
		},
		Fixes: []rule.Fix{rule.ReplaceRange(asyncRange, requireAwaitReplacement(ctx, node))},
	})
}

// requireAwaitMessage renders the finding, naming the function the way upstream names it.
func requireAwaitMessage(ctx rule.Context, node *ast.Node) rule.Message {
	return rule.Message{
		Id: "missingAwait",
		Description: fmt.Sprintf(
			"%s has no `await` expression. The `async` keyword wraps the return value in a "+
				"promise and obliges every caller to await it or handle a floating promise, so "+
				"a function that never awaits pays that cost for nothing and tells the reader "+
				"there is asynchrony here to find. Remove `async`, or await what this was "+
				"waiting for.",
			requireAwaitDescribe(ctx, node)),
	}
}

// requireAwaitIsGenerator answers whether a function-like node is a generator.
//
// The asterisk is a field on each concrete type rather than a method on `Node`, so the kinds are
// enumerated. An arrow function cannot be a generator at all.
func requireAwaitIsGenerator(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration:
		return node.AsFunctionDeclaration().AsteriskToken != nil
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().AsteriskToken != nil
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().AsteriskToken != nil
	}
	return false
}

// requireAwaitBodyIsEmpty answers whether a function body contains no statements.
//
// Upstream's `isEmptyFunction`, which tests for a block with an empty body. A concise arrow body is
// an expression rather than a block and is never empty, so it always answers false. A block holding
// only an empty statement is NOT empty -- measured, `async function f() { ; }` reports.
func requireAwaitBodyIsEmpty(body *ast.Node) bool {
	if body.Kind != ast.KindBlock {
		return false
	}
	statements := body.AsBlock().Statements
	return statements == nil || len(statements.Nodes) == 0
}

// requireAwaitContainsAwait searches a function body for anything that awaits.
//
// The search stops at every nested function boundary, which is what makes an inner function's await
// belong to the inner function. Upstream reaches the same place with a stack of frames pushed per
// function; this is the same rule stated as a walk, and it is the piece a naive descendant search
// gets backwards.
func requireAwaitContainsAwait(node *ast.Node) bool {
	found := false
	var walk func(current *ast.Node)
	walk = func(current *ast.Node) {
		if current == nil || found {
			return
		}
		switch current.Kind {
		case ast.KindAwaitExpression:
			found = true
			return
		case ast.KindForOfStatement:
			if current.AsForInOrOfStatement().AwaitModifier != nil {
				found = true
				return
			}
		case ast.KindVariableDeclarationList:
			if requireAwaitIsAwaitUsing(current) {
				found = true
				return
			}
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
			ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor,
			ast.KindConstructor, ast.KindClassDeclaration, ast.KindClassExpression:
			// A nested function owns its own awaits. Classes are included because their members
			// are functions of their own, and a class body cannot contain a bare await belonging
			// to the enclosing function.
			return
		}
		current.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return found
		})
	}
	// The body itself is walked rather than passed to the switch, so a body that IS one of the
	// stopping kinds -- which it never is, being a block or an expression -- could not end the
	// search before it starts.
	node.ForEachChild(func(child *ast.Node) bool {
		walk(child)
		return found
	})
	if !found && node.Kind != ast.KindBlock {
		// A concise arrow body is a single expression rather than a container, so it has to be
		// examined itself and not only its children: `async () => await x` awaits at the top.
		walk(node)
	}
	return found
}

// requireAwaitIsAwaitUsing answers whether a declaration list is `await using`.
//
// # The flag is a composite and the obvious test is wrong
//
// `NodeFlagsAwaitUsing` is defined as `NodeFlagsConst | NodeFlagsUsing`, not as a bit of its own,
// and typescript-go's own definition carries the note that on a single node those flags would
// otherwise be mutually exclusive. So `flags&NodeFlagsAwaitUsing != 0` is true for ANY const and
// for any using, which measured true on a plain `const` in a for-of header.
//
// The correct test compares the block-scoped bits for equality, which is the idiom typescript-go
// uses at `ast/utilities.go:1214`. Measured across all five declaration kinds, with the raw flag
// value recorded so the next reader can check this without re-probing:
//
//	await using   flags=8198   naive=true    correct=true
//	using         flags=8196   naive=true    correct=false
//	const         flags=8194   naive=true    correct=false
//	let           flags=8193   naive=false   correct=false
//	var           flags=8192   naive=false   correct=false
//
// So the naive test is wrong on three of the five rather than only on const, and the two it gets
// right are the two nobody would have thought to check.
func requireAwaitIsAwaitUsing(list *ast.Node) bool {
	return list.Flags&ast.NodeFlagsBlockScoped == ast.NodeFlagsAwaitUsing
}

// requireAwaitHeadRange returns the span upstream reports on.
//
// Upstream calls `getFunctionHeadLoc`, which is three branches in a specific order, and the order
// is the part worth stating because the branches overlap:
//
//  1. If the function's PARENT is a property, a method definition or a property definition, the
//     span starts at the parent and runs to the opening parenthesis of the parameters. This branch
//     is tested first, so it wins over the arrow branch below.
//  2. Otherwise, if the function is an arrow, the span is the arrow token alone.
//  3. Otherwise the span starts at the function and runs to the opening parenthesis.
//
// Reproducing only branches two and three puts the start on the `async` keyword whenever a function
// is a property value, which is five of the eleven shapes below and 490 of the 767 findings this
// rule produced on the ahra tree. The line was right in every one of them and only the column moved,
// which is why a differential that compared lines reported perfect agreement.
//
//	const o = { run: async function(a) {} }         "run: async function"
//	const o = { async run(a) {} }                   "async run"
//	const o = { run: async (a) => {} }              "run: async "       <- not the arrow
//	class A { foo = async function(a) {} }          "foo = async function"
//	class A { static foo = async function(a) {} }   "static foo = async function"
//	class A { foo = async (a) => {} }               "foo = async "      <- not the arrow
//	class A { static async foo(a) {} }              "static async foo"
//	async function foo(a) {}                        "async function foo"
//	const f = async function(a) {}                  "async function"
//	const f = async function named(a) {}            "async function named"
//	const f = async (a) => {}                       "=>"                <- the arrow, no parent
//
// Every span above was measured against the installed rule rather than read off the source.
func requireAwaitHeadRange(ctx rule.Context, node *ast.Node) core.TextRange {
	// Branch one, and it is first for the reason above: a property value takes the parent's start
	// even when the function is an arrow. Our parser models a method as one node rather than as a
	// property holding a function, so the method shapes reach this through the node itself and
	// only the value-holding shapes have a parent to climb to.
	if parent := node.Parent; parent != nil && requireAwaitParentOwnsTheHead(parent) {
		start := rule.TokenRange(ctx.SourceFile, parent).Pos()
		return core.NewTextRange(start, requireAwaitParametersStart(ctx, node, start))
	}

	// Branch two.
	if node.Kind == ast.KindArrowFunction {
		if arrow := node.AsArrowFunction().EqualsGreaterThanToken; arrow != nil {
			return rule.TokenRange(ctx.SourceFile, arrow)
		}
	}

	// Branch three. The head runs from the first token of the node that carries the `async` keyword
	// through the end of the name. A method's `async` sits on the method rather than on any inner
	// function, which our parser already gives us since the two are one node.
	start := requireAwaitHeadStart(ctx, node)
	end := start
	if name := node.Name(); name != nil {
		end = name.End()
	} else {
		end = requireAwaitParametersStart(ctx, node, start)
	}
	if end <= start {
		end = start + len("async")
	}
	return core.NewTextRange(start, end)
}

// requireAwaitHeadStart finds where the reported span begins for a function that owns its own head.
//
// Upstream starts at `node.loc.start`, and the subtlety is what that node CONTAINS. ESTree treats a
// class member's modifiers as part of the function's parent and an `export` as a wrapper AROUND the
// declaration, so the two kinds of leading keyword fall on opposite sides of the start:
//
//	class A { static async foo() {} }           "static async foo"
//	class A { private static async foo() {} }   "private static async foo"
//	export async function foo() {}              "async function foo"     <- not `export`
//	export default async function foo() {}      "async function foo"     <- not `export default`
//
// Our parser hangs both kinds off the same node, so taking its start includes the `export` and puts
// the span a keyword too early. This was measured rather than reasoned: a differential against the
// installed rule over the ahra tree disagreed on 154 findings after the parent branch was fixed,
// every one of them an exported function, and every one agreeing on the line.
//
// So only the export-ish modifiers are skipped, and the class member modifiers are deliberately
// kept. Skipping all of them would move `static async foo` to `async foo`, which trades one column
// defect for another and looks equally plausible from the inside.
func requireAwaitHeadStart(ctx rule.Context, node *ast.Node) int {
	start := rule.TokenRange(ctx.SourceFile, node).Pos()
	modifiers := node.Modifiers()
	if modifiers == nil {
		return start
	}
	for _, modifier := range modifiers.Nodes {
		if modifier == nil {
			continue
		}
		switch modifier.Kind {
		case ast.KindExportKeyword, ast.KindDefaultKeyword, ast.KindDeclareKeyword:
			// Skip past it and keep looking, since `export default` is two of them.
			if end := rule.TokenRange(ctx.SourceFile, modifier).End(); end > start {
				start = end
			}
		default:
			// The first modifier that belongs to the head stops the scan, so a `static` after an
			// `export` still starts the span.
			return requireAwaitSkipSpaces(ctx, start)
		}
	}
	return requireAwaitSkipSpaces(ctx, start)
}

// requireAwaitSkipSpaces advances past whitespace, so a skipped keyword does not leave its space.
func requireAwaitSkipSpaces(ctx rule.Context, start int) int {
	text := ctx.SourceFile.Text()
	for start < len(text) {
		switch text[start] {
		case ' ', '\t', '\n', '\r':
			start++
			continue
		}
		break
	}
	return start
}

// requireAwaitParentOwnsTheHead answers whether the reported span starts at the function's parent.
//
// Upstream names five parent types. Two of them, `TSPropertySignature` and `TSMethodSignature`,
// describe declarations with no body, which this rule cannot reach because a function with no body
// has nothing to search for an await. The three that remain are a property in an object literal and
// a property declaration in a class.
//
// Upstream also has no value test and needs none, and neither does this. A function nested deeper
// inside an initializer, `foo = [async function(){}]`, has the ARRAY as its parent rather than the
// property, so the kind test alone already declines it. A guard comparing the initializer against
// the node was written here first and then removed: it survived every mutation because nothing can
// reach it, which is the signature of a condition that only looks like it is doing work. The four
// nested shapes in `TestRequireAwaitReportsOnTheFunctionHead` pin the behaviour either way.
func requireAwaitParentOwnsTheHead(parent *ast.Node) bool {
	switch parent.Kind {
	case ast.KindPropertyAssignment, ast.KindPropertyDeclaration:
		return true
	}
	return false
}

// requireAwaitParametersStart finds the opening parenthesis of a function's parameter list.
//
// Upstream reaches it through `getOpeningParenOfParams`. Found by scanning here rather than through
// the parameter list node, whose type carries a position but no `*ast.Node` to hand `TokenRange`.
// A type parameter list can put an earlier `<` in the way but never an earlier `(`, so the first
// one after the start is the right one.
func requireAwaitParametersStart(ctx rule.Context, node *ast.Node, start int) int {
	text := ctx.SourceFile.Text()
	for scan := start; scan < node.End() && scan < len(text); scan++ {
		if text[scan] == '(' {
			return scan
		}
	}
	return start
}

// requireAwaitAsyncKeywordRange returns the span the suggestion replaces.
//
// Upstream's range runs from the `async` token to the start of the NEXT token including comments,
// so the trailing whitespace goes with the keyword while a comment after it survives. Measured:
// `async /* c */ function foo` becomes `/* c */ function foo`.
func requireAwaitAsyncKeywordRange(ctx rule.Context, node *ast.Node) (core.TextRange, bool) {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return core.TextRange{}, false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier == nil || modifier.Kind != ast.KindAsyncKeyword {
			continue
		}
		keyword := rule.TokenRange(ctx.SourceFile, modifier)
		text := ctx.SourceFile.Text()
		end := keyword.End()
		// Consume the whitespace that follows, stopping at the next non-space byte. That byte
		// begins either the next token or a comment, and upstream stops before both.
		for end < len(text) {
			switch text[end] {
			case ' ', '\t', '\n', '\r':
				end++
				continue
			}
			break
		}
		return core.NewTextRange(keyword.Pos(), end), true
	}
	return core.TextRange{}, false
}

// requireAwaitReplacement returns what the suggestion writes in place of `async`.
//
// Almost always the empty string. Upstream substitutes a SEMICOLON instead when removing the
// keyword would let automatic semicolon insertion join this construct to the previous line, which
// its corpus exercises three times:
//
//	class A { a = 0 \n async [b](){} }    the field and the method would merge
//	class A { a = 0 \n async in(){} }     same, with a keyword-named method
//	foo \n async () => {}                 the call and the arrow would merge
//
// The test is whether the token after `async` could continue the preceding expression. Reproduced
// narrowly rather than by porting upstream's full `needsPrecedingSemicolon`, because the shapes
// that reach it here are exactly the two the corpus names: a class member whose predecessor is a
// property with no semicolon, and an expression statement beginning with a parenthesis or a
// bracket.
func requireAwaitReplacement(ctx rule.Context, node *ast.Node) string {
	if requireAwaitNeedsPrecedingSemicolon(ctx, node) {
		return ";"
	}
	return ""
}

// requireAwaitNeedsPrecedingSemicolon answers whether removing `async` would merge two statements.
//
// # Both halves are required, and each was measured
//
// Upstream asks two questions and emits a semicolon only when both answer yes: whether the token
// after `async` could CONTINUE the preceding expression, and whether the preceding construct
// actually needs a semicolon. Testing only the second reports a semicolon on shapes upstream leaves
// alone, which is how this arm was wrong on two of upstream's own cases before it was measured:
//
//	class A { a = 0 \n async [b](){} }    ";"   the bracket continues `0` as an index
//	class A { a = 0 \n async in(){} }     ";"   `in` continues `0` as a relational operator
//	class A { a = 0 \n async m(){} }      ""    a plain identifier cannot continue it
//	class A { a \n async [b](){} }        ""    the property has no initializer to continue
//	class A { foo() {} \n async [bar](){} } ""  a method needs no semicolon after it
//
// So the continuation test is about the token AFTER `async`, and the semicolon test is about what
// comes BEFORE the member. Reproduced narrowly rather than by porting upstream's full
// `needsPrecedingSemicolon`, because the shapes that reach it here are exactly these.
func requireAwaitNeedsPrecedingSemicolon(ctx rule.Context, node *ast.Node) bool {
	if !requireAwaitTokenAfterAsyncContinuesExpression(ctx, node) {
		return false
	}
	previous := requireAwaitPreviousSibling(node)
	if previous == nil {
		return false
	}
	// Only a construct that could still be an expression needs the guard. A method declaration is
	// already terminated by its body, so nothing can continue it.
	if !requireAwaitCanBeContinued(previous) {
		return false
	}
	// A previous sibling that already ends in a semicolon cannot be continued either.
	text := ctx.SourceFile.Text()
	end := previous.End()
	for end > 0 && (text[end-1] == ' ' || text[end-1] == '\t' || text[end-1] == '\n' ||
		text[end-1] == '\r') {
		end--
	}
	return end == 0 || text[end-1] != ';'
}

// requireAwaitCanBeContinued answers whether a preceding member could still be an open expression.
//
// A property declaration WITH an initializer is the only class member that can, because the
// initializer is an expression and the next line can extend it. A property with no initializer, a
// method, an accessor and a constructor are all closed already.
func requireAwaitCanBeContinued(previous *ast.Node) bool {
	switch previous.Kind {
	case ast.KindPropertyDeclaration:
		return previous.AsPropertyDeclaration().Initializer != nil
	case ast.KindExpressionStatement, ast.KindVariableStatement:
		return true
	}
	return false
}

// requireAwaitTokenAfterAsyncContinuesExpression answers whether the token following `async` could
// extend the expression on the line above.
//
// Three shapes can: an opening bracket (an index), an opening parenthesis (a call), and a keyword
// that is also a binary operator, of which `in` and `instanceof` are the ones a method can be named.
// A plain identifier cannot, because two identifiers in a row are not an expression.
func requireAwaitTokenAfterAsyncContinuesExpression(ctx rule.Context, node *ast.Node) bool {
	asyncRange, found := requireAwaitAsyncKeywordRange(ctx, node)
	if !found {
		return false
	}
	text := ctx.SourceFile.Text()
	scan := asyncRange.End()
	for scan < len(text) && (text[scan] == ' ' || text[scan] == '\t' || text[scan] == '\n' ||
		text[scan] == '\r') {
		scan++
	}
	if scan >= len(text) {
		return false
	}
	if text[scan] == '[' || text[scan] == '(' {
		return true
	}
	// A keyword-named method whose name is also a binary operator. `in` and `instanceof` are the
	// two; upstream reaches the same set through its own token classification.
	rest := text[scan:]
	for _, keyword := range []string{"instanceof", "in"} {
		if strings.HasPrefix(rest, keyword) {
			after := scan + len(keyword)
			if after >= len(text) || !requireAwaitIsIdentifierByte(text[after]) {
				return true
			}
		}
	}
	return false
}

// requireAwaitIsIdentifierByte answers whether a byte can continue an identifier.
func requireAwaitIsIdentifierByte(character byte) bool {
	switch {
	case character >= 'a' && character <= 'z':
		return true
	case character >= 'A' && character <= 'Z':
		return true
	case character >= '0' && character <= '9':
		return true
	case character == '_' || character == '$':
		return true
	}
	return false
}

// requireAwaitPreviousSibling returns the statement or member preceding a node, or nil.
//
// Only two containers matter: a class body, where a property with an initializer can run into the
// next member, and a statement list, where an expression statement can continue onto the next line.
//
// The walk climbs through intermediate nodes so a function EXPRESSION reaches the statement that
// holds it: `foo \n async () => {}` has the arrow nested inside an expression statement rather than
// sitting in the statement list itself.
func requireAwaitPreviousSibling(node *ast.Node) *ast.Node {
	subject := node
	for subject != nil {
		parent := subject.Parent
		if parent == nil {
			return nil
		}
		var siblings []*ast.Node
		switch parent.Kind {
		case ast.KindClassDeclaration, ast.KindClassExpression:
			siblings = parent.Members()
		case ast.KindBlock:
			siblings = parent.AsBlock().Statements.Nodes
		case ast.KindSourceFile:
			siblings = parent.AsSourceFile().Statements.Nodes
		}
		if siblings != nil {
			for index, sibling := range siblings {
				if sibling == subject {
					if index == 0 {
						return nil
					}
					return siblings[index-1]
				}
			}
			return nil
		}
		subject = parent
	}
	return nil
}

// requireAwaitDescribe renders a function the way upstream's `getFunctionNameWithKind` does, with
// the first letter capitalized.
//
// Nine renderings appear in the corpus and each is a distinct combination:
//
//	Async function 'foo'      a named declaration or named expression
//	Async function            an anonymous function expression
//	Async arrow function      any arrow, named or not
//	Async method 'foo'        a class or object-literal method
//	Async method ''           a method whose name is the empty string literal
//	Async method              a computed method name, which has no readable name
//
// The property name wins over the function's own where both exist: `{ async: async function foo() }`
// renders `Async method 'async'`, because the object property is what a reader searches for.
func requireAwaitDescribe(ctx rule.Context, node *ast.Node) string {
	kind := "function"
	switch node.Kind {
	case ast.KindArrowFunction:
		return "Async arrow function"
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		kind = "method"
	case ast.KindConstructor:
		return "Async method 'constructor'"
	}

	// A function expression that is the value of a property or a method definition is described by
	// the property's name rather than its own.
	nameHolder := node
	if parent := node.Parent; parent != nil {
		switch parent.Kind {
		case ast.KindPropertyAssignment:
			if parent.AsPropertyAssignment().Initializer == node {
				nameHolder = parent
				kind = "method"
			}
		}
	}

	if name := nameHolder.Name(); name != nil {
		if text, ok := requireAwaitNameText(name); ok {
			return fmt.Sprintf("Async %s '%s'", kind, text)
		}
	}
	return "Async " + strings.TrimSpace(kind)
}

// requireAwaitNameText reads a name node's text when the syntax settles it.
//
// A computed name answers nothing, which is what produces the bare `Async method` rendering. A
// string-literal name answers its cooked value, so a method named with an empty string
// literal renders WITH that empty name rather than as an unnamed method -- the
// empty string is a NAME rather than an absent one, and the two render differently.
func requireAwaitNameText(name *ast.Node) (string, bool) {
	switch name.Kind {
	case ast.KindIdentifier, ast.KindPrivateIdentifier:
		return name.Text(), true
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return name.Text(), true
	case ast.KindNumericLiteral:
		return name.Text(), true
	}
	return "", false
}
