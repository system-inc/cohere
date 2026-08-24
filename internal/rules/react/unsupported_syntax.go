package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utils/typecheck"
)

var messageUnsupportedEval = rule.Message{
	Id: "unsupportedEval",
	Description: "This component or hook calls `eval`, so React Compiler stops optimizing it. " +
		"The compiler works by tracking where every value comes from and what touches it, and " +
		"`eval` runs code that only exists as a string at runtime, so there is nothing to track " +
		"through. The component still works, it just never gets memoized. Replace the eval with " +
		"the operation it stands in for: `JSON.parse` for data, bracket access for a dynamic " +
		"property, a lookup table for a dynamic function.",
}

var messageUnsupportedWith = rule.Message{
	Id: "unsupportedWith",
	Description: "This component or hook uses a `with` statement, so React Compiler stops " +
		"optimizing it. Inside a `with` block, a bare name might be a property of the object or " +
		"might be an outer variable, and which one it is cannot be known until the code runs, so " +
		"the compiler cannot say where any value came from. The statement is also forbidden in " +
		"strict mode and in modules. Destructure the object or read its properties explicitly.",
}

var messageUnsupportedInlineClass = rule.Message{
	Id: "unsupportedInlineClass",
	Description: "This component or hook declares a `class` in its body, so React Compiler stops " +
		"optimizing it. A class declared during render is a brand new class on every render, so " +
		"every instance of it is a fresh identity that nothing downstream can memoize against, " +
		"and the compiler declines to reason about it rather than guessing. Move the class to " +
		"module scope, where it is declared once.",
}

// UnsupportedSyntax flags syntax React Compiler will not support inside a component or hook.
//
//	valid:   function Component(props) { return <div>{props.text}</div>; }
//	valid:   function helper(a) { eval('a'); return a; }          (not a component or hook)
//	valid:   function Component(props) { const Foo = class {}; return <div />; }  (expression)
//	invalid: function Component(props) { eval('props.x = true'); return <div />; }
//	invalid: function Component(props) { with (props) {} return <div />; }
//	invalid: function Component(props) { class Foo {} return <div />; }
//
// Ported from React's `unsupported-syntax` rule, which is `ErrorCategory.UnsupportedSyntax` in the
// React Compiler. Unlike `gating` and `config`, oxc does ship a linter rule for this one, so both
// authorities were readable and both were run:
//
//	react 7.1.1   node_modules/eslint-plugin-react-hooks/cjs/eslint-plugin-react-hooks.development.js
//	              and driven through the ESLint Linter API on 60-plus probe inputs
//	oxc           crates/oxc_linter/src/rules/react/unsupported_syntax.rs, a thin category filter
//	              over crates/oxc_react_compiler, and driven through the 1.79.0 release binary
//
// # The category is exactly three diagnostics, and the dispatch that sent me here named a fourth
//
// The task named four sites in oxc's `build_hir.rs`: `with` at 6567, inline `class` at 6570, `eval`
// at 875, and a missing parameter binding at 714. **The fourth is not this rule.** Read at
// `diagnostics.rs:1525`, `missing_parameter_binding` constructs with `ErrorCategory::Invariant`,
// while the other three construct with `ErrorCategory::UnsupportedSyntax` at 1533, 1557 and 1566.
// Since oxc's lint rule is `run_react_compiler_rule(ctx, ErrorCategory::UnsupportedSyntax)`, a
// category filter and nothing else, an `Invariant` diagnostic can never surface under this rule's
// name. Grepping `ErrorCategory::UnsupportedSyntax` across the whole crate finds those three
// constructors and one test enumeration, and nothing else. So the count is three, not four.
//
// That diagnostic is also not syntactic in any case. It fires when a plain identifier parameter
// fails to resolve to a local binding after the compiler has already tried a descendant-scope
// lookup, which is a scope-resolution failure inside the compiler rather than a property of the
// source. Its own help text calls it `[BuildHIR] Could not find binding`, which is a note to a
// compiler author rather than to a user.
//
// # React has a fourth that oxc does not, and it is unreachable
//
// React constructs `UnsupportedSyntax` at four sites, not three. The extra one is in
// `resolveBinding` (bundle line 21155): an identifier literally named `this` reports "`this` is not
// supported syntax". oxc has no counterpart.
//
// It was probed rather than reasoned about, and it does not fire. `function Component(props) {
// return <div>{this.x}</div>; }` is silent under React's own rule, because ordinary `this` parses
// as a ThisExpression and never reaches `resolveBinding`, which takes an Identifier node. The one
// upstream fixture that produces a `this`-named identifier is
// `error.typescript-this-param-in-compiled-function`, a TypeScript `this` parameter, and its golden
// reports "Expected a non-reserved identifier name" under `ErrorCategory::Syntax` rather than under
// this category at all. So the site exists, is unreachable through this rule, and is not ported.
// Recorded rather than omitted silently, because the next reader will find it in the bundle.
//
// # The gate, which is the whole rule
//
// None of the three diagnostics fires on code outside a component or hook. Measured on React's own
// rule: `eval` at module top level, `eval` in a lowercase function, and `eval` in a capitalized
// function that returns a number are all silent, and only the component case reports. This is not
// an optimization. It is what the rule decides, and a port without it would report every `eval` in
// every file.
//
// The gate is React Compiler's `compilationMode: 'infer'` inference, `getComponentOrHookLike` at
// bundle line 51105, and it asks three things of the enclosing function:
//
//   - **A name that claims to be a component or a hook.** `/^[A-Z]/` for a component,
//     `/^use[A-Z0-9]/` for a hook. Both are ASCII regexes.
//   - **Evidence it is one**: `callsHooksOrCreatesJsx`, which walks the body for any JSX node or any
//     call whose callee reads as a hook name, and which **skips nested functions**, so JSX that only
//     appears inside an inner closure does not count.
//   - **For components only**, parameters that could be props: at most two, the first not a rest
//     element and not annotated as a primitive, and the second, if present, named with `ref` in it.
//
// Every one of those was measured on React's own rule rather than inferred. `function Component(...props)`
// is silent; `function Component(props, other)` is silent while `function Component(props, ref)`
// reports; `function Component(props: string)` is silent while `function Component(props: {a: number})`
// reports; and a component whose only JSX is inside a nested arrow is silent.
//
// # Where this port narrows the gate deliberately, and why
//
// React's inference also names a function through a variable declarator, an assignment, an object
// property, and a member expression, and it has `forwardRef` and `memo` callback paths. Measured
// against React's own rule, most of those are silent in practice: an object property with a
// capitalized key, an assignment to `ns.Component`, a bare `forwardRef(...)` callback and a
// `memo(...)` callback all produce no finding, while a plain `const Component = (props) => ...`
// does. So this port recognizes two naming forms, the function declaration and the variable
// declarator, and that is a measured subset rather than a guess about which paths matter.
//
// `returnsNonNode` is **not** implemented, and that is also measured rather than skipped. React
// reads it as a component disqualifier, so a function returning an object literal should be
// exempt, and it is not: `function Component(props) { eval('x'); if (props.a) return {}; return <div />; }`
// **reports** under React's own rule. Implementing the check as written would have made this port
// silent on an input React reports. That is the port brief's warning about a helper being more
// careful than upstream, arriving through the reference implementation's own source.
//
// The consequence of both narrowings is the same and it is stated rather than hidden: this rule
// reports a **subset** of what React reports, never a superset. A component named only through an
// object property is missed. Nothing clean is flagged. That is the direction a port should miss in,
// and every narrowing here was chosen after measuring which way upstream actually falls.
//
// # `eval` is resolved, not matched
//
// The `eval` diagnostic fires in `lowerIdentifier` on a binding that resolved to the **global**
// named `eval`, so it is a name-resolution question rather than a syntactic one, and two
// consequences follow that a text match would get wrong in opposite directions.
//
// A shadowed `eval` is silent: `function Component(eval) { eval('x'); return <div />; }` reports
// nothing under React's rule, because the binding resolves to the parameter. And a bare reference
// fires without any call: `const e = eval;` reports, because the identifier is lowered whether or
// not it is being invoked. Both were measured, and a rule keyed on `KindCallExpression` with a
// callee named `eval` would have been wrong on both.
//
// So this rule declares the type checker and asks `typecheck.IsSymbolFromDefaultLibrary`. Probed
// against this rule's own inputs rather than trusted: global `eval` resolves to a symbol declared
// in `lib.es5.d.ts`, a declaration file, while a shadowing parameter or local resolves to a
// declaration in the source file. The shelf helper and a hand-rolled `IsDeclarationFile` loop agree
// on all three shapes.
//
// # A syntactic filter on the eval identifier was written, measured subsumed, and deleted
//
// The first version of this rule carried an `isReadReference` predicate excluding a property name,
// a member access's name half, a declared binding's own identifier, an import specifier and a
// class member, on the reasoning that none of those is a reference to the global. It read as
// obviously load-bearing and a mutation disabling it entirely SURVIVED the whole fixture suite.
//
// Probed rather than patched with another fixture. Over six shapes, printing the guard's answer
// and the symbol lookup's answer side by side, the two columns were identical everywhere: every
// identifier the predicate declined also failed `IsSymbolFromDefaultLibrary`, because a property
// name resolves to the property's own symbol and a declared binding resolves to its declaration in
// the source file. Neither can resolve to `lib.es5.d.ts`.
//
// So it was a subsumed branch rather than a fixture gap, and the port brief's standing advice is to
// delete a subsumed branch and leave the reasoning at the line rather than write a test that
// asserts nothing. The clean cases it was protecting are still fixtures here, now pinned by the
// mechanism that actually decides them.
//
// # `with` is reachable for us where it was not for oxc, which is a parser difference
//
// Probing oxc's release binary on a `.tsx` file, `with` produced a bare `error: 'with' statements
// are not allowed` with no rule code, which is a parse error rather than a finding: TypeScript
// forbids the statement outright. Re-running the same body in a `.jsx` file produced
// `react(unsupported-syntax): JavaScript 'with' syntax is not supported`, so oxc and React agree
// and the earlier zero was an artifact of the extension. That is the port brief's "run a control
// alongside any zero" rule catching a divergence that was not one.
//
// Our parser recovers instead of refusing. Probed on typescript-go: `with (props) {}` produces a
// live `KindWithStatement` in both `.ts` and `.tsx`, so the arm is reachable here in files where
// oxc could not reach it. The rule is therefore wider than oxlint on TypeScript input and exactly
// as wide as React, which is the right side to land on.
//
// # An inline class is a DECLARATION, and the corpus says so
//
// `oxc::Statement::ClassDeclaration` and React's `case 'ClassDeclaration'` both sit in the statement
// dispatch, so a class **expression** is untouched: `const Foo = class {};` inside a component is
// silent at both upstreams and silent here, measured on React's rule and on the release binary.
//
// The corpus carries one case for this and it is a clean one that looks like a violation.
// `error.todo-kitchensink.js` declares `class Bar {}` inside `function foo`, and it is silent under
// React's own rule because `foo` is lowercase, so the gate declines the whole function. Its golden
// reports a different category entirely. That fixture is the reason the gate is not optional: a port
// without it would report an imported case upstream passes.
//
// # Where the findings point
//
// The identifier `eval` alone, the whole `with` statement, and the whole class declaration. Read off
// React's own golden for the eval case, which draws `^^^^` under four characters, and off the
// ESLint runs for the other two, which report columns 3 through 19 for `with (props) {}` and 3
// through 15 for `class Foo {}`. All three reproduce exactly through `ctx.ReportNode`, verified
// against the source bytes before this rule was written, and asserted in the fixtures because a
// message-id assertion cannot see a span.
var UnsupportedSyntax = rule.Rule{
	// No namespace prefix. The config writes `react/unsupported-syntax`, and the parity guard
	// strips the namespace on a `/` boundary, so `react-unsupported-syntax` would match no
	// inventory entry while still passing every fixture in this package.
	Name: "unsupported-syntax",

	// The `eval` arm asks which declaration a name binds to, which is the resolution half of the
	// port brief's table rather than the scope-flag half. Established by probe: `GetSymbolAtLocation`
	// on `eval` answers a `lib.es5.d.ts` declaration for the global and a source-file declaration
	// for a shadowing parameter, and nothing in the abstract syntax tree distinguishes them.
	NeedsTypeChecker: true,

	// `typecheck.IsSymbolFromDefaultLibrary` takes the compiled unit, so the handle is genuinely
	// read here rather than only named in prose.
	ReadsProgram: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindIdentifier: func(node *ast.Node) {
				if ctx.TypeChecker == nil {
					return
				}
				if node.Text() != "eval" {
					return
				}
				// There is deliberately no syntactic filter here excluding a property name or a
				// declared binding, and its absence is a measurement rather than an omission. See
				// the note below on why the symbol lookup already answers for all of them.
				if !isInsideComponentOrHook(node) {
					return
				}
				symbol := ctx.TypeChecker.GetSymbolAtLocation(node)
				if !typecheck.IsSymbolFromDefaultLibrary(ctx.Program, symbol) {
					return
				}
				ctx.ReportNode(node, messageUnsupportedEval)
			},
			ast.KindWithStatement: func(node *ast.Node) {
				if !isInsideComponentOrHook(node) {
					return
				}
				ctx.ReportNode(node, messageUnsupportedWith)
			},
			ast.KindClassDeclaration: func(node *ast.Node) {
				// A class declaration whose parent is the source file is module scope, which is
				// exactly where upstream tells the author to move it. The gate below already
				// declines it, since module scope has no enclosing function, but stating it here
				// keeps the arm readable next to the message that says "move it to module scope".
				if !isInsideComponentOrHook(node) {
					return
				}
				ctx.ReportNode(node, messageUnsupportedInlineClass)
			},
		}
	},
}

// isInsideComponentOrHook reports whether a node sits inside a function React Compiler compiles.
//
// # The rule, which took seventeen probe rounds against React's own rule to state correctly
//
// Two conditions, and both are necessary. A function is a compilation root when it is
// component-or-hook-like on its own merits, **and** its position is reachable: every step from it
// out to the source file passes through either a component-or-hook-NAMED function or a construct
// that forms no scope of its own. Once a root is chosen its whole subtree is lowered, so anything
// nested inside it is judged whether or not the nested thing looks like a component.
//
// The second condition is the one that is invisible from `getComponentOrHookLike`, which knows
// nothing about where its argument sits, and it is where a port goes wrong. It comes from
// `findFunctionsToCompile`, which walks the program picking roots.
//
// The distinction between "like" and "NAMED" in that sentence is load-bearing and was measured
// rather than reasoned. Varying only the enclosing function's name over eight spellings, with the
// body held byte-identical:
//
//	function useOuter() { const Component = p => { eval('x'); return <div />; }; ... }   reports
//	function Outer()    { ... same body ... }                                            reports
//	function Xyz()      { ... same body ... }                                            reports
//	function use2()     { ... same body ... }                                            reports
//	function plainOuter() { ... same body ... }                                          silent
//	function outer()    { ... same body ... }                                            silent
//	function abc()      { ... same body ... }                                            silent
//	function use()      { ... same body ... }                                            silent
//
// The enclosing function has no evidence of its own in any of those, so it never qualifies as a
// root itself. Its NAME still decides whether the traversal reaches the inner arrow. That is why
// the position walk asks only the name question of ancestors while asking the full question of the
// candidate itself.
//
// # The measurements that pin the rest of it
//
// Position, holding shape fixed:
//
//	{ function Component(props) { eval('x'); return <div />; } }                 silent
//	if (true) { function Component(props) { eval('x'); return <div />; } }       silent
//	class K { m() { function Component(props) { eval('x'); ... } } }             silent
//	function Component(props) { eval('x'); return <div />; }                     reports
//
// Evidence, holding position and name fixed:
//
//	function Component(props) { eval('x'); const inner = () => <div />; ... }    silent
//	function Component(props) { eval('x'); return <div />; }                     reports
//
// The first of those is the nested-function skip in the evidence walk: JSX that only appears inside
// an inner closure does not count for the enclosing function. And once a root does qualify on its
// own evidence, a plain nested function inside it is still judged:
//
//	function Component(props) { const inner = () => { eval('x'); ... }; return <div>{inner()}</div>; }  reports
func isInsideComponentOrHook(node *ast.Node) bool {
	for current := node.Parent; current != nil; current = current.Parent {
		if isComponentOrHookLike(current) && isReachableRootPosition(current) {
			return true
		}
	}
	return false
}

// isReachableRootPosition reports whether the traversal picking compilation roots can reach a
// function-like node at all.
//
// Everything between the candidate and the source file must be either transparent, meaning it forms
// no scope and is just how the function is written down, or a component-or-hook-NAMED function,
// through which the traversal continues looking for roots.
//
// A name test rather than the full `isComponentOrHookLike` on ancestors, for the reason measured at
// the caller: an enclosing function with a qualifying name and no evidence of its own still lets the
// traversal through, while the same body under a non-qualifying name does not.
//
// The transparent set is an allow list rather than a deny list on purpose. A deny list silently
// admits every node kind nobody thought of, and admitting one wrongly makes this rule report inside
// functions upstream never compiles, which is the expensive direction to be wrong in.
func isReachableRootPosition(node *ast.Node) bool {
	for current := node.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindSourceFile:
			return true
		case ast.KindVariableDeclaration, ast.KindVariableDeclarationList,
			ast.KindVariableStatement, ast.KindExportAssignment, ast.KindExportDeclaration,
			ast.KindParenthesizedExpression:
			// The spelling of a top-level binding, an export wrapper, and a redundant parenthesis
			// our parser keeps where Babel's tree drops it. None of these moves the function into a
			// nested scope, and omitting the parenthesis arm made `const Component = ((props) => ...)`
			// unreachable while React reports it.
			continue
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
			if !hasComponentOrHookName(current) {
				return false
			}
			continue
		case ast.KindBlock:
			// A function body block is transparent, because the function above it has already been
			// judged by the arm above. A bare block, an `if` body or a loop body is not, and both
			// spell as KindBlock, so the parent is what separates them.
			parent := current.Parent
			if parent == nil || !isSkippedNestedFunction(parent) {
				return false
			}
			continue
		default:
			return false
		}
	}
	return false
}

// skipParenthesesUpward walks out through redundant parentheses to the first real parent.
//
// The mirror of `ast.SkipParentheses`, which walks inward. Our parser keeps a parenthesized
// expression as a live node, so a function written `((props) => {})` sits one level deeper than the
// same function written without them, and every ancestor test has to account for it.
func skipParenthesesUpward(node *ast.Node) *ast.Node {
	current := node.Parent
	for current != nil && current.Kind == ast.KindParenthesizedExpression {
		current = current.Parent
	}
	return current
}

// hasComponentOrHookName reports whether a function-like node carries a name that claims to be a
// component or a hook, without asking whether it has any evidence of being one.
//
// The name half of the gate on its own. See the table at `isInsideComponentOrHook` for the eight
// spellings that establish this is genuinely the name and not the shape.
func hasComponentOrHookName(node *ast.Node) bool {
	name, named := inferredFunctionName(node)
	if !named {
		return false
	}
	return isReactComponentName(name) || isReactHookName(name)
}

// isComponentOrHookLike answers React's `getComponentOrHookLike` for the two naming forms this port
// recognizes.
//
// The name is read from the declaration's own identifier or, for a function expression and an arrow,
// from the variable declarator that holds it. See the note on the rule for why the object-property,
// assignment, member-expression, `forwardRef` and `memo` paths are not recognized: all of them were
// measured silent under React's own rule on this rule's diagnostics.
func isComponentOrHookLike(node *ast.Node) bool {
	name, named := inferredFunctionName(node)
	if !named {
		return false
	}
	if isReactHookName(name) {
		return callsHooksOrCreatesJsx(node)
	}
	if !isReactComponentName(name) {
		return false
	}
	return callsHooksOrCreatesJsx(node) && hasComponentShapedParameters(node)
}

// inferredFunctionName returns the name React would infer for a function-like node.
//
// A function declaration carries its own identifier. A function expression or an arrow takes the
// name of the variable declarator initializing it, which is the `parent.isVariableDeclarator() &&
// parent.get('init').node === path.node` branch of upstream's `getFunctionName`. The identity check
// on the initializer matters: without it a declarator whose initializer merely *contains* the
// function, such as `const Component = memo(props => ...)`, would hand the arrow the outer name,
// and that case is measured silent upstream.
func inferredFunctionName(node *ast.Node) (string, bool) {
	switch node.Kind {
	case ast.KindFunctionDeclaration:
		name := node.AsFunctionDeclaration().Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			return "", false
		}
		return name.Text(), true
	case ast.KindFunctionExpression, ast.KindArrowFunction:
		// Parentheses are skipped on the way OUT to the declarator, and this direction was measured
		// rather than assumed. Our parser keeps `const Component = ((props) => {...});` as a real
		// KindParenthesizedExpression between the declaration and the arrow, where Babel's tree has
		// already dropped it, so an unskipped walk finds a parenthesized expression as the parent
		// and answers unnamed. Measured on React's own rule, that input **reports**, so withholding
		// the skip here would be a false negative rather than a harmless narrowing. It was the only
		// disagreement in a 111-input differential run against React and it is the port brief's
		// "measure which tree drops the parens" rule producing a live hit.
		parent := skipParenthesesUpward(node)
		if parent == nil || parent.Kind != ast.KindVariableDeclaration {
			return "", false
		}
		declaration := parent.AsVariableDeclaration()
		if ast.SkipParentheses(declaration.Initializer) != node {
			return "", false
		}
		name := declaration.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			return "", false
		}
		return name.Text(), true
	}
	return "", false
}

// isReactComponentName answers React's `/^[A-Z]/`.
//
// Written here rather than taken from `react.IsLikelyComponentName`, and the reason is measured
// rather than stylistic. That shelf helper tests `unicode.IsUpper`, so `Émile` and `Ωmega` are
// component names to it. Probed against React's own rule, `function Émile(props) { eval('x');
// return <div />; }` is **silent**, because React's test is an ASCII regex. Building on the shelf
// helper would have reported an input upstream passes.
//
// The shelf's own doc comment already records that divergence and says nothing has established
// which answer we want. For this rule the answer is established: React is the authority and React
// is ASCII, so the ASCII test is what ships here.
func isReactComponentName(name string) bool {
	if name == "" {
		return false
	}
	first := name[0]
	return first >= 'A' && first <= 'Z'
}

// isReactHookName answers React's `/^use[A-Z0-9]/`.
//
// Also written here rather than taken from `react.IsHookName`, and also for a measured reason
// running the other way. The shelf helper rejects a digit after the prefix, so `use2Things` is not a
// hook to it. Probed against React's own rule, `function use2Things(a) { eval('x'); return <div />; }`
// **reports**, because React's character class is `[A-Z0-9]`. Building on the shelf helper would
// have gone silent on an input upstream reports.
//
// Both shelf name helpers are therefore wrong for this rule, in opposite directions, and both have
// doc comments accurately describing behavior that does not match this authority. That is the port
// brief's shelf-probe rule producing two hits in one file.
func isReactHookName(name string) bool {
	if len(name) < 4 {
		return false
	}
	if name[0:3] != "use" {
		return false
	}
	fourth := name[3]
	return (fourth >= 'A' && fourth <= 'Z') || (fourth >= '0' && fourth <= '9')
}

// callsHooksOrCreatesJsx reports whether a function's own body writes JSX or calls a hook.
//
// Upstream's `callsHooksOrCreatesJsx` traverses the body and installs skip handlers on every nested
// function kind, so a nested closure's contents do not count toward the enclosing function's
// evidence. That skip is reproduced by not descending into a nested function-like node, and it is
// load-bearing rather than an optimization: measured on React's rule, a capitalized function whose
// only JSX lives inside a nested arrow is silent, and so is one whose only hook call does.
//
// The parameter list is not walked either, matching upstream, which traverses the body path.
func callsHooksOrCreatesJsx(node *ast.Node) bool {
	body := functionBody(node)
	if body == nil {
		return false
	}
	found := false
	var walk func(current *ast.Node)
	walk = func(current *ast.Node) {
		if current == nil || found {
			return
		}
		if isJsxNode(current) {
			found = true
			return
		}
		if current.Kind == ast.KindCallExpression && isCompilerHookCallee(current.AsCallExpression().Expression) {
			found = true
			return
		}
		if isSkippedNestedFunction(current) {
			return
		}
		current.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return found
		})
	}
	// The body itself may be a function-like node's block; walking its children rather than the
	// body node avoids the skip test declining the very function being judged.
	body.ForEachChild(func(child *ast.Node) bool {
		walk(child)
		return found
	})
	// An arrow with an expression body is the one shape whose body is not a block, and its
	// expression is the thing to judge rather than something to descend past.
	if !found && body.Kind != ast.KindBlock {
		walk(body)
	}
	return found
}

// functionBody returns the body of a function-like node, or nil when it has none.
//
// `Node.Body()` is an accessor and the port brief's standing hazard is that every `Node.Xxx()`
// accessor panics off its kind, so the kind is established by the switch before the typed accessor
// is reached rather than by trusting a general helper.
func functionBody(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindFunctionDeclaration:
		return node.AsFunctionDeclaration().Body
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().Body
	case ast.KindArrowFunction:
		return node.AsArrowFunction().Body
	}
	return nil
}

// isSkippedNestedFunction reports the node kinds upstream installs skip handlers for.
//
// Exactly three, matching upstream's `ArrowFunctionExpression`, `FunctionExpression` and
// `FunctionDeclaration` skip handlers. A method or an accessor is deliberately absent: upstream
// does not skip them either, and a class body inside a component is already the inline-class
// finding rather than something to search for evidence in.
//
// Not named `isFunctionLike`, which `error_boundaries.go` in this package already defines for a
// different question. Its set is the one that answers "is this a function", while this one is the
// narrower set upstream declines to descend through, and collapsing them would silently widen or
// narrow one caller.
func isSkippedNestedFunction(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
		return true
	}
	return false
}

// isJsxNode reports whether a node is any JSX construct.
//
// Upstream's traversal registers the `JSX` alias, which Babel expands to every JSX node type, so
// the test here is over the kinds our parser produces for the same source rather than over element
// kinds alone. A fragment is included because `return <></>;` was measured as sufficient evidence
// under React's rule.
func isJsxNode(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindJsxElement, ast.KindJsxSelfClosingElement, ast.KindJsxFragment,
		ast.KindJsxOpeningElement, ast.KindJsxOpeningFragment, ast.KindJsxExpression,
		ast.KindJsxText, ast.KindJsxAttribute, ast.KindJsxAttributes, ast.KindJsxSpreadAttribute:
		return true
	}
	return false
}

// isCompilerHookCallee reports whether a call's callee reads as a hook to the React Compiler.
//
// Upstream's `isHook` accepts a bare identifier with a hook name and a non-computed member
// expression whose property is a hook name and whose object is a capitalized namespace. Both are
// reproduced; the namespace test is what makes `React.useState(...)` count while `obj.useThing(...)`
// on a lowercase receiver does not.
//
// # Why this does not reuse `isHookCallee`, which is in this package and answers the same shape
//
// `rules_of_hooks.go` defines `isHookCallee` with an identical structure, and reusing it was the
// first instinct. Its name predicate is `isHookIdentifierName`, whose own doc comment records a
// deliberate divergence: it accepts **bare `use`**, because both of that rule's upstreams treat
// `use(...)` as a hook and its corpus turns on the case.
//
// The React Compiler does not. Its `isHookName` is `/^use[A-Z0-9]/`, which bare `use` fails.
// Measured on React's own rule rather than argued from the regex: a capitalized function whose only
// evidence is a `use(props.p)` call is **silent**, and so is `function use(a) { eval('x'); return
// <div />; }`, while both `use42` spellings **report**. Routing this rule through the neighbouring
// helper would have reported two inputs React passes.
//
// So the two live side by side, each correct for its own authority, and this is the port brief's
// "read the body of anything whose name matches your question" warning producing a hit inside a
// single package rather than out on a shelf.
func isCompilerHookCallee(callee *ast.Node) bool {
	if callee == nil {
		return false
	}
	switch callee.Kind {
	case ast.KindIdentifier:
		return isReactHookName(callee.Text())
	case ast.KindPropertyAccessExpression:
		access := callee.AsPropertyAccessExpression()
		name := access.Name()
		if name == nil || name.Kind != ast.KindIdentifier || !isReactHookName(name.Text()) {
			return false
		}
		object := access.Expression
		return object != nil && object.Kind == ast.KindIdentifier &&
			isReactComponentName(object.Text())
	}
	return false
}

// hasComponentShapedParameters answers React's `isValidComponentParams`.
//
// Three rules, all measured on React's own rule rather than read off the source alone. No
// parameters is valid. One or two are valid when the first is not a rest element and is not
// annotated as a primitive type. A second parameter must be an identifier whose name contains
// `ref` or `Ref`, which is upstream's own `name.includes('ref') || name.includes('Ref')`. Three or
// more is never valid.
//
// This applies to components only. Upstream asks it inside the `isComponentName` branch and not
// inside the hook branch, so a hook with any parameter shape still qualifies, which is why
// `function use2Things(a)` reports.
func hasComponentShapedParameters(node *ast.Node) bool {
	parameters := functionParameters(node)
	switch len(parameters) {
	case 0:
		return true
	case 1:
		first := parameters[0]
		return !isSpreadParameter(first) && !hasPrimitiveTypeAnnotation(first)
	case 2:
		first := parameters[0]
		if isSpreadParameter(first) || hasPrimitiveTypeAnnotation(first) {
			return false
		}
		second := parameters[1].AsParameterDeclaration().Name()
		if second == nil || second.Kind != ast.KindIdentifier {
			return false
		}
		name := second.Text()
		return containsRef(name)
	}
	return false
}

// functionParameters returns a function-like node's parameters.
//
// `Node.ParameterList()` panics off function-like kinds, which the port brief names, so the kind is
// established here before any typed accessor is reached.
func functionParameters(node *ast.Node) []*ast.Node {
	switch node.Kind {
	case ast.KindFunctionDeclaration:
		return node.AsFunctionDeclaration().Parameters.Nodes
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().Parameters.Nodes
	case ast.KindArrowFunction:
		return node.AsArrowFunction().Parameters.Nodes
	}
	return nil
}

// isSpreadParameter reports whether a parameter is written with a spread.
//
// Not named `isRestParameter`, which `error_boundaries.go` in this package already defines.
func isSpreadParameter(parameter *ast.Node) bool {
	return parameter.AsParameterDeclaration().DotDotDotToken != nil
}

// containsRef answers upstream's `name.includes('ref') || name.includes('Ref')`.
//
// Written as two substring tests rather than as a case-insensitive comparison, because that is what
// upstream does and the two differ: a parameter named `REF` contains neither spelling and is
// therefore not a valid second parameter upstream, while a case-insensitive test would accept it.
func containsRef(name string) bool {
	return containsSubstring(name, "ref") || containsSubstring(name, "Ref")
}

// containsSubstring is `strings.Contains`, spelled out to keep this file's imports to the two
// packages the rule genuinely needs.
func containsSubstring(haystack string, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

// hasPrimitiveTypeAnnotation answers upstream's `isValidPropsAnnotation` inverted.
//
// Upstream lists the TypeScript annotation kinds that disqualify a first parameter from being
// props, and everything else, including no annotation at all, is valid. The list is reproduced
// rather than approximated as "not an object type", because upstream's own set admits shapes an
// approximation would decline: a type reference, an intersection, a union and a mapped type are all
// valid props annotations there.
//
// Measured both ways on React's rule: `function Component(props: string)` is silent and
// `function Component(props: {a: number})` reports.
func hasPrimitiveTypeAnnotation(parameter *ast.Node) bool {
	annotation := parameter.AsParameterDeclaration().Type
	if annotation == nil {
		return false
	}
	switch annotation.Kind {
	case ast.KindArrayType, ast.KindBigIntKeyword, ast.KindBooleanKeyword,
		ast.KindConstructorType, ast.KindFunctionType, ast.KindLiteralType,
		ast.KindNeverKeyword, ast.KindNumberKeyword, ast.KindStringKeyword,
		ast.KindSymbolKeyword, ast.KindTupleType:
		return true
	}
	return false
}
