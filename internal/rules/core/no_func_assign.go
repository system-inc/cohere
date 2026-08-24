package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

var messageNoFuncAssign = rule.Message{
	Id: "noFuncAssign",
	Description: "This assigns to the name a function declares. The declaration is hoisted, so the " +
		"assignment usually parses and runs without complaint, and what it costs shows up somewhere " +
		"else: every later call of the name invokes whatever was assigned instead of the function. " +
		"Inside the function's own body the mistake is quieter still, because the write happens on " +
		"the first call and the second call reaches a different value. A named function expression " +
		"is stricter, where the binding is immutable and the write throws in strict mode. Assign to " +
		"a separate variable, or declare the function as a variable if it is meant to be replaced.",
}

// NoFuncAssign flags an assignment to the name a function declares.
//
//	valid:   function foo() { var foo = bar; }
//	valid:   function foo(foo) { foo = bar; }
//	valid:   var foo = function() {}; foo = bar;
//	valid:   function foo() {} foo.x = 0;
//	invalid: function foo() {}; foo = bar;
//	invalid: function foo() { foo = bar; }
//	invalid: foo = bar; function foo() { };
//	invalid: var a = function foo() { foo = 123; };
//
// A function declaration binds its name in the enclosing scope, so the assignment parses and the
// mistake is silent at the line that makes it. What it costs shows up at the call sites: the name
// now holds whatever was assigned, so calling it calls that instead. The common real case is the
// write sitting inside the function's own body, where the first call replaces the function and
// every call after it reaches something else.
//
// A named function expression binds its name only inside its own body, and that binding is
// immutable, so `var a = function foo() { foo = 123; };` throws in strict mode rather than
// rebinding. Both shapes are in scope and both are upstream's.
//
// # Why this reads the checker
//
// The discrimination is entirely name resolution and none of it is structural. Four of upstream's
// seven clean cases write to a name spelled exactly like the function:
//
//	function foo(foo) { foo = bar; }              a parameter shadows it
//	function foo() { var foo = bar; }             a local var shadows it inside the body
//	function foo() { var foo; foo = bar; }        the same, written on a later line
//	var foo = function() { foo = bar; };          the variable, not any function name
//
// Their text is nearly identical to the failing forms, so a rule matching on the name reports all
// four. Upstream answers with `get_resolved_references` plus `is_write()`, which is a find-all
// -references index we do not have. The equivalent question our checker does answer is which
// declaration a given identifier binds to, so this anchors on the function node the listener
// already received and asks, for each write, whether the resolved symbol's first declaration is
// that same node.
//
// Node identity rather than declaration kind. Kind looks like it would work, since the anchor is a
// distinctive node type, and it fails silently the moment a shadow shares it: in
// `function foo() {} { function foo() {} foo = 1; }` both anchors are `KindFunctionDeclaration`, so
// a kind test calls the inner write a match for the outer function too and reports twice where
// identity reports once. Upstream's corpus contains no same-kind shadow pair at all and cannot
// catch that, which is why the case is written by hand in the fixtures. The same trap is sharper on
// the sibling rules over `const`, where a `const A` and a shadowing `let A` are both
// `KindVariableDeclaration` and kind cannot separate them even in principle.
//
// # Why the checker is not sufficient on its own
//
// Symbol identity says which binding an identifier names; it says nothing about whether the
// occurrence writes. `function foo() {} foo.x = 0;` and `function foo() {} foo();` both resolve to
// the function and neither reassigns it, so the rule needs the structural half too and reports only
// where the two agree. `writesToItsIdentifier` is that half, shared with the sibling rule on class
// bindings.
//
// # One shape the checker answers differently
//
// `({foo} = {})` is a shorthand property in an object literal being used as a destructuring target,
// and `GetSymbolAtLocation` on its identifier returns the property's own symbol rather than the
// binding being written. TypeScript has a separate accessor for the value side and
// `resolvesToDeclaration` reaches for it. Without that, the case reads exactly like a correctly
// declined shadow, which is the quiet direction. Upstream's corpus has the object-pattern shape
// only with an explicit key (`({x: foo = 0} = bar)`), which the plain accessor handles, so this one
// is covered by a hand-written fixture rather than an imported one.
//
// No fix. The repair is a new binding, which means choosing a name and deciding which later uses of
// the old one meant the function, and the rule cannot know that.
var NoFuncAssign = rule.Rule{
	Name: "no-func-assign",

	// See the doc above: four of upstream's seven clean cases are textually near-identical to
	// failing ones and differ only in what the name resolves to.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		reportWritesTo := func(node *ast.Node) {
			// The engine hands every rule a nil checker when the program could not be built, and
			// this rule can answer nothing without one.
			if ctx.TypeChecker == nil {
				return
			}

			// An anonymous function expression declares no name, so there is nothing to reassign.
			// `var foo = function() { foo = bar; };` writes to the variable and upstream leaves it,
			// and this is the line that keeps it clean. Arrow functions are not listened for at
			// all, since they can never carry a name.
			name := node.Name()
			if name == nil || name.Kind != ast.KindIdentifier {
				return
			}

			declaration := declarationAnchoredAt(ctx, name)
			if declaration == nil {
				return
			}

			// The whole file, not the function subtree. A write can sit before the declaration
			// (`foo = bar; function foo() {}`), after it, or inside it, and hoisting makes all
			// three the same binding. Anchoring the search on the file and the match on the symbol
			// is what makes the position of the write irrelevant.
			sourceFile := ast.GetSourceFileOfNode(node)
			if sourceFile == nil {
				return
			}

			var visit func(*ast.Node)
			visit = func(current *ast.Node) {
				if current == nil {
					return
				}
				// The function's own name needs no exclusion here. It is an identifier whose text
				// matches and which resolves to this very declaration, so only the structural half
				// declines it, and that is enough: the climb from a function name reaches the
				// function itself, which is not an assignment.
				//
				// The text comparison is a pre-filter and not a discrimination. Symbol identity
				// already implies it, since an identifier spelled differently cannot resolve to
				// this declaration. It is here because it is far cheaper than a checker call and
				// this walk visits every identifier in the file.
				//
				// A mutant removing it survived the whole suite, and that survival is the subsumed
				// case rather than a blind spot: `resolvesToDeclaration` answers false for every
				// identifier this line would have rejected, so no input can distinguish the two
				// versions by what they report. The line stays for cost, not for correctness, and
				// no fixture was added for it because a fixture asserting a performance guard
				// asserts nothing.
				if current.Kind == ast.KindIdentifier &&
					current.Text() == name.Text() &&
					writesToItsIdentifier(current) &&
					resolvesToDeclaration(ctx, current, declaration) {
					ctx.ReportNode(current, messageNoFuncAssign)
				}
				current.ForEachChild(func(child *ast.Node) bool {
					visit(child)
					return false
				})
			}
			visit(sourceFile.AsNode())
		}

		// Both kinds, and they behave differently on purpose. A declaration's name is visible to
		// the enclosing scope, so a write anywhere in the file can reach it. A function
		// expression's name binds only inside its own body, so
		// `var foo = function() {}; foo = bar;` writes to the variable and is clean while
		// `var a = function foo() { foo = 123; };` writes to the function name and is not. Nothing
		// here encodes that split: the checker resolves each write and the two cases fall out of
		// the same code.
		//
		// Arrow functions are deliberately absent. They carry no name of their own, so
		// `var foo = () => {}; foo = bar;` writes to the variable and upstream leaves it, which is
		// what its `foo` pass case asserts.
		return rule.Listeners{
			ast.KindFunctionDeclaration: reportWritesTo,
			ast.KindFunctionExpression:  reportWritesTo,
		}
	},
}
