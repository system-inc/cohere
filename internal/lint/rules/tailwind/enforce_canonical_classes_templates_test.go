package tailwind

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestCanonicalClassesInTemplateRuns reads the static runs of a template with holes (#j9yk44e).
//
// Measured on eslint-plugin-better-tailwindcss 4.9.1 under ESLint on 2026-11-07, with ahra's theme.
// Each piece of a template is judged as a literal, so a collapse pairs classes within one piece and
// never across a hole, and a class glued to a hole is judged as written:
//
//	`px-4 py-4 ${x}`      collapses to p-4
//	`w-8 h-8 ${x}`        collapses to size-8
//	`px-4 py-4${x}`       collapses to p-4, the glued py-4 included
//	`!px-4 !py-4 ${x}`    collapses to p-4!, with no single for either class
//	`px-4 ${x} py-4`      silent
//	`z-[1] ${x}`          z-[1] to z-1, fixed
//	`z-[1]${x} block`     z-[1] to z-1, which upstream fixes and cohere does not
//	`!flex${x} block`     !flex to flex!, likewise
//
// A collapse sits on its run, as a literal's sits on the literal, and offers no fix. A single sits on
// its own class.
func TestCanonicalClassesInTemplateRuns(test *testing.T) {
	test.Parallel()
	type finding struct {
		id      string
		covered string
		fixed   bool
	}
	testCases := []struct {
		name     string
		source   string
		findings []finding
		fixed    string
	}{
		{
			name:     "a collapse in a run beside a hole",
			source:   "const element = <div className={`block px-4 py-4 ${size}`} />;",
			findings: []finding{{id: "canonicalCollapse", covered: "block px-4 py-4 "}},
		},
		{
			name:     "a collapse across properties that share no name",
			source:   "const element = <div className={`w-8 h-8 ${size}`} />;",
			findings: []finding{{id: "canonicalCollapse", covered: "w-8 h-8 "}},
		},
		{
			name:     "a collapse with a class glued to the hole",
			source:   "const element = <div className={`px-4 py-4${size}`} />;",
			findings: []finding{{id: "canonicalCollapse", covered: "px-4 py-4"}},
		},
		{
			// !px-4 alone would be px-4!, but upstream reports a class in a collapse once, under the
			// collapse, so the run carries one finding.
			name:     "a collapse whose classes would each rewrite alone",
			source:   "const element = <div className={`!px-4 !py-4 ${size}`} />;",
			findings: []finding{{id: "canonicalCollapse", covered: "!px-4 !py-4 "}},
		},
		{
			name:     "no collapse across a hole",
			source:   "const element = <div className={`px-4 ${size} py-4`} />;",
			findings: []finding{},
		},
		{
			name:     "a single beside a hole, fixed",
			source:   "const element = <div className={`block z-[1] ${size}`} />;",
			findings: []finding{{id: "canonicalSingle", covered: "z-[1]", fixed: true}},
			fixed:    "const element = <div className={`block z-1 ${size}`} />;\n",
		},
		{
			name:     "a single glued before a hole, not fixed",
			source:   "const element = <div className={`z-[1]${size} block`} />;",
			findings: []finding{{id: "canonicalSingle", covered: "z-[1]"}},
		},
		{
			name:     "a single glued after a hole, not fixed",
			source:   "const element = <div className={`block ${size}!flex`} />;",
			findings: []finding{{id: "canonicalSingle", covered: "!flex"}},
		},
		{
			// The repeat is no-duplicate-classes's to delete, so only the first is fixed.
			name:   "a repeated single in one run",
			source: "const element = <div className={`z-[1] z-[1] ${size}`} />;",
			findings: []finding{
				{id: "canonicalSingle", covered: "z-[1]", fixed: true},
				{id: "canonicalSingle", covered: "z-[1]"},
			},
			fixed: "const element = <div className={`z-1 z-[1] ${size}`} />;\n",
		},
		{
			// An escape makes the run's source differ from its decoded value, so the finding sits on the
			// run, unfixed.
			name:     "a run holding an escape",
			source:   "const element = <div className={`z-[1]\\u0020block ${size}`} />;",
			findings: []finding{{id: "canonicalSingle", covered: "z-[1]\\u0020block "}},
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runCanonicalFixture(test, "Component.tsx", testCase.source)
			if len(result.Diagnostics) != len(testCase.findings) {
				for _, diagnostic := range result.Diagnostics {
					test.Logf("got %s: %s", diagnostic.Message.Id, diagnostic.Message.Description)
				}
				test.Fatalf("got %d findings, want %d", len(result.Diagnostics), len(testCase.findings))
			}
			text := result.SourceFile.Text()
			for index, diagnostic := range result.Diagnostics {
				want := testCase.findings[index]
				if diagnostic.Message.Id != want.id {
					test.Errorf("finding %d is %s, want %s", index, diagnostic.Message.Id, want.id)
				}
				if covered := text[diagnostic.Range.Pos():diagnostic.Range.End()]; covered != want.covered {
					test.Errorf("finding %d covers %q, want %q", index, covered, want.covered)
				}
				if hasFix := len(diagnostic.Fixes) > 0; hasFix != want.fixed {
					test.Errorf("finding %d has a fix: %t, want %t", index, hasFix, want.fixed)
				}
			}
			if testCase.fixed != "" {
				rule_testing.ExpectFixedSource(test, result, testCase.fixed)
			}
		})
	}
}
