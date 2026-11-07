package tailwind

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// Every expectation was measured against eslint-plugin-better-tailwindcss 4.7.0, and the
// corrections were checked against its own autofix output rather than only against which classes it
// reports: a rule that reports the right set and rewrites it wrongly passes a set comparison.
//
// `rule_testing.Run` rather than the class-order program harness, because the question is entirely about
// how a class is spelled and this rule declares no ProgramReads.
func runVariableSyntaxFixture(test *testing.T, source string) rule_testing.Result {
	test.Helper()
	return rule_testing.Run(test, EnforceConsistentVariableSyntax, "Component.tsx", source)
}

// TestEnforceConsistentVariableSyntaxReports covers what upstream reports.
func TestEnforceConsistentVariableSyntaxReports(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name       string
		source     string
		correction string
	}{
		// The bare property in brackets.
		{
			name:       "bracketed property",
			source:     `const element = <div className="text-[--my-var]" />;`,
			correction: "text-(--my-var)",
		},
		// The call form. Both bracketed spellings collapse onto the parenthesised one, which is the
		// part a reader predicts wrongly.
		{
			name:       "bracketed var call",
			source:     `const element = <div className="text-[var(--my-var)]" />;`,
			correction: "text-(--my-var)",
		},
		{
			name:       "bracketed var call on a background",
			source:     `const element = <div className="bg-[var(--other)]" />;`,
			correction: "bg-(--other)",
		},
		// The variant is preserved, which is what the prefix split is for.
		{
			name:       "behind a variant",
			source:     `const element = <div className="hover:text-[--v]" />;`,
			correction: "hover:text-(--v)",
		},
		// The modifier rides outside the brackets and has to be put back untouched.
		{
			name:       "with an opacity modifier",
			source:     `const element = <div className="text-[--v]/50" />;`,
			correction: "text-(--v)/50",
		},
		// A property name may hold dashes and underscores.
		{
			name:       "property name with dashes and underscores",
			source:     `const element = <div className="text-[--a-b_c]" />;`,
			correction: "text-(--a-b_c)",
		},
		// A fallback inside the call survives the unwrap.
		{
			name:       "var call with a fallback",
			source:     `const element = <div className="w-[var(--x,10px)]" />;`,
			correction: "w-(--x,10px)",
		},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runVariableSyntaxFixture(test, testCase.source)
			rule_testing.ExpectFindings(test, result, "variableSyntax")
			if len(result.Diagnostics) == 0 {
				test.Fatal("expected a finding")
			}
			if !strings.Contains(result.Diagnostics[0].Message.Description, testCase.correction) {
				test.Errorf("the message does not name the correction %q: %s",
					testCase.correction, result.Diagnostics[0].Message.Description)
			}
		})
	}
}

// TestEnforceConsistentVariableSyntaxInTemplateRuns reads the static runs of a template with holes.
//
// Measured on eslint-plugin-better-tailwindcss 4.9.1 under ESLint on 2026-11-07, with ahra's theme:
// each piece of a template is judged as a literal, the class glued to a hole included, and each
// finding lands on its own class.
//
//	`block bg-[var(--color-x)] ${x}`      reports bg-[var(--color-x)]
//	`bg-[var(--color-x)]${x} block`       reports bg-[var(--color-x)]
//	`block ${x}bg-[var(--color-x)]`       reports bg-[var(--color-x)]
//	`bg-(--color-x) ${x} block`           silent
func TestEnforceConsistentVariableSyntaxInTemplateRuns(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name    string
		source  string
		classes []string
	}{
		{
			name:    "a run beside a hole",
			source:  "const element = <div className={`block bg-[var(--color-x)] ${size}`} />;",
			classes: []string{"bg-[var(--color-x)]"},
		},
		{
			name:    "glued before a hole",
			source:  "const element = <div className={`bg-[var(--color-x)]${size} block`} />;",
			classes: []string{"bg-[var(--color-x)]"},
		},
		{
			name:    "glued after a hole, in the last run",
			source:  "const element = <div className={`block ${size}bg-[var(--color-x)]`} />;",
			classes: []string{"bg-[var(--color-x)]"},
		},
		{
			name:    "already shorthand",
			source:  "const element = <div className={`bg-(--color-x) ${size} block`} />;",
			classes: []string{},
		},
		{
			// An escape makes the run's source differ from its decoded value, so the finding sits on
			// the run.
			name:    "a run holding an escape",
			source:  "const element = <div className={`bg-[var(--color-x)]\\u0020block ${size}`} />;",
			classes: []string{"bg-[var(--color-x)]\\u0020block "},
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runVariableSyntaxFixture(test, testCase.source)
			if len(result.Diagnostics) != len(testCase.classes) {
				test.Fatalf("got %d findings, want %d", len(result.Diagnostics), len(testCase.classes))
			}
			text := result.SourceFile.Text()
			for index, diagnostic := range result.Diagnostics {
				reported := text[diagnostic.Range.Pos():diagnostic.Range.End()]
				if reported != testCase.classes[index] {
					test.Errorf("finding %d covers %q, want the class %q", index, reported, testCase.classes[index])
				}
			}
		})
	}
}

// TestEnforceConsistentVariableSyntaxStaysSilent covers what upstream leaves alone.
func TestEnforceConsistentVariableSyntaxStaysSilent(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name   string
		source string
	}{
		{
			name:   "already parenthesised",
			source: `const element = <div className="text-(--my-var)" />;`,
		},
		/*
		 * A colon in the base means the brackets name a property rather than read one, and
		 * rewriting them would change meaning rather than spelling. Upstream calls this skipping
		 * variable definitions.
		 */
		{
			name:   "arbitrary property",
			source: `const element = <div className="text-[color:red]" />;`,
		},
		/*
		 * The brackets hold arithmetic, not a property reference. Upstream's shorthand test carries
		 * a lookahead for exactly this, and dropping it would rewrite the calc into nonsense.
		 */
		{
			name:   "calc containing a var call",
			source: `const element = <div className="w-[calc(var(--x)*2)]" />;`,
		},
		// Two calls in one bracket are not a single property reference either.
		{
			name:   "several var calls in one value",
			source: `const element = <div className="shadow-[var(--a),var(--b)]" />;`,
		},
		{
			name:   "no custom property at all",
			source: `const element = <div className="flex items-center" />;`,
		},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, runVariableSyntaxFixture(test, testCase.source))
		})
	}
}

// TestEnforceConsistentVariableSyntaxVariableForm pins the configured direction.
func TestEnforceConsistentVariableSyntaxVariableForm(test *testing.T) {
	test.Parallel()
	variableForm := EnforceConsistentVariableSyntaxOptions{Syntax: variableSyntaxVariable}

	reported := rule_testing.RunWithOptions(test, EnforceConsistentVariableSyntax, "Component.tsx",
		`const element = <div className="text-(--my-var)" />;`, variableForm)
	rule_testing.ExpectFindings(test, reported, "variableSyntax")
	if len(reported.Diagnostics) > 0 &&
		!strings.Contains(reported.Diagnostics[0].Message.Description, "text-[var(--my-var)]") {
		test.Errorf("expected the bracketed call form: %s", reported.Diagnostics[0].Message.Description)
	}

	silent := rule_testing.RunWithOptions(test, EnforceConsistentVariableSyntax, "Component.tsx",
		`const element = <div className="text-[var(--my-var)]" />;`, variableForm)
	rule_testing.ExpectClean(test, silent)
}
