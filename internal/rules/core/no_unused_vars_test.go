package core

import (
	"encoding/json"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// TestNoUnusedVarsReportsAtTheBindingName asserts WHERE the finding points, which no message-id
// assertion can see. A rule naming the right message over the wrong span passes a complete fixture
// pair while being wrong, and this rule has several candidate spans to get wrong: the declaration
// statement, the declarator, and the name.
func TestNoUnusedVarsReportsAtTheBindingName(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"variable", "const forgotten = 1;", "forgotten"},
		{"parameter", "function f(leftBehind) {} f();", "leftBehind"},
		{"import", "import { unusedThing } from './m';", "unusedThing"},
		{"function declaration", "function neverCalled() {}", "neverCalled"},
		{"interface", "interface Orphan { a: string }", "Orphan"},
		{"catch binding", "try { console.log(1); } catch (caught) {}", "caught"},
		{"destructured element", "const { plucked } = source;", "plucked"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", testCase.source)
			if len(result.Diagnostics) != 1 {
				t.Fatalf("want exactly 1 finding, got %d", len(result.Diagnostics))
			}
			diagnostic := result.Diagnostics[0]
			source := result.SourceFile.Text()
			if got := source[diagnostic.Range.Pos():diagnostic.Range.End()]; got != testCase.want {
				t.Errorf("finding points at %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestNoUnusedVarsMessageIdAndDescription asserts the message against literals typed here rather
// than against the rule's own constant. Comparing a finding to the constant it was reported with is
// equality that cannot fail: both sides move together under mutation, so a message-text mutant
// survives a test written that way.
func TestNoUnusedVarsMessageIdAndDescription(t *testing.T) {
	result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", "const forgotten = 1;")
	if len(result.Diagnostics) != 1 {
		t.Fatalf("want 1 finding, got %d", len(result.Diagnostics))
	}
	if got := result.Diagnostics[0].Message.Id; got != "noUnusedVars" {
		t.Errorf("message id is %q, want %q", got, "noUnusedVars")
	}
	const wantPrefix = "This binding is declared and nothing ever reads it."
	if got := result.Diagnostics[0].Message.Description; len(got) < len(wantPrefix) ||
		got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("description does not start with the sentence naming the defect; got %q", got)
	}
}

// TestNoUnusedVarsRequiresTheTypedHarness pins that this rule answers nothing without a checker.
//
// A rule declaring NeedsTypeChecker and reading it without a nil guard does not crash on our shim,
// it goes SILENT, and every StaysSilent fixture then passes vacuously. That failure is invisible
// from a green suite, so it gets its own assertion with a control on the other side.
func TestNoUnusedVarsRequiresTheTypedHarness(t *testing.T) {
	const source = "const forgotten = 1;"
	if result := ruletest.Run(t, NoUnusedVars, "a.ts", source); len(result.Diagnostics) != 0 {
		t.Errorf("untyped harness: want silence, got %d findings", len(result.Diagnostics))
	}
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", source); len(result.Diagnostics) != 1 {
		t.Errorf("typed harness control: want 1 finding, got %d", len(result.Diagnostics))
	}
}

// TestDecodeNoUnusedVarsOptions routes through the exported decoder rather than building the
// struct, because the defaults are what the decoder applies and a hand-built struct bypasses every
// one of them. Three of this rule's defaults are not the zero value, and the inventory records the
// rule as having no options at all, so each default is pinned here individually.
func TestDecodeNoUnusedVarsOptions(t *testing.T) {
	t.Run("bare error supplies every default", func(t *testing.T) {
		decoded, err := DecodeNoUnusedVarsOptions(nil)
		if err != nil {
			t.Fatalf("decoding empty options failed: %v", err)
		}
		if decoded.Vars != "all" {
			t.Errorf("vars default is %q, want %q", decoded.Vars, "all")
		}
		if decoded.Args != "after-used" {
			t.Errorf("args default is %q, want %q — NOT \"all\"", decoded.Args, "after-used")
		}
		if decoded.CaughtErrors != "all" {
			t.Errorf("caughtErrors default is %q, want %q", decoded.CaughtErrors, "all")
		}
		if !decoded.varsPatternIsDefault || !decoded.argsPatternIsDefault {
			t.Error("an absent ignore pattern must record as default, which means leading underscore")
		}
	})

	t.Run("configured values override", func(t *testing.T) {
		decoded, err := DecodeNoUnusedVarsOptions(json.RawMessage(
			`{"args":"none","varsIgnorePattern":"^ignore"}`))
		if err != nil {
			t.Fatalf("decoding failed: %v", err)
		}
		if decoded.Args != "none" {
			t.Errorf("args is %q, want %q", decoded.Args, "none")
		}
		if decoded.varsPatternIsDefault {
			t.Error("an explicitly configured pattern must not record as default")
		}
	})
}

// TestNoUnusedVarsIgnorePatternDefaults pins the leading-underscore default through the rule rather
// than through the decoder, since that default is the single largest behavioral difference between
// oxc and ESLint and the one a later reader is most likely to "correct".
func TestNoUnusedVarsIgnorePatternDefaults(t *testing.T) {
	// Routed through the shared harness assertions rather than through a length comparison, so the
	// fixture-pair guard can see that this rule is shown both to fire and to stay quiet.
	ruletest.ExpectClean(t, ruletest.RunTyped(t, NoUnusedVars, "a.ts", "const _ignored = 1;"))
	ruletest.ExpectFindings(t,
		ruletest.RunTyped(t, NoUnusedVars, "a.ts", "const reported = 1;"), "noUnusedVars")

	// oxc ignores a leading underscore by default. ESLint reports it. oxc wins.
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", "const _deliberate = 1;"); len(result.Diagnostics) != 0 {
		t.Errorf("a leading underscore is ignored by default upstream; got %d findings",
			len(result.Diagnostics))
	}
	// The control, so the case above cannot pass because the rule sees nothing.
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", "const deliberate = 1;"); len(result.Diagnostics) != 1 {
		t.Errorf("control: want 1 finding on the same shape without the underscore, got %d",
			len(result.Diagnostics))
	}
	// A parameter named exactly `_` is NOT ignored, while a variable named `_` is. Upstream's
	// asymmetry at `ignored.rs:424`, reproduced.
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", "const _ = 1;"); len(result.Diagnostics) != 0 {
		t.Errorf("a variable named `_` is ignored; got %d findings", len(result.Diagnostics))
	}
}

// TestNoUnusedVarsJsxFactoryImport pins the exemption that removed 510 false positives from our own
// tree. `import React` in a `.tsx` looks untouched and is used by the JSX transform.
func TestNoUnusedVarsJsxFactoryImport(t *testing.T) {
	const source = "import React from 'react';\nexport const A = 1;\n"
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.tsx", source); len(result.Diagnostics) != 0 {
		t.Errorf("a React import in a .tsx file is exempt; got %d findings", len(result.Diagnostics))
	}
	// The control that makes the case above mean something: the same import in a `.ts` file, where
	// no JSX transform can reach it, still reports.
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", source); len(result.Diagnostics) != 1 {
		t.Errorf("control: the same import in a .ts file should report; got %d",
			len(result.Diagnostics))
	}
}

// TestNoUnusedVarsSeparatesShadowedBindingsBySymbol is the identity test.
//
// Name matching cannot separate these, and it fails in the quiet direction: it sees a read spelled
// like the binding and declines, missing the finding. Both orderings are pinned because a symbol
// can carry more than one declaration and the ordering is not something to assume.
func TestNoUnusedVarsSeparatesShadowedBindingsBySymbol(t *testing.T) {
	// The inner binding is read; the outer one is not. A name-matching rule reports neither.
	result := ruletest.RunTyped(t, NoUnusedVars, "a.ts",
		"const outer = 1; { const outer = 2; console.log(outer); }")
	if len(result.Diagnostics) != 1 {
		t.Fatalf("want exactly 1 finding for the unread outer binding, got %d", len(result.Diagnostics))
	}
	source := result.SourceFile.Text()
	if got := source[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]; got != "outer" {
		t.Errorf("finding points at %q, want the outer binding's name", got)
	}
	if position := result.Diagnostics[0].Range.Pos(); position > 20 {
		t.Errorf("finding is at offset %d, which is the INNER binding; identity was not used", position)
	}
}

// TestNoUnusedVarsDeclarationMergingExportsFromEitherSide pins that the export check asks every
// declaration of a merged symbol rather than the first.
//
// Both orderings, because an index-zero test passes one and fails the other, and which one it fails
// depends on which declaration the checker happens to list first.
func TestNoUnusedVarsDeclarationMergingExportsFromEitherSide(t *testing.T) {
	for _, source := range []string{
		"interface Merged {\n  bar: string;\n}\nexport const Merged = 'bar';\n",
		"export const Merged = 'bar';\ninterface Merged {\n  bar: string;\n}\n",
	} {
		if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", source); len(result.Diagnostics) != 0 {
			t.Errorf("a merged symbol exported from either side is exempt; got %d findings for %q",
				len(result.Diagnostics), source)
		}
	}
}

// TestNoUnusedVarsSkipsDeclarationFiles pins the `.d.ts` behavior, and says what it does NOT prove.
//
// It asserts the observable answer, which is real: a declaration file reports nothing while the same
// source as `.ts` reports both. It does NOT prove the suffix test in the rule is what produces that,
// and a mutant disabling that test survives this fixture. The cause is recorded at the rule's own
// line: typescript-go marks declarations in a `.d.ts` ambient, so the ambient guard already declines
// them and the suffix test is subsumed today.
//
// Kept as a behavior fixture rather than deleted, and labelled rather than left to imply more than
// it establishes, because a fixture whose name promises to guard a branch it cannot see is worse
// than no fixture: it stops the next reader from checking.
func TestNoUnusedVarsSkipsDeclarationFiles(t *testing.T) {
	const source = "interface Unreferenced {}\ntype AlsoUnreferenced = {};\n"
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.d.ts", source); len(result.Diagnostics) != 0 {
		t.Errorf("a .d.ts declares rather than defines and is skipped whole; got %d findings",
			len(result.Diagnostics))
	}
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", source); len(result.Diagnostics) != 2 {
		t.Errorf("control: the same source as .ts must report both; got %d",
			len(result.Diagnostics))
	}
}

// TestNoUnusedVarsExemptsMappedTypeKeys pins a deliberate divergence from ESLint.
//
// Also a surviving mutant, and for a reason worth recording: the only occurrence of this shape
// anywhere in reach was in our own tree rather than in the corpus, so the case that proves the
// guard matters had to be written from the real-tree finding it removed. Upstream oxc never reports
// it (`mod.rs:388`); ESLint does, measured with its own Linter API.
func TestNoUnusedVarsExemptsMappedTypeKeys(t *testing.T) {
	// NOT exported, and that is the whole point of the case. The first fixture written for this
	// survivor used an exported type and the mutant survived it again: an exported declaration is
	// already exempt for a different reason, so both the guarded and the unguarded rule reach
	// silence by different routes and no fixture over that shape can separate them. Measured on the
	// mutated rule, this input reports `K` and the exported one does not.
	const source = "type Slots<T extends string> = { [K in T]?: string };\n" +
		"const value: Slots<'a'> = {};\nconsole.log(value);\n"
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", source); len(result.Diagnostics) != 0 {
		t.Errorf("a mapped type key is always used by the type it builds; got %d findings",
			len(result.Diagnostics))
	}
	// The control: an ordinary type parameter that genuinely goes unused still reports, so the
	// case above cannot be passing because type parameters are exempt as a class. Not exported,
	// because an exported declaration is exempt for a different reason and would mask this.
	const control = "type Ignores<T extends string> = { a: string };\nconst v: Ignores<'x'> = { a: '' };\nconsole.log(v);\n"
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", control); len(result.Diagnostics) != 1 {
		t.Errorf("control: an unused type parameter outside a mapped type reports; got %d",
			len(result.Diagnostics))
	}
}

// TestNoUnusedVarsExportedContainerDoesNotExemptItsContents pins the change that closed the largest
// group of gaps.
//
// An exported function leaves the file; its parameter list does not, and no importer can read a
// parameter name. The same holds for a type parameter and for anything inside an exported
// namespace's body. Before this, the export climb walked from the binding all the way to the
// enclosing `export` and exempted everything under it.
//
// The controls matter as much as the cases: the exported binding ITSELF must still be exempt, or
// this test would pass on a rule that had simply stopped believing in exports.
func TestNoUnusedVarsExportedContainerDoesNotExemptItsContents(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		{"parameter of an exported function", "export function f(x, y) { return x; }", 1},
		{"type parameter of an exported interface", "export interface M<T> { a: string }", 1},
		{"binding inside an exported namespace", "export namespace N { function inner() {} }", 1},
		{"import equals inside an exported namespace",
			"namespace Foo { export const foo = 1; }\nexport namespace Bar { import TheFoo = Foo; }", 1},

		// Controls: the exported thing itself stays exempt, and a local inside an exported arrow
		// is still judged rather than being swept up by the same change.
		{"control, the exported function itself", "export function used() {}", 0},
		{"control, the exported interface itself", "export interface Used { a: string }", 0},
		{"control, exported const", "export const value = 1;", 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", testCase.source)
			if len(result.Diagnostics) != testCase.want {
				t.Errorf("want %d findings, got %d", testCase.want, len(result.Diagnostics))
			}
		})
	}
}

// TestNoUnusedVarsDiscardedReads pins the three positions where a read's value goes nowhere.
//
// Each has a control differing in exactly the one thing that decides it, because every one of these
// is a left-versus-right or a same-name-versus-different-name distinction where a rule that ignored
// the side would pass a one-sided fixture.
func TestNoUnusedVarsDiscardedReads(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		// A comma sequence yields its RIGHT operand, so only the left one is discarded.
		{"sequence left operand", "let a = 0; let b = (a, 0) + 1; f(b);", 1},
		{"control, sequence right operand", "let a = 0; let b = (0, a) + 1; f(b);", 0},

		// A self-update assigned back into its own binding observes nothing.
		{"update fed back to itself", "let a = 0; a = ++a;", 1},
		{"control, update fed to another binding", "let a = 0; let b = ++a; f(b);", 0},
		{"update through a cast", "let a = 0; a = a++ as any;", 1},

		// A closure assigned to the binding it reads.
		{"closure assigned to itself",
			"function foo(cb) { cb = function(a) { cb(1 + a); }; bar(not_cb); } foo();", 1},
		{"control, immediately invoked so it really runs",
			"function foo(cb) { cb = function(a) { return cb(1 + a); }(); } foo();", 0},
		{"control, parameter default shadows the outer binding",
			"let a; a = function(a = a) {}; a();", 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", testCase.source)
			if len(result.Diagnostics) != testCase.want {
				t.Errorf("want %d findings, got %d", testCase.want, len(result.Diagnostics))
			}
		})
	}
}

// TestNoUnusedVarsTypePositionsThatNameWithoutReading pins the type-level shapes where a binding is
// mentioned without being used.
func TestNoUnusedVarsTypePositionsThatNameWithoutReading(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		{"return type predicate", "export function f(a: unknown): a is string { return true; }", 1},
		{"asserts predicate", "function g(a: unknown): asserts a is string {} g('');", 1},
		{"rest parameter typed by itself", "function h(...args: typeof args) {} h();", 1},
		// Only `R` reports. `T` is genuinely read by the `T extends` on its left, which is worth
		// pinning: the first version of this fixture expected two and the rule was right.
		{"infer binding nothing uses",
			"export type F<T> = T extends infer R ? string : never;", 1},

		// Control: a parameter the body genuinely reads is not reported just because a predicate
		// also names it.
		{"control, predicate over a parameter the body reads",
			"export function f(a: unknown): a is string { return typeof a === 'string'; }", 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", testCase.source)
			if len(result.Diagnostics) != testCase.want {
				t.Errorf("want %d findings, got %d", testCase.want, len(result.Diagnostics))
			}
		})
	}
}

// TestNoUnusedVarsAmbientModuleExplicitExports pins the split between an ambient block that states
// an interface and one that does not, and the interface-versus-alias split inside an ambient block.
//
// Both are upstream distinctions visible only in its snapshot rather than in its source, and each
// one has a near-identical neighbour falling the other way.
func TestNoUnusedVarsAmbientModuleExplicitExports(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		{"ambient block with no exports is left alone",
			"declare module 'm' { type Unused = any; }", 0},
		{"ambient block WITH an export has its contents judged",
			"declare module 'm' { type Unused = any; const x = 1; export = x; }", 1},
		{"ambient interface type parameter is left alone",
			"declare module 'vitest' { interface Matchers<T> { toBeFoo(v: unknown): unknown; } }", 0},
		{"ambient type alias type parameter is judged",
			"declare module 'bun:test' { type Matchers2<T> = {} }", 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", testCase.source)
			if len(result.Diagnostics) != testCase.want {
				t.Errorf("want %d findings, got %d", testCase.want, len(result.Diagnostics))
			}
		})
	}
}

// TestNoUnusedVarsReExportFromModuleIsNotALocalRead pins the one-word difference between a specifier
// that reads a local binding and one that names something in another module.
func TestNoUnusedVarsReExportFromModuleIsNotALocalRead(t *testing.T) {
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts",
		"import { resolve } from \"path\";\nexport { resolve } from \"path\";"); len(result.Diagnostics) != 1 {
		t.Errorf("`export { x } from './m'` names the other module, so the import is unused; got %d",
			len(result.Diagnostics))
	}
	// The control is the same file with the `from` clause removed, where the specifier really does
	// read the local binding.
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts",
		"import { resolve } from \"path\";\nexport { resolve };"); len(result.Diagnostics) != 0 {
		t.Errorf("control: `export { x }` without a module specifier reads the local binding; got %d",
			len(result.Diagnostics))
	}
}

// TestNoUnusedVarsCaughtErrorsHaveNoDefaultIgnorePattern pins the asymmetry between the three ignore
// patterns: a variable or parameter named with a leading underscore is ignored by default, a caught
// error is not.
func TestNoUnusedVarsCaughtErrorsHaveNoDefaultIgnorePattern(t *testing.T) {
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", "try {} catch(_) { }"); len(result.Diagnostics) != 1 {
		t.Errorf("caughtErrorsIgnorePattern has no default, so `catch(_)` reports; got %d",
			len(result.Diagnostics))
	}
	// The control on the other side of the asymmetry: a VARIABLE named `_` is ignored.
	if result := ruletest.RunTyped(t, NoUnusedVars, "a.ts", "const _ = 1;"); len(result.Diagnostics) != 0 {
		t.Errorf("control: a variable named `_` is ignored by default; got %d", len(result.Diagnostics))
	}
	// And configuring a pattern turns it back off.
	options, err := DecodeNoUnusedVarsOptions(json.RawMessage(`{"caughtErrorsIgnorePattern":"^_"}`))
	if err != nil {
		t.Fatalf("decoding failed: %v", err)
	}
	if result := ruletest.RunTypedWithOptions(t, NoUnusedVars, "a.ts", "try {} catch(_) { }", options); len(result.Diagnostics) != 0 {
		t.Errorf("an explicit caughtErrorsIgnorePattern ignores it; got %d", len(result.Diagnostics))
	}
}
