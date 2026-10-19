package tailwind

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// The one-class half of enforce-canonical-classes (#15swfzq). The Canonicalizer's own differential,
// TestCanonicalizeAgreesWithTailwind, holds every answer to Tailwind's over thousands of classes; these
// hold what the rule does with an answer: upstream's message, a fix on the class's own text, the option
// that no longer silences it, and the filters that keep a class from being asked at all.

func TestCanonicalSinglesReportInUpstreamsWords(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		source      string
		wantMessage string
		wantFixed   string
	}{
		{
			// deprecatedUtilities, one of the #3dpwm9b sites on ahra.
			name:        "logical inset rename",
			source:      `const element = <div className="absolute end-0" />;`,
			wantMessage: `The class: "end-0" can be simplified to "inset-e-0".`,
			wantFixed:   `const element = <div className="absolute inset-e-0" />;` + "\n",
		},
		{
			name:        "negative logical inset rename",
			source:      `const element = <div className="-start-px" />;`,
			wantMessage: `The class: "-start-px" can be simplified to "-inset-s-px".`,
			wantFixed:   `const element = <div className="-inset-s-px" />;` + "\n",
		},
		{
			// modernizeArbitraryValuesVariant, the TableTheme.ts site.
			name:        "arbitrary child variant",
			source:      `const element = <div className="data-[state=selected]:[&>*]:flex" />;`,
			wantMessage: `The class: "data-[state=selected]:[&>*]:flex" can be simplified to "data-[state=selected]:*:flex".`,
			wantFixed:   `const element = <div className="data-[state=selected]:*:flex" />;` + "\n",
		},
		{
			// One arbitrary variant becomes two, and the importance moves to the end as upstream prints it.
			name:        "child attribute variant splits in two",
			source:      `const element = <div className="[&>[data-dot=true]]:!bg-red-600" />;`,
			wantMessage: `The class: "[&>[data-dot=true]]:!bg-red-600" can be simplified to "*:data-[dot=true]:bg-red-600!".`,
			wantFixed:   `const element = <div className="*:data-[dot=true]:bg-red-600!" />;` + "\n",
		},
		{
			name:        "attribute variant becomes aria",
			source:      `const element = <div className="[&[aria-sort]]:hover:flex" />;`,
			wantMessage: `The class: "[&[aria-sort]]:hover:flex" can be simplified to "aria-[sort]:hover:flex".`,
			wantFixed:   `const element = <div className="aria-[sort]:hover:flex" />;` + "\n",
		},
		{
			// No pass at all: printing the parse is the rewrite.
			name:        "lone var prints as the shorthand",
			source:      `const element = <div className="bg-[var(--table-pinned-background)]" />;`,
			wantMessage: `The class: "bg-[var(--table-pinned-background)]" can be simplified to "bg-(--table-pinned-background)".`,
			wantFixed:   `const element = <div className="bg-(--table-pinned-background)" />;` + "\n",
		},
		{
			name:        "leading importance prints trailing",
			source:      `const element = <div className="!outline" />;`,
			wantMessage: `The class: "!outline" can be simplified to "outline!".`,
			wantFixed:   `const element = <div className="outline!" />;` + "\n",
		},
		{
			name:        "legacy gradient",
			source:      `const element = <div className="bg-gradient-to-r" />;`,
			wantMessage: `The class: "bg-gradient-to-r" can be simplified to "bg-linear-to-r".`,
			wantFixed:   `const element = <div className="bg-linear-to-r" />;` + "\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := runCanonicalFixture(t, "Component.tsx", testCase.source)
			rule_testing.ExpectFindings(t, result, "canonicalSingle")
			if got := result.Diagnostics[0].Message.Description; got != testCase.wantMessage {
				t.Errorf("message\n got: %s\nwant: %s", got, testCase.wantMessage)
			}
			rule_testing.ExpectFixedSource(t, result, testCase.wantFixed)
		})
	}
}

// TestCanonicalSinglesStaySilent is the half that keeps the rule on: classes Tailwind leaves as written,
// including the near misses each ported pass must not reach.
func TestCanonicalSinglesStaySilent(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		source string
	}{
		{name: "already the current inset", source: `const element = <div className="inset-e-0 inset-s-4" />;`},
		{name: "end that is not an inset", source: `const element = <div className="justify-end self-end items-end" />;`},
		{name: "rounded logical edge", source: `const element = <div className="rounded-e-md" />;`},
		{name: "child variant already named", source: `const element = <div className="*:flex **:flex" />;`},
		{name: "child selector that is more than the child", source: `const element = <div className="[&>*_p]:flex [&>*:hover]:flex" />;`},
		{name: "descendant element selector", source: `const element = <div className="[&_p]:flex" />;`},
		{name: "arbitrary value that is no variable", source: `const element = <div className="bg-[#123456] w-[17px]" />;`},
		{name: "a class nothing rewrites", source: `const element = <div className="px-4 bg-red-500" />;`},
		{
			// Tailwind would rewrite it, through arbitraryUtilities, which is not ported yet. Silent rather
			// than wrong, and listed in canonical_unported.json.
			name:   "a rewrite only an unported pass makes",
			source: `const element = <div className="z-[1]" />;`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectClean(t, runCanonicalFixture(t, "Component.tsx", testCase.source))
		})
	}
}

// TestCanonicalSingleKeepsARepositoryOverride is upstream's "With custom implementation" case: a
// repository that declares its own `start-*` meant something by it, and the sameness check is what keeps
// the class as written. The control, the same class over the bare framework, reports.
func TestCanonicalSingleKeepsARepositoryOverride(t *testing.T) {
	t.Parallel()
	source := `const element = <div className="start-0" />;`

	bare := runCanonicalFixture(t, "Component.tsx", source)
	rule_testing.ExpectFindings(t, bare, "canonicalSingle")

	overridden := runCanonicalFixtureWithStylesheet(t, "Component.tsx", source,
		"@import \"tailwindcss\";\n@utility start-* {\n    --custom-start: --value(integer);\n}\n", nil)
	rule_testing.ExpectClean(t, overridden)
}

// TestCanonicalSinglesReportWithCollapseOff holds upstream's `collapse: false`: pairs go quiet and single
// classes still report. Before #15swfzq this option declined the whole file.
func TestCanonicalSinglesReportWithCollapseOff(t *testing.T) {
	t.Parallel()
	off := false
	result := runCanonicalFixtureWithOptions(t, "Component.tsx",
		`const element = <div className="px-4 py-4 end-0" />;`,
		EnforceCanonicalClassesOptions{Collapse: &off})
	rule_testing.ExpectFindings(t, result, "canonicalSingle")
}

// TestCanonicalSinglesSkipWhatUpstreamDoesNotAsk holds the filters in front of the canonicalizer: an
// ignored class and a class the design system cannot compile are never asked about.
func TestCanonicalSinglesSkipWhatUpstreamDoesNotAsk(t *testing.T) {
	t.Parallel()
	ignored := runCanonicalFixtureWithOptions(t, "Component.tsx",
		`const element = <div className="end-0" />;`,
		EnforceCanonicalClassesOptions{Ignore: []string{"^end-"}})
	rule_testing.ExpectClean(t, ignored)

	// It parses, its variant would modernize to `*:`, and `zz` is no inset value, so it compiles to
	// nothing. Upstream drops it before asking.
	unknown := runCanonicalFixture(t, "Component.tsx",
		`const element = <div className="[&>*]:end-zz" />;`)
	rule_testing.ExpectClean(t, unknown)
}

// TestCanonicalSingleFixesOnlyTheFirstRepeat holds the overlap rule reportDeprecations set: every
// occurrence is reported, as upstream reports each, and only the first is rewritten, because
// no-duplicate-classes deletes the second.
func TestCanonicalSingleFixesOnlyTheFirstRepeat(t *testing.T) {
	t.Parallel()
	result := runCanonicalFixture(t, "Component.tsx", `const element = <div className="end-0 end-0" />;`)
	rule_testing.ExpectFindings(t, result, "canonicalSingle", "canonicalSingle")
	rule_testing.ExpectFixedSource(t, result, `const element = <div className="inset-e-0 end-0" />;`+"\n")
}
