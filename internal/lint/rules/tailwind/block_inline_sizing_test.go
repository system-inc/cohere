package tailwind

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// The functional `block-*` and `inline-*` classes, held rule by rule to what Tailwind 4.3.3 and
// better-tailwindcss 4.11.0 answer.
//
// `block` and `inline` are each two registrations upstream: the static `display` utility and a
// `spacingUtility` declaring `block-size` or `inline-size`. The port read only the first, so
// `no-unknown-classes` reported every value form and `enforce-consistent-class-order` hoisted them as
// classes Tailwind ranks null. Every expectation below was measured on Tailwind rather than reasoned:
// `candidatesToCss` for whether a class compiles, `getClassOrder` for the order, and the plugin's own
// `getCanonicalClasses` and `getConflictingClasses` run over a 4.3.3 design system for the other two.

// TestNoUnknownClassesKnowsTheSizingForms is Tailwind compiling each value form and refusing the
// negative one, which `spacingUtility` never registers because these roots pass no `supportsNegative`.
func TestNoUnknownClassesKnowsTheSizingForms(test *testing.T) {
	test.Parallel()
	for _, className := range []string{
		"inline-72", "block-80", "inline-1/3", "block-[13px]", "block-(--x)", "inline-px",
		"inline-72/50", "inline-lg", "block", "inline",
	} {
		test.Run(className, func(test *testing.T) {
			test.Parallel()
			result := runUnknownFixture(test, "Component.tsx", `const element = <div className="`+className+`" />;`)
			rule_testing.ExpectClean(test, result)
		})
	}
	for _, className := range []string{"-inline-4", "-block-4", "block-lg", "inline-3.7"} {
		test.Run(className, func(test *testing.T) {
			test.Parallel()
			result := runUnknownFixture(test, "Component.tsx", `const element = <div className="`+className+`" />;`)
			rule_testing.ExpectFindings(test, result, "unknownClass")
		})
	}
}

// TestClassOrderPlacesTheSizingForms is `getClassOrder` on a list mixing the logical and physical
// sizes. `block-size`, `inline-size` and `max-inline-size` are outside property-order.ts, so the three
// carry no position, sort after every class that has one, and tie down to their names.
func TestClassOrderPlacesTheSizingForms(test *testing.T) {
	test.Parallel()
	const tailwindOrder = "size-4 h-72 w-72 min-w-4 block-80 inline-72 max-inline-4"

	test.Run("misordered", func(test *testing.T) {
		test.Parallel()
		result := runClassOrderFixture(test, "Component.tsx",
			`const element = <div className="inline-72 w-72 h-72 block-80 size-4 min-w-4 max-inline-4" />;`)
		rule_testing.ExpectFixedSource(test, result, `const element = <div className="`+tailwindOrder+`" />;`+"\n")
	})
	test.Run("in Tailwind's order", func(test *testing.T) {
		test.Parallel()
		result := runClassOrderFixture(test, "Component.tsx", `const element = <div className="`+tailwindOrder+`" />;`)
		rule_testing.ExpectClean(test, result)
	})
}

// TestCanonicalClassesTreatTheSizingFormsAsUpstream is the plugin's defaults, collapse and
// logicalToPhysical on: no collapse family joins `inline-size` to `width` or `block-size` to `inline-size`,
// and an arbitrary value naming a theme value is written with its name.
func TestCanonicalClassesTreatTheSizingFormsAsUpstream(test *testing.T) {
	test.Parallel()
	for _, classes := range []string{
		"inline-4 w-4", "inline-4 block-4", "block-4 h-4", "block-[13px]", "inline-(--x)",
	} {
		test.Run(classes, func(test *testing.T) {
			test.Parallel()
			result := runCanonicalFixture(test, "Component.tsx", `const element = <div className="`+classes+`" />;`)
			rule_testing.ExpectClean(test, result)
		})
	}
	for _, testCase := range []struct{ className, canonical string }{
		{className: "inline-[1rem]", canonical: "inline-4"},
		{className: "block-[1rem]", canonical: "block-4"},
		{className: "inline-[100%]", canonical: "inline-full"},
	} {
		test.Run(testCase.className, func(test *testing.T) {
			test.Parallel()
			result := runCanonicalFixture(test, "Component.tsx",
				`const element = <div className="`+testCase.className+`" />;`)
			rule_testing.ExpectFindings(test, result, "canonicalSingle")
			if description := result.Diagnostics[0].Message.Description; !strings.Contains(
				description,
				"\""+testCase.canonical+"\"",
			) {
				test.Errorf("message does not name %q: %s", testCase.canonical, description)
			}
		})
	}
}

// TestConflictingClassesCompareTheSizingProperties is the plugin's rule, which reports two classes
// whose declared properties are the same set: `inline-size` against `width` is two properties and no
// conflict, two `inline-size` classes are one, and the bare `block` is `display` beside `block-size`.
func TestConflictingClassesCompareTheSizingProperties(test *testing.T) {
	test.Parallel()
	for _, classes := range []string{"inline-4 w-4", "block-4 h-4", "block block-4", "inline inline-4"} {
		test.Run(classes, func(test *testing.T) {
			test.Parallel()
			result := runConflictFixture(test, "Component.tsx", `const element = <div className="`+classes+`" />;`)
			rule_testing.ExpectClean(test, result)
		})
	}
	for _, classes := range []string{
		"inline-4 inline-8", "block-4 block-[13px]", "inline-4 inline-full", "block-(--x) block-80",
	} {
		test.Run(classes, func(test *testing.T) {
			test.Parallel()
			result := runConflictFixture(test, "Component.tsx", `const element = <div className="`+classes+`" />;`)
			rule_testing.ExpectFindings(test, result, "conflictingClasses", "conflictingClasses")
		})
	}
}
