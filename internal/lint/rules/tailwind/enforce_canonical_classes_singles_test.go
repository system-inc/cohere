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
		}, {
			// arbitraryUtilities, the site #15swfzq was filed on: the one listed class signing alike.
			name:        "theme variable as the listed colour",
			source:      `const element = <div className="ring-(--color-red-500)" />;`,
			wantMessage: `The class: "ring-(--color-red-500)" can be simplified to "ring-red-500".`,
			wantFixed:   `const element = <div className="ring-red-500" />;` + "\n",
		},
		{
			// TaskRow's site: one side pair, whose signature expands to two longhands.
			name:        "theme variable on a border side pair",
			source:      `const element = <div className="border-y-(--color-red-500)" />;`,
			wantMessage: `The class: "border-y-(--color-red-500)" can be simplified to "border-y-red-500".`,
			wantFixed:   `const element = <div className="border-y-red-500" />;` + "\n",
		},
		{
			// No listed class is `z-index: 1`, so the fallback finds `z-1` as a bare value.
			name:        "arbitrary value as a bare value",
			source:      `const element = <div className="z-[1]" />;`,
			wantMessage: `The class: "z-[1]" can be simplified to "z-1".`,
			wantFixed:   `const element = <div className="z-1" />;` + "\n",
		},
		{
			// The fallback's spacing multiple: 45.5rem over 0.25rem.
			name:        "arbitrary length as a spacing multiple",
			source:      `const element = <div className="max-w-[45.5rem]" />;`,
			wantMessage: `The class: "max-w-[45.5rem]" can be simplified to "max-w-182".`,
			wantFixed:   `const element = <div className="max-w-182" />;` + "\n",
		},
		{
			// `-mt-0` signs like `mt-0`, and upstream drops it from the list so the positive is preferred.
			name:        "zero as the listed positive",
			source:      `const element = <div className="[margin-top:0px]" />;`,
			wantMessage: `The class: "[margin-top:0px]" can be simplified to "mt-0".`,
			wantFixed:   `const element = <div className="mt-0" />;` + "\n",
		},
		{
			// 90rem over 0.25rem is 360, within the largest breakpoint, so the fallback tries it.
			name:        "spacing multiple within the largest breakpoint",
			source:      `const element = <div className="max-w-[90rem]" />;`,
			wantMessage: `The class: "max-w-[90rem]" can be simplified to "max-w-360".`,
			wantFixed:   `const element = <div className="max-w-360" />;` + "\n",
		},
		{
			// optimizeArbitraryValueExpressions moves the sign into the root, then the list has `mt-48`.
			name:        "negated negative length",
			source:      `const element = <div className="-mt-[-12rem]" />;`,
			wantMessage: `The class: "-mt-[-12rem]" can be simplified to "mt-48".`,
			wantFixed:   `const element = <div className="mt-48" />;` + "\n",
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
			// Tailwind would rewrite it, through themeToVarUtility, which is not ported. Silent rather than
			// wrong, and listed in canonical_unported.json.
			name:   "a rewrite only an unported pass makes",
			source: `const element = <div className="w-[theme(maxWidth.md)]" />;`,
		},
		{
			// `max-w-sm` and `max-w-96` both sign as `max-width: 24rem`, two non-negative classes, so
			// upstream bails.
			name:   "two listed classes sign alike",
			source: `const element = <div className="max-w-[24rem]" />;`,
		},
		{
			// 100rem over 0.25rem is 400, past the largest breakpoint's 96rem, so upstream's fallback does
			// not offer `max-w-400` and the class stays as written.
			name:   "spacing multiple past the largest breakpoint",
			source: `const element = <div className="max-w-[100rem]" />;`,
		},
		{
			// A variable the theme does not hold signs as itself, and only the arbitrary spelling has it.
			name:   "a variable no listed class carries",
			source: `const element = <div className="ring-(--my-ring)" />;`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectClean(t, runCanonicalFixture(t, "Component.tsx", testCase.source))
		})
	}
}

// TestCanonicalSingleBailsOnARepositoryTwin is Structure's case: its `@utility background-*` lists
// `background-content-1` beside the framework's `bg-content-1`, two non-negative classes signing alike, so
// upstream leaves `bg-(--color-content-1)` as written. The control, the same theme without the block, has
// one listed class and reports. Measured against Tailwind 4.3.3 both ways.
func TestCanonicalSingleBailsOnARepositoryTwin(t *testing.T) {
	t.Parallel()
	source := `const element = <div className="bg-(--color-content-1)" />;`
	theme := "@import \"tailwindcss\";\n@theme {\n    --color-content-1: #123456;\n}\n"

	control := runCanonicalFixtureWithStylesheet(t, "Component.tsx", source, theme, nil)
	rule_testing.ExpectFindings(t, control, "canonicalSingle")
	if got := control.Diagnostics[0].Message.Description; got != `The class: "bg-(--color-content-1)" can be simplified to "bg-content-1".` {
		t.Errorf("control message: %s", got)
	}

	twinned := runCanonicalFixtureWithStylesheet(t, "Component.tsx", source,
		theme+"@utility background-* {\n    background-color: --value(--color-*);\n}\n", nil)
	rule_testing.ExpectClean(t, twinned)
}

// TestCanonicalSingleDeclinesARedeclaredUtility holds the guard for a root declared in two `@utility`
// blocks. Upstream registers both, so it lists `paint-a` from the first and `paint-b` from the second,
// both signing as `background-color: #111111`, and bails. The evaluator here keeps one block, and from
// its list alone `paint-b` would be the one match, which is the rewrite Tailwind does not make: the
// control, one block, is that rewrite. Measured against Tailwind 4.3.3 both ways.
func TestCanonicalSingleDeclinesARedeclaredUtility(t *testing.T) {
	t.Parallel()
	source := `const element = <div className="[background-color:#111111]" />;`
	theme := "@import \"tailwindcss\";\n@theme {\n    --paint-a: #111111;\n    --hue-b: #111111;\n}\n"
	second := "@utility paint-* {\n    background-color: --value(--hue-*);\n}\n"

	single := runCanonicalFixtureWithStylesheet(t, "Component.tsx", source, theme+second, nil)
	rule_testing.ExpectFindings(t, single, "canonicalSingle")

	redeclared := runCanonicalFixtureWithStylesheet(t, "Component.tsx", source,
		theme+"@utility paint-* {\n    background-color: --value(--paint-*);\n}\n"+second, nil)
	rule_testing.ExpectClean(t, redeclared)
}

// TestCanonicalSingleCountsAnUnknownClassTowardTheBail is condition 1 of #15swfzq unit 1b. With
// `--leading-one: 1` in the theme, `leading-one` and `leading-none` both sign as `line-height: 1`, and
// upstream bails on `[line-height:1]`. This port signs `leading-one` exactly and cannot sign
// `leading-none`, whose body is a named static value it does not hold, so the possible-match relation
// has to count it. Dropping it from the count would leave `leading-one` the one match and report a
// rewrite Tailwind does not make. Measured against Tailwind 4.3.3, which also rewrites to `leading-none`
// when the theme key is absent, a rewrite this port leaves unreported.
func TestCanonicalSingleCountsAnUnknownClassTowardTheBail(t *testing.T) {
	t.Parallel()
	result := runCanonicalFixtureWithStylesheet(t, "Component.tsx",
		`const element = <div className="[line-height:1]" />;`,
		"@import \"tailwindcss\";\n@theme {\n    --leading-one: 1;\n}\n", nil)
	rule_testing.ExpectClean(t, result)
}

// TestCanonicalSingleNeverNamesAShortReplacement holds the two places the pass declines rather than
// guess, each at a class upstream does rewrite. `[rotate:45deg]` reaches the fallback's arbitrary
// spelling `rotate-[45deg]`, which upstream's arbitraryValueToBareValueUtility, not ported, carries on to
// `rotate-45`. `[tab-size:7]` matches in two roots, the framework's `tab` and a repository `zeta`, and
// upstream takes the first registered, `tab-7`, an order this port does not hold. Either may be reported
// once the port can name Tailwind's answer, and neither may be reported naming another. Measured against
// Tailwind 4.3.3.
func TestCanonicalSingleNeverNamesAShortReplacement(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		source     string
		stylesheet string
		upstream   string
	}{
		{
			name:       "arbitrary spelling a later pass shortens",
			source:     `const element = <div className="[rotate:45deg]" />;`,
			stylesheet: `@import "tailwindcss";`,
			upstream:   `The class: "[rotate:45deg]" can be simplified to "rotate-45".`,
		},
		{
			name:       "two roots in the fallback",
			source:     `const element = <div className="[tab-size:7]" />;`,
			stylesheet: "@import \"tailwindcss\";\n@utility zeta-* {\n    tab-size: --value(integer);\n}\n",
			upstream:   `The class: "[tab-size:7]" can be simplified to "tab-7".`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := runCanonicalFixtureWithStylesheet(t, "Component.tsx", testCase.source, testCase.stylesheet, nil)
			for _, diagnostic := range result.Diagnostics {
				if got := diagnostic.Message.Description; got != testCase.upstream {
					t.Errorf("reported %q; the only report allowed is upstream's, %q", got, testCase.upstream)
				}
			}
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
