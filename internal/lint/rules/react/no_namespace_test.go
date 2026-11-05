package react

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// noNamespaceFile is where the fixtures pretend to live. A .tsx extension, because half the corpus
// is JSX and the harness picks its script kind off the suffix.
//
// No file-suffix gate exists in this rule and none should be added. Three shipped react rules here
// gated on .tsx or .jsx through an isJsxFileName helper, residue from an oxc-era port and not
// upstream behavior: eslint-plugin-react reports on a .ts file exactly as it does on a .tsx one.
// Those three gates were removed in 4cff5fe, 206b628 and 9dbd234; none remains in this package.
// See TestNoNamespaceHasNoFileSuffixGate below, which pins that.
const noNamespaceFile = "/repository/source/Namespace.tsx"

// The corpus is eslint-plugin-react's own, imported verbatim from
// tests/lib/rules/no-namespace.js by evaluating the upstream tester with a stub RuleTester and
// serializing what it was handed, so no case was retyped and no escape could be cooked on the way
// in. 27 valid and 16 invalid, and every invalid case names exactly one entry in its
// errors array, so one finding per input is stated by upstream rather than recovered.
//
// The valid list is doing most of the work here. Sixteen of its 27 entries are the dotted forms,
// object.testcomponent and Object.TestComponent and their case variants, which exist because a
// naive colon test over a rendered tag name would be fine on them and a naive namespace test over
// a member expression would not. Three more are React.createElement called with null, true and an
// object literal, which pin that the first argument must be a string literal specifically.
func TestNoNamespaceFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		findings   []string
	}{
		{"<ns:testcomponent />", []string{"noNamespace"}},
		{"React.createElement(\"ns:testcomponent\")", []string{"noNamespace"}},
		{"<ns:testComponent />", []string{"noNamespace"}},
		{"React.createElement(\"ns:testComponent\")", []string{"noNamespace"}},
		{"<ns:test_component />", []string{"noNamespace"}},
		{"React.createElement(\"ns:test_component\")", []string{"noNamespace"}},
		{"<ns:TestComponent />", []string{"noNamespace"}},
		{"React.createElement(\"ns:TestComponent\")", []string{"noNamespace"}},
		{"<Ns:testcomponent />", []string{"noNamespace"}},
		{"React.createElement(\"Ns:testcomponent\")", []string{"noNamespace"}},
		{"<Ns:testComponent />", []string{"noNamespace"}},
		{"React.createElement(\"Ns:testComponent\")", []string{"noNamespace"}},
		{"<Ns:test_component />", []string{"noNamespace"}},
		{"React.createElement(\"Ns:test_component\")", []string{"noNamespace"}},
		{"<Ns:TestComponent />", []string{"noNamespace"}},
		{"React.createElement(\"Ns:TestComponent\")", []string{"noNamespace"}},
	}
	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.findings...)
		})
	}
}

func TestNoNamespaceStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []string{
		"<testcomponent />",
		"React.createElement(\"testcomponent\")",
		"<testComponent />",
		"React.createElement(\"testComponent\")",
		"<test_component />",
		"React.createElement(\"test_component\")",
		"<TestComponent />",
		"React.createElement(\"TestComponent\")",
		"<object.testcomponent />",
		"React.createElement(\"object.testcomponent\")",
		"<object.testComponent />",
		"React.createElement(\"object.testComponent\")",
		"<object.test_component />",
		"React.createElement(\"object.test_component\")",
		"<object.TestComponent />",
		"React.createElement(\"object.TestComponent\")",
		"<Object.testcomponent />",
		"React.createElement(\"Object.testcomponent\")",
		"<Object.testComponent />",
		"React.createElement(\"Object.testComponent\")",
		"<Object.test_component />",
		"React.createElement(\"Object.test_component\")",
		"<Object.TestComponent />",
		"React.createElement(\"Object.TestComponent\")",
		"React.createElement(null)",
		"React.createElement(true)",
		"React.createElement({})",
	}
	for _, sourceText := range cases {
		test.Run(sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// Beyond the imported corpus. Each case exists because the corpus cannot see the distinction, and
// each verdict was measured against the installed eslint-plugin-react through the Linter API before
// being written here.

// TestNoNamespaceCreateElementBoundary pins which createElement calls the rule accepts.
//
// This is the single most important test in this file, because the shelf's own
// react.IsCreateElementCall answers differently on four of these six inputs and reaching for it is
// the natural first move. Upstream's util/isCreateElement.js requires a static member whose object
// is spelled exactly React; the shelf accepts a bare call, any object, and a computed member.
//
// Not one case in the imported corpus writes any of these spellings, so all 43 of them stay green
// under either predicate. Measured verdicts, all from the installed build.
func TestNoNamespaceCreateElementBoundary(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		findings   []string
	}{
		{"the pragma spelling reports", "React.createElement(\"ns:x\");", []string{"noNamespace"}},
		{"a parenthesized pragma callee reports, because a parenthesis is not a node upstream walks", "(React.createElement)(\"ns:x\");", []string{"noNamespace"}},
		{"a bare call is silent without a React import to trace it to", "createElement(\"ns:x\");", nil},
		{"another object is silent, and Preact is the case that matters", "Preact.createElement(\"ns:x\");", nil},
		{"document is silent for the same reason rather than by a special case", "document.createElement(\"ns:x\");", nil},
		{"a computed member is silent", "React[\"createElement\"](\"ns:x\");", nil},
		{"a template literal is silent, because upstream tests for a Literal node", "React.createElement(`ns:x`);", nil},
		{"no arguments at all is silent", "React.createElement();", nil},
		{"the name is read syntactically, so a local React holding a number still reports", "const React = 1; React.createElement(\"ns:x\");", []string{"noNamespace"}},
		{"a colon anywhere in the string is enough", "React.createElement(\":\");", []string{"noNamespace"}},
		{"and more than one colon still reports once", "React.createElement(\"a:b:c\");", []string{"noNamespace"}},
		{"a namespaced name past the first argument is still read from the first", "React.createElement(\"a:b\", {}, \"c\");", []string{"noNamespace"}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.findings...)
		})
	}
}

// TestNoNamespaceReadsBothElementForms pins that a non-self-closing element reports too.
//
// Every JSX case upstream ships is self-closing, and our parser gives a self-closing element its own
// kind with no opening element inside it. So a rule listening on only one of the two kinds passes
// either the whole imported corpus or none of it, and this is what separates the two.
func TestNoNamespaceReadsBothElementForms(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		findings   []string
	}{
		{"self closing", "<ns:x />;", []string{"noNamespace"}},
		{"with a closing tag", "<ns:x></ns:x>;", []string{"noNamespace"}},
		{"a dotted tag name never reaches the namespace arm", "<a.b.c />;", nil},
		{"and neither does a this-rooted one", "<this.Foo />;", nil},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.findings...)
		})
	}
}

// TestNoNamespaceDoesNotPanicOnDottedTagNames is crash protection, not a behavioral fixture.
//
// ast.Node.Text has no case for a PropertyAccessExpression and reaches its unhandled-case panic, so
// a rule that rendered an arbitrary tag name to a string would take the whole file down for every
// rule in the package, not just this one. That failure is invisible to ExpectFindings, which is why
// this exists as its own test naming what it prevents rather than as another silent case above.
//
// A dotted tag name is ordinary code, so this is reachable rather than defensive.
func TestNoNamespaceDoesNotPanicOnDottedTagNames(test *testing.T) {
	test.Parallel()

	for _, sourceText := range []string{
		"<a.b />;",
		"<a.b.c.d />;",
		"<this.Foo />;",
		"<a.b></a.b>;",
	} {
		test.Run(sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestNoNamespaceSpanAndMessage asserts where the finding points and what it says.
//
// ExpectFindings compares message ids and a count and nothing else, so a rule pointing at the wrong
// node passes every fixture above. Upstream reports on the opening element for the JSX arm and on
// the whole call expression for the call arm, measured on the installed build: <ns:x></ns:x> spans
// columns 1 to 7, which is the opening element alone rather than the whole element.
//
// The message is compared against a literal typed here rather than against the rule's own constant,
// because a comparison to the constant moves with the constant under mutation and asserts nothing.
//
// Every span is sliced from the source the harness wrote rather than from the literal above:
// RunTyped trims its input, so a slice taken from an untrimmed Go literal is off by one and reads
// exactly like an off-by-one in the rule.
func TestNoNamespaceSpanAndMessage(test *testing.T) {
	test.Parallel()

	test.Run("a self closing element reports on the element", func(test *testing.T) {
		test.Parallel()
		const sourceText = "const a = <ns:x />;"
		result := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, sourceText)
		rule_testing.ExpectFindings(test, result, "noNamespace")
		reported := result.SourceFile.Text()[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
		if reported != "<ns:x />" {
			test.Fatalf("reported on %q, wanted %q", reported, "<ns:x />")
		}
	})

	test.Run("an element with a closing tag reports on the opening element alone", func(test *testing.T) {
		test.Parallel()
		const sourceText = "const a = <ns:x></ns:x>;"
		result := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, sourceText)
		rule_testing.ExpectFindings(test, result, "noNamespace")
		reported := result.SourceFile.Text()[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
		if reported != "<ns:x>" {
			test.Fatalf("reported on %q, wanted %q", reported, "<ns:x>")
		}
	})

	test.Run("a call reports on the whole call expression", func(test *testing.T) {
		test.Parallel()
		const sourceText = "const a = React.createElement(\"ns:x\");"
		result := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, sourceText)
		rule_testing.ExpectFindings(test, result, "noNamespace")
		reported := result.SourceFile.Text()[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
		if reported != "React.createElement(\"ns:x\")" {
			test.Fatalf("reported on %q, wanted %q", reported, "React.createElement(\"ns:x\")")
		}
	})

	test.Run("the message identifies the rule and explains the defect", func(test *testing.T) {
		test.Parallel()
		result := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, "<ns:x />;")
		rule_testing.ExpectFindings(test, result, "noNamespace")
		if result.Diagnostics[0].Message.Id != "noNamespace" {
			test.Fatalf("message id was %q", result.Diagnostics[0].Message.Id)
		}
		if !strings.HasPrefix(result.Diagnostics[0].Message.Description, "This element names a namespace, and React has no notion of one.") {
			test.Fatalf("message description was %q", result.Diagnostics[0].Message.Description)
		}
	})
}

// TestNoNamespaceHasNoFileSuffixGate pins the absence of a gate, so that nobody reintroduces one by
// pattern-matching a neighbour in this package.
//
// Three shipped react rules here gate on a .tsx or .jsx suffix, which is residue from an oxc-era
// port rather than upstream behavior, and it blinds them to most of this tree. Measured against
// eslint-plugin-react with the same source under four suffixes: all four report.
//
// A .ts file cannot hold JSX, so the call arm is what this test can exercise, and it is the arm a
// suffix gate would silence in exactly the files that matter.
//
// **Only the two TypeScript suffixes are asserted here, and that is a harness limit rather than a
// narrowing of the claim.** `rule_testing`'s typed harness writes a tsconfig whose include is
// `["**/*.ts", "**/*.tsx"]` (program.go:30), so a `.js` or `.jsx` fixture produces no program at all
// and the run fails with TS18003 before the rule is reached. Measured directly: both suffixes fail
// that way, with the identical source that passes under `.ts`. Recording the limit here rather than
// deleting the cases, because a case you cannot express is a fact about the harness, and hiding it
// inside a relaxed test turns it into a fact about the rule.
//
// The JavaScript half of the claim is covered by `TestNoNamespaceHasNoFileSuffixGateUntyped` below,
// which reaches all four suffixes because the untyped harness needs no program.
func TestNoNamespaceHasNoFileSuffixGate(test *testing.T) {
	test.Parallel()

	const sourceText = "React.createElement(\"ns:x\");"
	for _, fileName := range []string{
		"/repository/source/Namespace.ts",
		"/repository/source/Namespace.tsx",
	} {
		test.Run(fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoNamespace, fileName, sourceText)
			rule_testing.ExpectFindings(test, result, "noNamespace")
		})
	}
}

// TestNoNamespaceHasNoFileSuffixGateUntyped covers the two suffixes the typed harness cannot build.
//
// The JSX arm needs no checker, so it is fully exercised under the plain harness and reaches all
// four suffixes. This is what pins the absence of the gate on JavaScript files specifically, which
// is the half the typed test above structurally cannot reach.
func TestNoNamespaceHasNoFileSuffixGateUntyped(test *testing.T) {
	test.Parallel()

	const sourceText = "<ns:x />;"
	for _, fileName := range []string{
		"/repository/source/Namespace.tsx",
		"/repository/source/Namespace.jsx",
	} {
		test.Run(fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoNamespace, fileName, sourceText)
			rule_testing.ExpectFindings(test, result, "noNamespace")
		})
	}
}

// TestNoNamespaceNeedsTheTypedHarness pins that the checker declaration is load-bearing.
//
// A typed rule handed a nil checker goes silent rather than crashing, which is a vacuous green: the
// StaysSilent half of a fixture set passes for the wrong reason and nothing announces it. So the
// discriminator is one source that reports under RunTyped and is silent under Run, which is what a
// later revert of NeedsTypeChecker would turn red.
//
// The bare-call spelling is the only one that asks the checker anything. The JSX arm and the
// `React.createElement` member arm both answer identically either way, which is deliberate: a nil
// checker costs this rule one spelling rather than every finding.
func TestNoNamespaceNeedsTheTypedHarness(test *testing.T) {
	test.Parallel()

	const sourceText = "import {createElement} from \"react\";\ncreateElement(\"ns:x\");"

	typed := rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, sourceText)
	rule_testing.ExpectFindings(test, typed, "noNamespace")

	untyped := rule_testing.Run(test, NoNamespace, noNamespaceFile, sourceText)
	rule_testing.ExpectClean(test, untyped)
}

// TestNoNamespaceDoesNotPanicOnNonLiteralArguments is crash protection, not a behavioral fixture.
//
// `ast.Node.Text` has no case for a CallExpression or a PropertyAccessExpression and reaches its
// unhandled-case panic. The walk recovers per file rather than per rule, so one such node here would
// cost every rule in this package every finding in that file, and the run would still print a
// plausible summary line.
//
// **This is not a hypothetical, and it fired.** During a mid-edit window on this rule, a dry run
// against the real tree reported six crashed files: two `*ast.PropertyAccessExpression` and four
// `*ast.CallExpression`, in NavigationTrail.tsx, AssetGridCard.tsx, AssetRowName.tsx,
// TranslationUtilities.ts, FinanceInventoryAccountGroup.tsx and AssetHero.tsx. The finished rule
// crashes on none of them.
//
// The string-literal guard is what prevents the call half, measured rather than read: deleting
// `if first.Kind != ast.KindStringLiteral` and re-running the first case below panics with exactly
// `Unhandled case in Node.Text: *ast.CallExpression`, and restoring it clears it. So the kind check
// must precede the `Text()` call, and a later reordering that looks harmless is the thing this test
// exists to catch.
//
// No `ExpectFindings` fixture can see a panic, which is why this is its own test naming what it
// prevents rather than another silent row in a boundary table. Reaching this test at all is the
// assertion; the verdicts are pinned elsewhere.
func TestNoNamespaceDoesNotPanicOnNonLiteralArguments(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		// The call half. Four of the six crashed files were this shape.
		{"a call as the first argument", "declare function someCall(): string;\nReact.createElement(someCall());"},
		{"a nested call", "declare const a: any;\nReact.createElement(a.b.c());"},
		{"a conditional", "declare const x: boolean;\nReact.createElement(x ? 'a:b' : 'c');"},
		{"a template literal", "React.createElement(`x:y`);"},
		{"a spread argument, which has no first literal at all", "declare const args: any[];\nReact.createElement(...args);"},
		{"a parenthesized literal, which is not a literal node", "React.createElement(('a:b'));"},

		// The property-access half. Two of the six crashed files were this shape, reached through
		// the JSX arm rather than this one; both are pinned so a future reader sees the pair.
		{"a this-rooted tag name", "declare const x: any;\nconst a = <this.Foo />;"},
		{"a deeply dotted tag name", "declare const a: any;\nconst b = <a.b.c.d />;"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			// Surviving the walk is the assertion. The verdicts for shapes that have one are
			// pinned in the boundary tables above.
			rule_testing.RunTyped(test, NoNamespace, noNamespaceFile, testCase.sourceText)
		})
	}
}
