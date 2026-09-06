package core

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// objCallsFile is where the fixtures pretend to live.
const objCallsFile = "/repository/source/ObjCalls.ts"

// The corpus is oxc's, copied rather than rewritten.
//
// Every case below is verbatim from `oxc/crates/oxc_linter/src/rules/eslint/no_obj_calls.rs`. The
// extractor reports what a hand count misses: **two Tester blocks**, 35 pass / 40 fail in the first
// and 1 pass / 1 fail in the second, with the snapshot recording **42 diagnostics from 40
// snapshotted inputs**. So one finding per input is wrong here, and the two inputs that report twice
// are carried below with two ids each rather than one.
//
// The second block is not a second corpus. It runs the same input `Math();` twice, once under
// oxlint's `globals: { Math: "off" }` configuration and once without, so what it tests is oxlint's
// harness config rather than anything the rule reads. cohere has no analogue for that configuration
// and the rule declares no options, so its pass case is unportable and its fail case is byte
// identical to the first case of block one. Recorded rather than dropped silently.
//
// `RunTypedFiles` rather than `Run`, and that is required rather than preferred. This rule declares
// `NeedsTypeChecker`; the plain harness supplies a nil checker, `resolvesToAGlobal` answers false
// for every identifier, and the rule goes completely silent. Every clean case below would then pass
// while proving nothing, and every case above would fail looking like a rule bug.
// `TestNoObjCallsRequiresTheTypedHarness` pins that so a later revert to `Run` fails loudly.
func TestNoObjCallsFires(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
		// wantIds carries one id per expected finding. Two of upstream's inputs report twice.
		wantIds []string
	}{
		{"a bare call", "Math();", []string{"noObjCalls"}},
		{"a call in an initializer", "var x = Math();", []string{"noObjCalls"}},
		{"a call as an argument", "f(Math());", []string{"noObjCalls"}},
		{"a call whose result is read", "Math().foo;", []string{"noObjCalls"}},
		{"new without an argument list", "new Math;", []string{"noObjCalls"}},
		{"new with an empty argument list", "new Math();", []string{"noObjCalls"}},
		{"new with an argument", "new Math(foo);", []string{"noObjCalls"}},
		{"new whose result is read", "new Math().foo;", []string{"noObjCalls"}},
		{"new inside parentheses, then called", "(new Math).foo();", []string{"noObjCalls"}},
		{"JSON called", "var x = JSON();", []string{"noObjCalls"}},
		{"JSON called with an argument", "x = JSON(str);", []string{"noObjCalls"}},
		{"JSON constructed", "var x = new JSON();", []string{"noObjCalls"}},
		// One of the two inputs the snapshot records twice: outer and inner callee are each flagged.
		{"a flagged call nested in a flagged call", "Math( JSON() );",
			[]string{"noObjCalls", "noObjCalls"}},
		{"Reflect called", "var x = Reflect();", []string{"noObjCalls"}},
		{"Reflect constructed", "var x = new Reflect();", []string{"noObjCalls"}},
		{"Atomics called", "var x = Atomics();", []string{"noObjCalls"}},
		{"Atomics constructed", "var x = new Atomics();", []string{"noObjCalls"}},
		{"Intl called", "var x = Intl();", []string{"noObjCalls"}},
		{"Intl constructed", "var x = new Intl();", []string{"noObjCalls"}},
		{"through globalThis", "var x = globalThis.Math();", []string{"noObjCalls"}},
		{"constructed through globalThis", "var x = new globalThis.Math();", []string{"noObjCalls"}},
		{"through globalThis as an argument", "f(globalThis.Math());", []string{"noObjCalls"}},
		{"through globalThis, result read", "globalThis.Math().foo;", []string{"noObjCalls"}},
		{"constructed through globalThis, result read", "new globalThis.Math().foo;",
			[]string{"noObjCalls"}},
		{"JSON through globalThis", "var x = globalThis.JSON();", []string{"noObjCalls"}},
		{"JSON through globalThis with an argument", "x = globalThis.JSON(str);",
			[]string{"noObjCalls"}},
		// The second input the snapshot records twice.
		{"two globalThis calls nested", "globalThis.Math( globalThis.JSON() );",
			[]string{"noObjCalls", "noObjCalls"}},
		{"Reflect through globalThis", "var x = globalThis.Reflect();", []string{"noObjCalls"}},
		{"Reflect constructed through globalThis without an argument list",
			"var x = new globalThis.Reflect;", []string{"noObjCalls"}},
		{"Atomics through globalThis", "var x = globalThis.Atomics();", []string{"noObjCalls"}},
		{"Intl through globalThis", "var x = globalThis.Intl();", []string{"noObjCalls"}},
		{"Intl constructed through globalThis without an argument list",
			"var x = new globalThis.Intl;", []string{"noObjCalls"}},
		// An optional-chained member callee is still a member callee, and upstream reports it.
		{"through an optional globalThis", "var x = globalThis?.Reflect();", []string{"noObjCalls"}},
		// The alias cases. These are why the identifier arm walks the declaration's initializer
		// rather than stopping at the first name it sees.
		{"a one-step alias", "let j = JSON; j();", []string{"noObjCalls"}},
		{"a three-step alias chain", "let a = JSON; let b = a; let c = b; b();",
			[]string{"noObjCalls"}},
		{"an alias through a globalThis member", "let m = globalThis.Math; new m();",
			[]string{"noObjCalls"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectFindings(t,
				rule_testing.RunTypedFiles(t, NoObjCalls,
					map[string]string{objCallsFile: testCase.sourceText}, objCallsFile),
				testCase.wantIds...)
		})
	}
}

// The clean corpus, and it is where this rule is won or lost.
//
// **Eighteen of upstream's thirty five pass cases are shadowing.** `var Math; Math();` and
// `function foo(JSON) { new JSON(); }` call a local binding that happens to carry a global's
// spelling, and reporting them is a false positive on correct code. A port that matched on spelling
// alone would fail more than half of its own clean corpus, which is the measurement that decided
// this rule reads the checker.
//
// The rest split into member accesses that are correct uses of the global (`Math.random()`,
// `JSON.parse(foo)`), calls on a different object that merely ends in the same name
// (`foo.Math()`), and three cases from upstream bug reports.
func TestNoObjCallsStaysSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"naming the global without calling it", "var x = Math;"},
		{"calling a method on it", "var x = Math.random();"},
		{"reading a property", "var x = Math.PI;"},
		{"a same-named property of another object", "var x = foo.Math();"},
		{"constructing a same-named property of another object", "var x = new foo.Math();"},
		{"new on a property, no argument list", "var x = new Math.foo;"},
		{"new on a property", "var x = new Math.foo();"},
		{"JSON.parse", "JSON.parse(foo)"},
		{"new on JSON.parse", "new JSON.parse"},
		{"Reflect.get", "Reflect.get(foo, 'x')"},
		{"new on a Reflect property", "new Reflect.foo(a, b)"},
		{"Atomics.load", "Atomics.load(foo, 0)"},
		{"new on an Atomics property", "new Atomics.foo()"},
		{"new on an Intl property", "new Intl.Segmenter()"},
		{"an Intl method", "Intl.foo()"},

		// Shadowing. Every one of these is a call on a local binding, not on the global.
		{"a var shadow called", "var Math; Math();"},
		{"a var shadow constructed", "var Math; new Math();"},
		{"a let shadow called", "let JSON; JSON();"},
		{"a let shadow constructed", "let JSON; new JSON();"},
		{"a const shadow in a block", "if (foo) { const Reflect = 1; Reflect(); }"},
		{"a const shadow in a block, constructed", "if (foo) { const Reflect = 1; new Reflect(); }"},
		{"a parameter shadow", "function foo(Math) { Math(); }"},
		{"a parameter shadow constructed", "function foo(JSON) { new JSON(); }"},
		{"an Atomics parameter shadow", "function foo(Atomics) { Atomics(); }"},
		{"a shadow two blocks deep",
			"function foo() { if (bar) { let Atomics; if (baz) { new Atomics(); } } }"},
		{"a var shadow inside a function", "function foo() { var JSON; JSON(); }"},
		{"a shadow initialized from a call", "function foo() { var Atomics = bar(); var baz = Atomics(5); }"},
		// A conditional initializer. oxc's resolver falls to None here and so does ours, which is
		// the correct answer for a different reason: `construct` is a local binding either way.
		{"a local initialized from a conditional",
			`var construct = typeof Reflect !== "undefined" ? Reflect.construct : undefined; construct();`},
		{"an Intl parameter shadow", "function foo(Intl) { Intl(); }"},
		{"an Intl const shadow", "if (foo) { const Intl = 1; Intl(); }"},
		{"an Intl const shadow constructed", "if (foo) { const Intl = 1; new Intl(); }"},
		// https://github.com/oxc-project/oxc/pull/508#issuecomment-1618850742
		{"a shadow in an outer block, called in an inner one",
			"{const Math = () => {}; {let obj = new Math();}}"},
		// A destructured binding. The declaration is a BindingElement, not a VariableDeclaration
		// with an identifier name, so the alias walk must not follow it to `JSON`.
		{"a destructured binding from JSON", "{const {parse} = JSON;parse('{}')}"},
		// https://github.com/oxc-project/oxc/issues/4389 — a self-referential initializer, which a
		// naive alias walk recurses on forever.
		{"a self-referential export", "export const getConfig = getConfig;\n        getConfig();"},
		{"an alias shadowed in an inner scope",
			"let j = JSON;\n        function foo() {\n            let j = x => x;\n            return x();\n        }"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectClean(t,
				rule_testing.RunTypedFiles(t, NoObjCalls,
					map[string]string{objCallsFile: testCase.sourceText}, objCallsFile))
		})
	}
}

// Cases upstream does not carry, each from reading our code rather than oxc's.
func TestNoObjCallsOurOwnCases(t *testing.T) {
	t.Parallel()

	t.Run("upstream's TODO misses are reproduced rather than improved on", func(t *testing.T) {
		// oxc ships these commented out under `// TODO: Fix these.` and `// TODO: Fix.`. They are
		// known false negatives, and matching upstream means missing them too. Asserted so that a
		// later change closing one of these gaps is a visible decision rather than a silent drift.
		for _, sourceText := range []string{
			// A conditional initializer: the alias walk reads only an identifier or a globalThis
			// member, so it stops here.
			"var foo = bar ? baz: JSON; foo();",
			"var foo = bar ? baz: JSON; new foo();",
			// A `window.X` initializer rather than a `globalThis.X` one.
			"var foo = window.Atomics; foo();",
			"var foo = window.Intl; new foo;",
			// The parenthesis case. The callee is a ParenthesizedExpression, which matches neither
			// arm, so nothing is reported. A port reflexively calling `ast.SkipParentheses` on the
			// callee would report this and diverge from upstream in the direction of being more
			// correct, which is still a divergence.
			"var x = (globalThis?.Reflect)();",
		} {
			rule_testing.ExpectClean(t,
				rule_testing.RunTypedFiles(t, NoObjCalls,
					map[string]string{objCallsFile: sourceText}, objCallsFile))
		}
	})

	t.Run("the set is the whole discrimination on both arms", func(t *testing.T) {
		// Written for two surviving mutants, one per arm. Dropping the `nonCallableGlobals` lookup
		// left both arms scoring green against the whole verbatim corpus, because upstream has no
		// case that calls a *different* global or a different `globalThis` member. Without these
		// the rule would report `parseInt()` and `globalThis.isNaN()` and nothing would notice.
		for _, sourceText := range []string{
			// Identifier arm: a global that resolves exactly as `Math` does and is callable.
			"parseInt('1');",
			"isNaN(1);",
			// The alias walk reaching a callable global is the same hole one step further out.
			"let p = parseInt; p('1');",
			// Member arm: a `globalThis` member that is not in the set.
			"globalThis.parseInt('1');",
			"globalThis.isNaN(1);",
			"new globalThis.Date();",
			// A constructible global, so that "not in the set" is tested under `new` too.
			"new Date();",
		} {
			rule_testing.ExpectClean(t,
				rule_testing.RunTypedFiles(t, NoObjCalls,
					map[string]string{objCallsFile: sourceText}, objCallsFile))
		}
	})

	t.Run("Temporal is not in oxc's set and is not in ours", func(t *testing.T) {
		// ESLint's list is Atomics, JSON, Math, Reflect, Intl and Temporal. oxc's omits Temporal,
		// and oxc is the port target, so this stays clean deliberately rather than by oversight.
		rule_testing.ExpectClean(t,
			rule_testing.RunTypedFiles(t, NoObjCalls,
				map[string]string{objCallsFile: "var x = Temporal;"}, objCallsFile))
	})

	t.Run("the finding spans the whole call, not the callee", func(t *testing.T) {
		// `ExpectFindings` asserts ids and count and nothing else, so a rule pointing at the wrong
		// place passes a complete fixture pair. The snapshot underlines the whole call or new
		// expression, which is what these compare against.
		spanCases := []struct {
			sourceText string
			want       []string
		}{
			{"Math();", []string{"Math()"}},
			{"var x = Math();", []string{"Math()"}},
			// No argument list: the span still ends at the callee.
			{"new Math;", []string{"new Math"}},
			{"new Math(foo);", []string{"new Math(foo)"}},
			// The result is read, and the span covers the call rather than the member access.
			{"new Math().foo;", []string{"new Math()"}},
			{"Math().foo;", []string{"Math()"}},
			// The inner new expression is reported, at column 2, and the outer `.foo()` is not.
			{"(new Math).foo();", []string{"new Math"}},
			{"var x = globalThis.Math();", []string{"globalThis.Math()"}},
			{"new globalThis.Math().foo;", []string{"new globalThis.Math()"}},
			{"var x = globalThis?.Reflect();", []string{"globalThis?.Reflect()"}},
			// Both findings, outer first, in the order the walk visits them.
			{"Math( JSON() );", []string{"Math( JSON() )", "JSON()"}},
			{"globalThis.Math( globalThis.JSON() );",
				[]string{"globalThis.Math( globalThis.JSON() )", "globalThis.JSON()"}},
			// The alias reports at the call site, not at the declaration that named the global.
			{"let j = JSON; j();", []string{"j()"}},
			{"let m = globalThis.Math; new m();", []string{"new m()"}},
		}
		for _, spanCase := range spanCases {
			result := rule_testing.RunTypedFiles(t, NoObjCalls,
				map[string]string{objCallsFile: spanCase.sourceText}, objCallsFile)
			if len(result.Diagnostics) != len(spanCase.want) {
				t.Fatalf("%q: got %d findings, want %d",
					spanCase.sourceText, len(result.Diagnostics), len(spanCase.want))
			}
			for index, want := range spanCase.want {
				reported := spanCase.sourceText[result.Diagnostics[index].Range.Pos():result.Diagnostics[index].Range.End()]
				if reported != want {
					t.Errorf("%q finding %d: reported %q, want %q",
						spanCase.sourceText, index, reported, want)
				}
			}
		}
	})

	t.Run("the message names the callee as written, not the global it resolves to", func(t *testing.T) {
		// Upstream passes `ident.name`, so an alias reports the alias. `let m = globalThis.Math;
		// new m();` snapshots as "`m` is not a function", not "`Math`". ESLint answers differently
		// here, with a second message naming both; this follows oxc.
		result := rule_testing.RunTypedFiles(t, NoObjCalls,
			map[string]string{objCallsFile: "let m = globalThis.Math; new m();"}, objCallsFile)
		if len(result.Diagnostics) != 1 {
			t.Fatalf("got %d findings, want 1", len(result.Diagnostics))
		}
		if !strings.Contains(result.Diagnostics[0].Message.Description, "`m`") {
			t.Errorf("description %q does not name the callee as written",
				result.Diagnostics[0].Message.Description)
		}
	})

	t.Run("a shadowed globalThis is a false positive, same as upstream", func(t *testing.T) {
		// oxc tests the object's spelling with `is_specific_id("globalThis")` and so does this
		// port, so a local named `globalThis` is reported. Measured rather than assumed: the
		// checker cannot answer this one, because the real `globalThis` does not resolve to a
		// declaration-file declaration through `GetSymbolAtLocation`, so the shadow and the global
		// are indistinguishable by the mechanism the identifier arm uses.
		rule_testing.ExpectFindings(t,
			rule_testing.RunTypedFiles(t, NoObjCalls,
				map[string]string{objCallsFile: "function f(globalThis: any) { globalThis.Math(); }"},
				objCallsFile),
			"noObjCalls")
	})

	t.Run("a computed member access through globalThis is not reported", func(t *testing.T) {
		// oxc reads `static_property_name()`, which answers for a string-literal subscript as well
		// as a dotted one. Our member arm reads only the dotted form, so this is a deliberate
		// narrowing: `globalThis["Math"]()` is not in upstream's corpus in either direction, and
		// reporting it would be a divergence nothing measured.
		rule_testing.ExpectClean(t,
			rule_testing.RunTypedFiles(t, NoObjCalls,
				map[string]string{objCallsFile: `globalThis["Math"]();`}, objCallsFile))
	})
}

// The typed harness is not a preference, and this test is why.
//
// `NeedsTypeChecker` is what gets `ctx.TypeChecker` populated. Under `rule_testing.Run` it is nil,
// `resolvesToAGlobal` returns false for every identifier, and the rule reports nothing at all. Every
// case in `TestNoObjCallsStaysSilent` would still pass, vacuously, so the clean half of the corpus
// cannot detect the mistake. This asserts the declaration and the silence together, so a revert to
// the plain harness fails here rather than passing everywhere.
func TestNoObjCallsRequiresTheTypedHarness(t *testing.T) {
	t.Parallel()

	if !NoObjCalls.NeedsTypeChecker {
		t.Fatal("NoObjCalls must declare NeedsTypeChecker; the identifier arm resolves nothing without it")
	}

	// The identifier arm goes silent without a checker. Asserted rather than described.
	rule_testing.ExpectClean(t, rule_testing.Run(t, NoObjCalls, objCallsFile, "Math();"))

	// And the member arm does not, because it reads spelling rather than resolution. Recorded so
	// that "the untyped harness is useless for this rule" is stated at the precision it is true at:
	// half the rule still fires, which is what makes the failure look like a rule bug.
	rule_testing.ExpectFindings(t,
		rule_testing.Run(t, NoObjCalls, objCallsFile, "globalThis.Math();"), "noObjCalls")
}

// The registry entry, so that a rule that passes every fixture and lints zero files fails here.
func TestNoObjCallsIsRegistered(t *testing.T) {
	t.Parallel()

	var found *rule.Registration
	for index, registration := range rule.Registered() {
		if registration.Rule.Name == "no-obj-calls" {
			found = &rule.Registered()[index]
			break
		}
	}
	if found == nil {
		t.Fatal("no-obj-calls is absent from the registry")
	}
	if !found.Rule.NeedsTypeChecker {
		t.Error("the registered copy does not declare NeedsTypeChecker")
	}
}
