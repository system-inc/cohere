package typescript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// redeclareFile is where the fixtures pretend to live.
const redeclareFile = "/repository/source/Redeclare.ts"

/*
 * The corpus is typescript-eslint 8.71.0's own no-redeclare rows, all of them, plus edge rows, each with
 * the verdict the installed rule gave on the same bytes under the same tsconfig;
 * no_redeclare_corpus_data_test.go says how it was built. Each row runs over a real program, so the lib a
 * builtin comes from is the program's, as it is for the installed rule under typed parsing.
 */

// noRedeclareTsconfig is the corpus tsconfig, with lib replaced for a row that names one.
func noRedeclareTsconfig(test *testing.T, lib []string) string {
	test.Helper()
	if lib == nil {
		return noRedeclareCorpusTsconfig
	}
	var configuration struct {
		CompilerOptions map[string]json.RawMessage `json:"compilerOptions"`
		Include         []string                   `json:"include"`
	}
	if decodeError := json.Unmarshal([]byte(noRedeclareCorpusTsconfig), &configuration); decodeError != nil {
		test.Fatal(decodeError)
	}
	encodedLib, libEncodeError := json.Marshal(lib)
	if libEncodeError != nil {
		test.Fatal(libEncodeError)
	}
	configuration.CompilerOptions["lib"] = encodedLib
	encoded, encodeError := json.Marshal(configuration)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	return string(encoded)
}

func TestNoRedeclareUpstreamCorpus(test *testing.T) {
	test.Parallel()

	valid, invalid, pinned := 0, 0, 0
	for _, row := range noRedeclareCorpus {
		if row.edge != "" {
			continue
		}
		if len(row.pinned) > 0 {
			pinned++
		}
		// Upstream's direction, not the recorded verdict's: a pinned row's verdict is taken with the
		// inexpressible part out, which can turn an invalid row clean
		if row.index < noRedeclareCorpusUpstreamValid {
			valid++
		} else {
			invalid++
		}
	}
	if valid != noRedeclareCorpusUpstreamValid ||
		invalid != noRedeclareCorpusUpstreamInvalid ||
		pinned != noRedeclareCorpusUpstreamPinned {
		test.Fatalf(
			"the corpus holds %d valid, %d invalid and %d pinned upstream rows, and upstream has %d, %d and %d",
			valid,
			invalid,
			pinned,
			noRedeclareCorpusUpstreamValid,
			noRedeclareCorpusUpstreamInvalid,
			noRedeclareCorpusUpstreamPinned,
		)
	}

	for _, row := range noRedeclareCorpus {
		name := fmt.Sprintf("upstream-%d", row.index)
		if row.edge != "" {
			name = row.edge
		}
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			options, decodeNoRedeclareOptionsError := DecodeNoRedeclareOptions([]byte(row.options))
			if decodeNoRedeclareOptionsError != nil {
				test.Fatalf("decoding %q: %v", row.options, decodeNoRedeclareOptionsError)
			}
			configuration := noRedeclareTsconfig(test, row.lib)
			result := rule_testing.RunTypedFilesWithSetupAndOptions(test, NoRedeclare, map[string]string{
				"file.ts": row.source,
			}, "file.ts", options,
				func(directory string) {
					if writeError := os.WriteFile(
						filepath.Join(directory, "tsconfig.json"),
						[]byte(configuration),
						0o644,
					); writeError != nil {
						test.Fatal(writeError)
					}
				})

			diagnostics := result.Diagnostics
			sort.SliceStable(diagnostics, func(first, second int) bool {
				return diagnostics[first].Range.Pos() < diagnostics[second].Range.Pos()
			})
			reported := []noRedeclareCorpusFinding{}
			for _, diagnostic := range diagnostics {
				reported = append(reported, noRedeclareCorpusFinding{
					id:   diagnostic.Message.Id,
					text: row.source[diagnostic.Range.Pos():diagnostic.Range.End()],
				})
			}
			if fmt.Sprint(reported) != fmt.Sprint(row.findings) {
				test.Fatalf("the rule reports %q, and typescript-eslint 8.71.0 reports %q", reported, row.findings)
			}
			rule_testing.RecordAssertedCase(test, result)
		})
	}
}

// The three things cohere cannot express are pinned on the rows that carry them, by name, so a row
// cannot lose its pin and quietly start asserting a verdict it was never recorded under, and a fourth
// kind cannot appear without a reason being written for it.
func TestNoRedeclareCorpusPinsWhatCohereCannotExpress(test *testing.T) {
	test.Parallel()

	reasons := map[string]bool{
		// cohere carries no globals configuration, by the 1.0 contract (#bfxz13m item 4).
		"globals configuration": true,
		// Directive globals are declined, as in no-implicit-globals.
		"global comment directive": true,
		// There is no CommonJS function scope around a file.
		"ecmaFeatures.globalReturn": true,
	}
	counts := map[string]int{}
	for _, row := range noRedeclareCorpus {
		for _, feature := range row.pinned {
			if !reasons[feature] {
				test.Errorf("row %d pins %q, which has no reason here", row.index, feature)
			}
			counts[feature]++
		}
	}
	for feature := range reasons {
		if counts[feature] == 0 {
			test.Errorf("no row pins %q, so its reason above is stale", feature)
		}
	}
}

// Three scopes upstream never examines, kept as upstream parity (ruled on #e1zk9s0). Upstream finds its
// scopes from eight listeners (the program, functions, arrow functions, blocks, `for`, `for in`,
// `for of`, `switch`), each checking only the scope that node owns. A namespace body, a class static
// block and a catch clause own a scope none of those nodes is the block of, so a duplicate there is
// never looked at. Nothing is lost: TypeScript's checker reports a real redeclaration in each (TS2451
// and its kin). Each case was recorded clean against the installed rule; the control shows the same
// duplicate in an ordinary block still reports, so the silence is the scope and not a dead rule.
func TestNoRedeclareScopesUpstreamNeverExamines(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		// A namespace body is the scope of the TSModuleBlock, which no listener owns.
		{"namespace body", "namespace N {\n  let a;\n  let a;\n}"},
		// A static block's statements sit directly in the StaticBlock, which is not a BlockStatement.
		{"class static block", "class C {\n  static {\n    let a;\n    let a;\n  }\n}"},
		// A catch parameter binds in the catch clause's own scope, which no listener owns.
		{"catch clause", "try {\n} catch ({ a, b: a }) {\n}"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(
				test,
				rule_testing.RunTypedWithOptions(test, NoRedeclare, redeclareFile, testCase.sourceText, nil),
			)
		})
	}

	test.Run("control: an ordinary block reports", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectFindings(test,
			rule_testing.RunTypedWithOptions(test, NoRedeclare, redeclareFile, "{\n  let a;\n  let a;\n}", nil),
			"redeclared")
	})
}

// optionsJSON builds this rule's configuration the way the config layer delivers it, which is the
// bare object rather than upstream's `[{...}]` tuple. Routing fixtures through the rule's own
// decoder is what puts the default inversion under test: `ignoreDeclarationMerge` defaults to true,
// so a zero-valued struct silently turns the merge exemptions off and every merge case starts
// reporting.
func redeclareOptions(ignoreDeclarationMerge bool) any {
	raw, encodeError := json.Marshal(map[string]bool{"ignoreDeclarationMerge": ignoreDeclarationMerge})
	if encodeError != nil {
		panic(encodeError)
	}
	decoded, decodeNoRedeclareOptionsError := DecodeNoRedeclareOptions(raw)
	if decodeNoRedeclareOptionsError != nil {
		panic(decodeNoRedeclareOptionsError)
	}
	return decoded
}

// TestNoRedeclareRequiresTheTypedHarness asserts the rule declines rather than panics when the
// program could not be built, and that the plain harness is not enough to exercise it.
//
// Without this a later revert of the nil guard would be invisible: the untyped harness hands the
// rule a nil checker, the rule returns early, and every StaysSilent case above would keep passing
// vacuously while every Fires case failed in a way that reads like a rule defect.
func TestNoRedeclareRequiresTheTypedHarness(test *testing.T) {
	test.Parallel()

	// The typed harness reports.
	rule_testing.ExpectFindings(test,
		rule_testing.RunTyped(test, NoRedeclare, redeclareFile, "var a = 1; var a = 2;"), "redeclared")

	// The plain one hands the rule no checker, so it declines. Silence here is the correct answer
	// and not a finding this rule missed.
	rule_testing.ExpectClean(test,
		rule_testing.Run(test, NoRedeclare, redeclareFile, "var a = 1; var a = 2;"))
}

// TestNoRedeclareDefaultsToIgnoringDeclarationMerge is the decoder test, and it exists because both
// of this rule's options default to true.
//
// A rule written down as a bare "error" is handed nil options, and a generic decoder would yield a
// zero-valued struct whose false `IgnoreDeclarationMerge` turns every merge exemption off. That does
// not weaken the rule, it inverts it: thirteen of upstream's clean cases would start reporting. Both
// halves are asserted, since a decoder that ignored its input entirely would pass the first alone.
func TestNoRedeclareDefaultsToIgnoringDeclarationMerge(test *testing.T) {
	test.Parallel()

	asOptions := func(decoded any) NoRedeclareOptions {
		options, isType := decoded.(NoRedeclareOptions)
		if !isType {
			test.Fatalf("decoding the options gave %T, expected NoRedeclareOptions", decoded)
		}
		return options
	}

	decoded, decodeNoRedeclareOptionsError := DecodeNoRedeclareOptions(nil)
	if decodeNoRedeclareOptionsError != nil {
		test.Fatalf("DecodeNoRedeclareOptions(nil) errored: %v", decodeNoRedeclareOptionsError)
	}
	if got := asOptions(decoded); !got.IgnoreDeclarationMerge || !got.BuiltinGlobals {
		test.Errorf("absent options: %+v, want both true", got)
	}
	builtinsOff, decodeNoRedeclareOptionsError := DecodeNoRedeclareOptions(json.RawMessage(`{"builtinGlobals":false}`))
	if decodeNoRedeclareOptionsError != nil {
		test.Fatalf("DecodeNoRedeclareOptions errored: %v", decodeNoRedeclareOptionsError)
	}
	if got := asOptions(builtinsOff); got.BuiltinGlobals || !got.IgnoreDeclarationMerge {
		test.Errorf("builtinGlobals false: %+v, want it off and the merge exemption still on", got)
	}

	explicit, decodeNoRedeclareOptionsError := DecodeNoRedeclareOptions(
		json.RawMessage(`{"ignoreDeclarationMerge":false}`),
	)
	if decodeNoRedeclareOptionsError != nil {
		test.Fatalf("DecodeNoRedeclareOptions errored: %v", decodeNoRedeclareOptionsError)
	}
	if got := asOptions(explicit); got.IgnoreDeclarationMerge {
		test.Errorf("explicit false: IgnoreDeclarationMerge = true, want false")
	}

	// And the same distinction reaching the rule, since the two lines above only prove the decoder
	// parses. A bare "error" configuration is nil options, and the merge must stay exempt there.
	rule_testing.ExpectClean(test,
		rule_testing.RunTypedWithOptions(test, NoRedeclare, redeclareFile, "interface A {}\ninterface A {}", nil))
	rule_testing.ExpectFindings(test,
		rule_testing.RunTypedWithOptions(
			test,
			NoRedeclare,
			redeclareFile,
			"interface A {}\ninterface A {}",
			redeclareOptions(false),
		),
		"redeclared")
}

// TestNoRedeclareSpans asserts where each finding points and what it says, which `ExpectFindings`
// cannot see at all: it compares message ids and a count and nothing else, so a rule anchoring every
// finding on the wrong node passes a complete fixture pair.
//
// The anchor is the redeclared identifier rather than the declaration statement, which is upstream's
// choice and is visible in its corpus as a column: the second `var a = 10;` reports at column 5, the
// `a` rather than the `var`. Anchoring on the statement would put it at column 1 and every message
// id fixture above would stay green.
//
// The harness writes each fixture as `strings.TrimSpace(contents)+"\n"`, so slicing a literal that
// carried a leading newline would be off by one byte. These sources are written without one, so the
// literal and the file agree, and the slice below is taken from the same string the harness wrote.
func TestNoRedeclareSpans(test *testing.T) {
	test.Parallel()

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
		{
			"merging off reports both",
			"function Beta() {}\nclass Beta {}\nnamespace Beta {}",
			redeclareOptions(false),
			[]string{"Beta", "Beta"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(
				test,
				NoRedeclare,
				redeclareFile,
				testCase.sourceText,
				testCase.options,
			)
			if len(result.Diagnostics) != len(testCase.wantTexts) {
				test.Fatalf("got %d findings, want %d", len(result.Diagnostics), len(testCase.wantTexts))
			}
			for index, diagnostic := range result.Diagnostics {
				reported := testCase.sourceText[diagnostic.Range.Pos():diagnostic.Range.End()]
				if reported != testCase.wantTexts[index] {
					test.Errorf("finding %d points at %q, want %q", index, reported, testCase.wantTexts[index])
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
func TestNoRedeclareMessage(test *testing.T) {
	test.Parallel()

	result := rule_testing.RunTypedWithOptions(test, NoRedeclare, redeclareFile, "var a = 3;\nvar a = 10;", nil)
	if len(result.Diagnostics) != 1 {
		test.Fatalf("got %d findings, want 1", len(result.Diagnostics))
	}
	const wantIdentifier = "redeclared"
	if got := result.Diagnostics[0].Message.Id; got != wantIdentifier {
		test.Errorf("message id = %q, want %q", got, wantIdentifier)
	}
	const wantPrefix = "This name is already declared in the same scope."
	if got := result.Diagnostics[0].Message.Description; len(got) < len(wantPrefix) ||
		got[:len(wantPrefix)] != wantPrefix {
		test.Errorf("message description = %q, want it to begin %q", got, wantPrefix)
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
func TestNoRedeclareMergeSetsSubsumeTheAllOneKindCases(test *testing.T) {
	test.Parallel()

	for _, sourceText := range []string{
		"interface A {}\ninterface A {}\ninterface A {}",
		"namespace A {}\nnamespace A {}\nnamespace A {}",
		"interface A {}\nnamespace A {}",
		"namespace A {}\ninterface A {}\nnamespace A {}",
	} {
		rule_testing.ExpectClean(test,
			rule_testing.RunTypedWithOptions(test, NoRedeclare, redeclareFile, sourceText, nil))
	}

	// The control, and the reason the four silences above are the sets working rather than the rule
	// being inert on these kinds: add a second class and the class set's primary count passes one,
	// so the exemption lapses and exactly the extra class reports.
	rule_testing.ExpectFindings(test,
		rule_testing.RunTypedWithOptions(test, NoRedeclare, redeclareFile,
			"interface A {}\nclass A {}\nclass A {}\nnamespace A {}", nil),
		"redeclared")
}
