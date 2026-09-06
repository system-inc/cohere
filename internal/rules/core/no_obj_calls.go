package core

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
)

// nonCallableGlobals are the namespace objects that exist to hold members and are not functions.
//
// This is oxc's set exactly: `Atomics | Intl | JSON | Math | Reflect`. ESLint's list also carries
// `Temporal`, and oxc is the port target, so the omission here is deliberate rather than an
// oversight. See the fixture named for it.
var nonCallableGlobals = map[string]bool{
	"Atomics": true,
	"Intl":    true,
	"JSON":    true,
	"Math":    true,
	"Reflect": true,
}

// globalThisName is the one object whose members this rule reaches through.
const globalThisName = "globalThis"

// NoObjCalls flags calling or constructing a global namespace object.
//
//	valid:   let area = r => 2 * Math.PI * r * r;
//	valid:   let object = JSON.parse("{}");
//	valid:   var Math; Math();
//	valid:   function foo(JSON) { new JSON(); }
//	invalid: let math = Math();
//	invalid: let newMath = new Math();
//	invalid: var x = globalThis.JSON();
//	invalid: let j = JSON; j();
//
// `Math`, `JSON`, `Reflect`, `Atomics` and `Intl` are ordinary objects holding methods, not
// functions and not constructors. Calling one throws a TypeError on the line it appears on, so this
// is a crash the code has never survived rather than a style preference.
//
// # Why this reads the checker
//
// Eighteen of upstream's thirty five clean cases are shadowing: `var Math; Math();`,
// `function foo(JSON) { new JSON(); }`, `{const Math = () => {}; {let obj = new Math();}}`. Each
// calls a local binding that happens to carry a global's spelling, and each is correct code. A rule
// matching on spelling alone fails more than half of its own clean corpus, so there is no cheap
// version of this rule that is also right.
//
// The predicate is `resolvesToAGlobal`, shared with `no-new-native-nonconstructor`: a global is
// declared in the TypeScript standard library, which is a declaration file, and any binding written
// in source is a shadow whatever its kind. Measured on every one of upstream's clean cases before
// this was written, with a probe reporting which side each landed on. The parameter, the `var`, the
// `let`, the `const` in a nested block and the destructured `BindingElement` all answer source.
//
// # What the member arm does not do
//
// `globalThis.Math()` is reported by reading the object's *spelling*, not by resolving it. That
// looks like the shortcut this rule was just argued out of, and it is there because the checker
// cannot answer the question: measured, `globalThis` does not resolve through `GetSymbolAtLocation`
// to a declaration-file declaration the way `Math` does, so a resolution test would answer false for
// the real one and report nothing at all. Upstream reads the spelling too, with
// `is_specific_id("globalThis")`, so a local named `globalThis` is a false positive in both. Pinned
// by a fixture rather than left to be discovered.
//
// Resolving the whole member expression instead is the trap in the other direction: `JSON.parse` is
// a declaration-file symbol, so `resolvesToAGlobal` answers true for the callee of `JSON.parse(foo)`,
// which is a clean case. The object identifier is what carries the discrimination, and only its
// spelling.
//
// # What it deliberately misses
//
// oxc ships nine cases commented out under `TODO: Fix these.` and this port reproduces every one of
// them. They are all the same shape: the alias walk reads an initializer that is either an
// identifier or a `globalThis.X` member and stops at anything else, so `var foo = bar ? baz: JSON;
// foo();` and `var foo = window.Atomics; foo();` resolve to nothing. `(globalThis?.Reflect)()` is
// missed for a different reason: its callee is a parenthesized expression, which matches neither
// arm. Calling `ast.SkipParentheses` there would close that gap and diverge from upstream in the
// direction of being more correct, which is still a divergence and is not taken here.
var NoObjCalls = rule.Rule{
	Name: "no-obj-calls",

	// See the doc above: eighteen of the clean cases are a local shadowing a global, and nothing
	// structural tells a shadow from the global it hides.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		// check reports one call or new expression if its callee names a non-callable global.
		//
		// The span is the whole call or new expression rather than the callee, matching upstream's
		// snapshot: `new Math().foo;` underlines `new Math()`, and `(new Math).foo();` underlines
		// the inner `new Math` at column two rather than the outer call.
		check := func(node *ast.Node, callee *ast.Node) {
			// No nil guard on `callee`. `Expression` is the one field of `CallExpression` and
			// `NewExpression` not marked optional, and the parser synthesizes a missing-identifier
			// node rather than leaving it empty: probed on `new;`, `new (`, `f(`, `a.();` and
			// `new .foo()`, every one of which builds a call node with a non-nil callee. A guard
			// here survived its own mutation because nothing can reach it.
			switch callee.Kind {
			case ast.KindIdentifier:
				// The name reported is the callee as written, not the global it resolves to.
				// Upstream passes `ident.name`, so `let m = globalThis.Math; new m();` says `m`.
				// ESLint answers differently, with a second message naming both; this follows oxc.
				// No separate empty check on `resolved`. A Go map answers false for a missing
				// key, so `nonCallableGlobals[""]` is already false and the lookup subsumes it;
				// an `== ""` arm here survived its own mutation for that reason.
				resolved := resolveGlobalBinding(ctx, callee)
				if !nonCallableGlobals[resolved] {
					return
				}
				ctx.ReportNode(node, objCallsMessage(callee.Text()))

			case ast.KindPropertyAccessExpression:
				name := globalThisMemberName(callee)
				if name == "" || !nonCallableGlobals[name] {
					return
				}
				ctx.ReportNode(node, objCallsMessage(name))
			}
		}

		return rule.Listeners{
			ast.KindCallExpression: func(node *ast.Node) {
				check(node, node.AsCallExpression().Expression)
			},
			ast.KindNewExpression: func(node *ast.Node) {
				check(node, node.AsNewExpression().Expression)
			},
		}
	},
}

// objCallsMessage builds the finding for one callee name.
func objCallsMessage(name string) rule.Message {
	return rule.Message{
		Id: "noObjCalls",
		Description: fmt.Sprintf(
			"This calls `%s`, which is an object holding members rather than a function. "+
				"Calling or constructing it throws a TypeError at runtime every time this line "+
				"is reached. Call a method on it instead.", name),
	}
}

// globalThisMemberName returns the property name of a `globalThis.X` access, or empty for anything
// else.
//
// Only the dotted form. `globalThis["Math"]()` is not in upstream's corpus in either direction, and
// upstream's `static_property_name()` would answer for it; reading only the dotted form is a
// narrowing, recorded here and pinned by a fixture rather than left implicit.
//
// The optional form `globalThis?.Reflect()` is the same node kind with a question-dot token, so it
// needs no arm of its own and upstream reports it too.
func globalThisMemberName(callee *ast.Node) string {
	access := callee.AsPropertyAccessExpression()
	if access.Expression == nil || access.Expression.Kind != ast.KindIdentifier {
		return ""
	}
	if access.Expression.Text() != globalThisName {
		return ""
	}
	name := access.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return ""
	}
	return name.Text()
}

// resolveGlobalBinding returns the name of the global an identifier ultimately denotes, or empty.
//
// Three answers, in the order they are asked:
//
//	the identifier resolves to a declaration-file declaration   it is the global; return its name
//	its declaration initializes it from another identifier      recur on that identifier
//	its declaration initializes it from `globalThis.X`          return X
//
// Everything else returns empty, which is upstream's `_ => None` and the source of every one of the
// misses documented on the rule.
//
// The recursion is bounded by `depth` rather than by a visited set, and that bound is load-bearing:
// `export const getConfig = getConfig;` is a real case from oxc issue 4389 whose declaration
// initializes the name from itself, so an unbounded walk on it never returns. Upstream guards the
// same case with `parent_ident.name != ident.name`, which stops the one-step cycle but not a
// two-step one; a depth bound stops both and costs nothing on the chains that are real, since
// upstream's longest is three links.
func resolveGlobalBinding(ctx rule.Context, identifier *ast.Node) string {
	return resolveGlobalBindingAtDepth(ctx, identifier, 0)
}

// maxAliasDepth bounds the alias walk. Upstream's longest chain is `let a = JSON; let b = a; let c =
// b; b();`, three links, so this is generous rather than tight; it exists to terminate on a cycle,
// not to model a real limit.
const maxAliasDepth = 8

func resolveGlobalBindingAtDepth(ctx rule.Context, identifier *ast.Node, depth int) string {
	if depth > maxAliasDepth {
		return ""
	}
	if ctx.TypeChecker == nil {
		return ""
	}

	// A name declared in a declaration file is the global itself, and this is the base case that
	// every clean shadowing case fails.
	if resolvesToAGlobal(ctx, identifier) {
		return identifier.Text()
	}

	symbol := ctx.TypeChecker.GetSymbolAtLocation(identifier)
	if symbol == nil || len(symbol.Declarations) == 0 {
		return ""
	}
	declaration := symbol.Declarations[0]
	if declaration.Kind != ast.KindVariableDeclaration {
		// A parameter, a function declaration, a class, an import, or the `BindingElement` of
		// `const {parse} = JSON`. None of them aliases a global in a form this walk reads, and each
		// is a shadow, so stopping here is the answer rather than a gap.
		return ""
	}

	variable := declaration.AsVariableDeclaration()
	// No check that the declaration's name is an identifier rather than a binding pattern. That
	// looks necessary, because `const {parse} = JSON; parse();` must not read `JSON` as an alias,
	// and it is not: probed across eight destructuring forms, object, array, renamed, defaulted,
	// nested and rest, and every one declares the bound name as a `BindingElement`, which the
	// declaration-kind guard above has already rejected. A guard here is subsumed by that one and
	// survived its own mutation for that reason.
	initializer := variable.Initializer
	if initializer == nil {
		return ""
	}

	switch initializer.Kind {
	case ast.KindIdentifier:
		// `let a = JSON; let b = a; b();`
		return resolveGlobalBindingAtDepth(ctx, initializer, depth+1)
	case ast.KindPropertyAccessExpression:
		// `let m = globalThis.Math; new m();`
		return globalThisMemberName(initializer)
	}
	return ""
}
