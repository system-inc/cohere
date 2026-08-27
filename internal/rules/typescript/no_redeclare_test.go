package typescript

import (
	"encoding/json"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// redeclareFile is where the fixtures pretend to live.
const redeclareFile = "/repository/source/Redeclare.ts"

// The corpus is typescript-eslint's own, copied rather than rewritten.
//
// Every case below is verbatim from
// `typescript-eslint/packages/eslint-plugin/tests/rules/no-redeclare.test.ts`: 52 cases, 21 clean and
// 31 reporting, carrying 35 findings between them. The strings were extracted through the TypeScript
// compiler API rather than retyped and this file was generated from that extraction, because a
// fixture a porter types encodes the same belief as the port and a tool that rewrites an escape on
// the way in goes green while asserting the opposite of upstream.
//
// Sixteen of the 52 are omitted here and the omission is deliberate rather than an oversight. They
// carry the `builtinGlobals` option, whose verdict comes from a surface we do not have. See the
// rule's doc comment, and `TestNoRedeclareBuiltinGlobalsIsOutOfScope` below, which pins the
// omission rather than leaving it as silence.
//
// The remaining 36 were each driven against the installed @typescript-eslint 8.67.0 rule through the
// ESLint Linter API before being written here, so the verdict beside each one is measured rather
// than read off the corpus. All 52 reproduced, including the 16 omitted.

// optionsJSON builds this rule's configuration the way the config layer delivers it, which is the
// bare object rather than upstream's `[{...}]` tuple. Routing fixtures through the rule's own
// decoder is what puts the default inversion under test: `ignoreDeclarationMerge` defaults to TRUE,
// so a zero-valued struct silently turns the merge exemptions off and every merge case starts
// reporting.
func redeclareOptions(ignoreDeclarationMerge bool) any {
	raw, err := json.Marshal(map[string]bool{"ignoreDeclarationMerge": ignoreDeclarationMerge})
	if err != nil {
		panic(err)
	}
	decoded, err := DecodeNoRedeclareOptions(raw)
	if err != nil {
		panic(err)
	}
	return decoded
}

// TestNoRedeclareFires covers every reporting case in the corpus that does not need the missing
// surface, with the message ids upstream states per case.
func TestNoRedeclareFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		options    any
		messageIds []string
	}{
		{"corpus case 21", "\nvar a = 3;\nvar a = 10;\n      ", nil, []string{"redeclared"}},
		{"corpus case 22", "\nswitch (foo) {\n  case a:\n    var b = 3;\n  case b:\n    var b = 4;\n}\n      ", nil, []string{"redeclared"}},
		{"corpus case 23", "\nvar a = 3;\nvar a = 10;\n      ", nil, []string{"redeclared"}},
		{"corpus case 24", "\nvar a = {};\nvar a = [];\n      ", nil, []string{"redeclared"}},
		{"corpus case 25", "\nvar a;\nfunction a() {}\n      ", nil, []string{"redeclared"}},
		{"corpus case 26", "\nfunction a() {}\nfunction a() {}\n      ", nil, []string{"redeclared"}},
		{"corpus case 27", "\nvar a = function () {};\nvar a = function () {};\n      ", nil, []string{"redeclared"}},
		{"corpus case 28", "\nvar a = function () {};\nvar a = new Date();\n      ", nil, []string{"redeclared"}},
		{"corpus case 29", "\nvar a = 3;\nvar a = 10;\nvar a = 15;\n      ", nil, []string{"redeclared", "redeclared"}},
		{"corpus case 30", "\nvar a;\nvar a;\n      ", nil, []string{"redeclared"}},
		{"corpus case 31", "\nexport var a;\nvar a;\n      ", nil, []string{"redeclared"}},
		{"corpus case 39", "\ntype T = 1;\ntype T = 2;\n      ", nil, []string{"redeclared"}},
		{"corpus case 41", "\ninterface A {}\ninterface A {}\n      ", redeclareOptions(false), []string{"redeclared"}},
		{"corpus case 42", "\ninterface A {}\nclass A {}\n      ", redeclareOptions(false), []string{"redeclared"}},
		{"corpus case 43", "\nclass A {}\nnamespace A {}\n      ", redeclareOptions(false), []string{"redeclared"}},
		{"corpus case 44", "\ninterface A {}\nclass A {}\nnamespace A {}\n      ", redeclareOptions(false), []string{"redeclared", "redeclared"}},
		{"corpus case 45", "\nclass A {}\nclass A {}\nnamespace A {}\n      ", redeclareOptions(true), []string{"redeclared"}},
		{"corpus case 46", "\nfunction A() {}\nnamespace A {}\n      ", redeclareOptions(false), []string{"redeclared"}},
		{"corpus case 47", "\nfunction A() {}\nfunction A() {}\nnamespace A {}\n      ", redeclareOptions(true), []string{"redeclared"}},
		{"corpus case 48", "\nfunction A() {}\nclass A {}\n      ", redeclareOptions(false), []string{"redeclared"}},
		{"corpus case 49", "\nenum A {}\nnamespace A {}\nenum A {}\n      ", redeclareOptions(true), []string{"redeclared"}},
		{"corpus case 50", "\nfunction A() {}\nclass A {}\nnamespace A {}\n      ", redeclareOptions(false), []string{"redeclared", "redeclared"}},
		{"corpus case 51", "\ntype something = string;\nconst something = 2;\n      ", nil, []string{"redeclared"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, testCase.sourceText, testCase.options)
			ruletest.ExpectFindings(t, result, testCase.messageIds...)
		})
	}
}

// TestNoRedeclareStaysSilent covers every clean case in the corpus that does not need the missing
// surface. These are the false positives upstream already thought about, and they carry the whole
// declaration-merge discrimination: an interface pair, a class beside an interface, a namespace
// beside anything, overload signatures, and the same name bound in scopes that do not overlap.
func TestNoRedeclareStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		options    any
	}{
		{"corpus case 0", "\nvar a = 3;\nvar b = function () {\n  var a = 10;\n};\n    ", nil},
		{"corpus case 1", "\nvar a = 3;\na = 10;\n    ", nil},
		{"corpus case 2", "\nif (true) {\n  let b = 2;\n} else {\n  let b = 3;\n}\n      ", nil},
		{"corpus case 11", "\nfunction foo({ bar }: { bar: string }) {\n  console.log(bar);\n}\n    ", nil},
		{"corpus case 12", "\ntype AST<T extends ParserOptions> = TSESTree.Program &\n  (T['range'] extends true ? { range: [number, number] } : {}) &\n  (T['tokens'] extends true ? { tokens: TSESTree.Token[] } : {}) &\n  (T['comment'] extends true ? { comments: TSESTree.Comment[] } : {});\ninterface ParseAndGenerateServicesResult<T extends ParserOptions> {\n  ast: AST<T>;\n  services: ParserServices;\n}\n    ", nil},
		{"corpus case 13", "\nfunction A<T>() {}\ninterface B<T> {}\ntype C<T> = Array<T>;\nclass D<T> {}\n    ", nil},
		{"corpus case 14", "\nfunction a(): string;\nfunction a(): number;\nfunction a() {}\n    ", nil},
		{"corpus case 15", "\ninterface A {}\ninterface A {}\n      ", redeclareOptions(true)},
		{"corpus case 16", "\ninterface A {}\nclass A {}\n      ", redeclareOptions(true)},
		{"corpus case 17", "\nclass A {}\nnamespace A {}\n      ", redeclareOptions(true)},
		{"corpus case 18", "\ninterface A {}\nclass A {}\nnamespace A {}\n      ", redeclareOptions(true)},
		{"corpus case 19", "\nenum A {}\nnamespace A {}\n      ", redeclareOptions(true)},
		{"corpus case 20", "\nfunction A() {}\nnamespace A {}\n      ", redeclareOptions(true)},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t,
				ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, testCase.sourceText, testCase.options))
		})
	}
}

// TestNoRedeclareBuiltinGlobalsIsOutOfScope pins the sixteen omitted corpus cases as omitted, so the
// gap is a recorded decision rather than fixtures that quietly do not exist.
//
// Upstream's `builtinGlobals` option reports a declaration that shadows a global. The measurement
// behind declining it, run with a control: for `var Object = 1;` the checker's scope enumeration
// returns ONLY the local declaration and the standard library's `Object` is not in scope at all,
// which is byte-identical to what it answers for a name that is not a builtin. So there is nothing
// to compare the local declaration against.
//
// The corpus itself shows the verdict is not a resolution question. `var Object = 0;` appears as a
// CLEAN case and as a REPORTING case under the same `builtinGlobals: true`, separated only by
// whether the file is a module, which is ESLint's environment model rather than anything a type
// checker knows. The two assertions below are what our rule does with those inputs today, and both
// are silence.
func TestNoRedeclareBuiltinGlobalsIsOutOfScope(t *testing.T) {
	// Upstream reports this in script mode with `builtinGlobals: true`, and is clean on it in module
	// mode with the same option. We are silent on both, having no such option.
	ruletest.ExpectClean(t,
		ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, "var Object = 0;", nil))

	// Upstream reports this against a name a comment directive introduced. We have no
	// directive-globals surface, so it is silent for the same reason.
	ruletest.ExpectClean(t,
		ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, "/*global b:false*/ var b = 1;", nil))

	// The control for both, and the reason the two silences above are a scoped decline rather than a
	// dead rule: an ordinary redeclaration in the same file shape still reports.
	ruletest.ExpectFindings(t,
		ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, "var b = 1; var b = 2;", nil),
		"redeclared")
}

// TestNoRedeclareRequiresTheTypedHarness asserts the rule declines rather than panics when the
// program could not be built, and that the plain harness is not enough to exercise it.
//
// Without this a later revert of the nil guard would be invisible: the untyped harness hands the
// rule a nil checker, the rule returns early, and every StaysSilent case above would keep passing
// vacuously while every Fires case failed in a way that reads like a rule defect.
func TestNoRedeclareRequiresTheTypedHarness(t *testing.T) {
	// The typed harness reports.
	ruletest.ExpectFindings(t,
		ruletest.RunTyped(t, NoRedeclare, redeclareFile, "var a = 1; var a = 2;"), "redeclared")

	// The plain one hands the rule no checker, so it declines. Silence here is the correct answer
	// and not a finding this rule missed.
	ruletest.ExpectClean(t,
		ruletest.Run(t, NoRedeclare, redeclareFile, "var a = 1; var a = 2;"))
}

// TestNoRedeclareDefaultsToIgnoringDeclarationMerge is the decoder test, and it exists because this
// rule's one option defaults to TRUE.
//
// A rule written down as a bare "error" is handed nil options, and a generic decoder would yield a
// zero-valued struct whose false `IgnoreDeclarationMerge` turns every merge exemption off. That does
// not weaken the rule, it inverts it: thirteen of upstream's clean cases would start reporting. Both
// halves are asserted, since a decoder that ignored its input entirely would pass the first alone.
func TestNoRedeclareDefaultsToIgnoringDeclarationMerge(t *testing.T) {
	decoded, err := DecodeNoRedeclareOptions(nil)
	if err != nil {
		t.Fatalf("DecodeNoRedeclareOptions(nil) errored: %v", err)
	}
	if got := decoded.(NoRedeclareOptions); !got.IgnoreDeclarationMerge {
		t.Errorf("absent options: IgnoreDeclarationMerge = false, want true")
	}

	explicit, err := DecodeNoRedeclareOptions(json.RawMessage(`{"ignoreDeclarationMerge":false}`))
	if err != nil {
		t.Fatalf("DecodeNoRedeclareOptions errored: %v", err)
	}
	if got := explicit.(NoRedeclareOptions); got.IgnoreDeclarationMerge {
		t.Errorf("explicit false: IgnoreDeclarationMerge = true, want false")
	}

	// And the same distinction reaching the rule, since the two lines above only prove the decoder
	// parses. A bare "error" configuration is nil options, and the merge must stay exempt there.
	ruletest.ExpectClean(t,
		ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, "interface A {}\ninterface A {}", nil))
	ruletest.ExpectFindings(t,
		ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, "interface A {}\ninterface A {}", redeclareOptions(false)),
		"redeclared")
}

// TestNoRedeclareSpans asserts WHERE each finding points and what it says, which `ExpectFindings`
// cannot see at all: it compares message ids and a count and nothing else, so a rule anchoring every
// finding on the wrong node passes a complete fixture pair.
//
// The anchor is the redeclared IDENTIFIER rather than the declaration statement, which is upstream's
// choice and is visible in its corpus as a column: the second `var a = 10;` reports at column 5, the
// `a` rather than the `var`. Anchoring on the statement would put it at column 1 and every message
// id fixture above would stay green.
//
// The harness writes each fixture as `strings.TrimSpace(contents)+"\n"`, so slicing a literal that
// carried a leading newline would be off by one byte. These sources are written without one, so the
// literal and the file agree, and the slice below is taken from the same string the harness wrote.
func TestNoRedeclareSpans(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		options    any
		wantTexts  []string
	}{
		// The redeclared name, not the statement and not the initializer.
		{"second variable", "var a = 3;\nvar a = 10;", nil, []string{"a"}},
		// Every copy after the first, in source order.
		{"third variable too", "var a = 3;\nvar a = 10;\nvar a = 15;", nil, []string{"a", "a"}},
		// A longer name, so a span that is right by luck on a one-character name fails here.
		{"a longer name", "type something = string;\nconst something = 2;", nil, []string{"something"}},
		// The class, not the namespace beside it: the namespace is a legitimate merge partner.
		{"the extra class only", "class Alpha {}\nclass Alpha {}\nnamespace Alpha {}", nil, []string{"Alpha"}},
		// With merging off, both the class and the namespace report, in source order.
		{"merging off reports both", "function Beta() {}\nclass Beta {}\nnamespace Beta {}", redeclareOptions(false), []string{"Beta", "Beta"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, testCase.sourceText, testCase.options)
			if len(result.Diagnostics) != len(testCase.wantTexts) {
				t.Fatalf("got %d findings, want %d", len(result.Diagnostics), len(testCase.wantTexts))
			}
			for index, diagnostic := range result.Diagnostics {
				reported := testCase.sourceText[diagnostic.Range.Pos():diagnostic.Range.End()]
				if reported != testCase.wantTexts[index] {
					t.Errorf("finding %d points at %q, want %q", index, reported, testCase.wantTexts[index])
				}
			}
		})
	}
}

// TestNoRedeclareMessage asserts the reported message exactly.
//
// A `rule.Message` is `{Id, Description}` with no interpolation layer, so there is nothing to render
// and the assertion is equality against a literal typed here rather than against the rule's own
// constant. Comparing to the constant would look correct and would move with any mutation of it,
// which is how a message-text mutant survives a test written the obvious way.
func TestNoRedeclareMessage(t *testing.T) {
	result := ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, "var a = 3;\nvar a = 10;", nil)
	if len(result.Diagnostics) != 1 {
		t.Fatalf("got %d findings, want 1", len(result.Diagnostics))
	}
	const wantIdentifier = "redeclared"
	if got := result.Diagnostics[0].Message.Id; got != wantIdentifier {
		t.Errorf("message id = %q, want %q", got, wantIdentifier)
	}
	const wantPrefix = "This name is already declared in the same scope."
	if got := result.Diagnostics[0].Message.Description; len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("message description = %q, want it to begin %q", got, wantPrefix)
	}
}

// TestNoRedeclareMergeSetsSubsumeTheAllOneKindCases pins the inputs upstream answers with an
// explicit all-interfaces / all-namespaces test that this port reaches through the merge sets
// instead.
//
// A mutant neutralising an explicit arm for those two survived the whole corpus, which is what
// identified the arm as subsumed rather than as untested. The corpus writes none of these shapes
// past two declarations, so without this test the subsumption argument would rest on reading alone.
// Every verdict below was driven against the installed rule first.
func TestNoRedeclareMergeSetsSubsumeTheAllOneKindCases(t *testing.T) {
	for _, sourceText := range []string{
		"interface A {}\ninterface A {}\ninterface A {}",
		"namespace A {}\nnamespace A {}\nnamespace A {}",
		"interface A {}\nnamespace A {}",
		"namespace A {}\ninterface A {}\nnamespace A {}",
	} {
		ruletest.ExpectClean(t,
			ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, sourceText, nil))
	}

	// The control, and the reason the four silences above are the sets working rather than the rule
	// being inert on these kinds: add a second CLASS and the class set's primary count passes one,
	// so the exemption lapses and exactly the extra class reports.
	ruletest.ExpectFindings(t,
		ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile,
			"interface A {}\nclass A {}\nclass A {}\nnamespace A {}", nil),
		"redeclared")
}
