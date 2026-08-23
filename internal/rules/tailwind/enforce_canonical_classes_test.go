package tailwind

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// The fixture corpus is the one the migration earned, and every entry in it was a real finding at
// some point. Several are the specific finding a cheaper approach silently lost.

func TestEnforceCanonicalClassesReportsCollapses(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
		wantIds  []string
	}{
		{
			name:     "padding axes",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-4 py-4" />;`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			// Different CSS properties merging into a shorthand. Filtering pairs by declared property
			// family measured fifteen times faster and silently stopped reporting this.
			name:     "width and height into size",
			fileName: "Component.tsx",
			source:   `const element = <div className="w-8 h-8" />;`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			// Roots with no value at all. A dash-splitter reads `border-l` as root `border` value
			// `l` and stops reporting this.
			name:     "border edges",
			fileName: "Component.tsx",
			source:   `const element = <div className="border-l border-r" />;`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			// A named scale rather than a numeric one, which the generator's first probe could not
			// reach at all.
			name:     "rounded corners",
			fileName: "Component.tsx",
			source:   `const element = <div className="rounded-tl-md rounded-tr-md" />;`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			name:     "gap axes",
			fileName: "Component.tsx",
			source:   `const element = <div className="gap-x-2 gap-y-2" />;`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			// Variants match on both sides, so the merge is allowed and carries the variant through.
			name:     "collapse under a shared variant",
			fileName: "Component.tsx",
			source:   `const element = <div className="sm:px-4 sm:py-4" />;`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			// Negative roots are their own roots rather than a sign flag on a positive one.
			name:     "negative margins",
			fileName: "Component.tsx",
			source:   `const element = <div className="-mt-1 -mb-1" />;`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			// Arbitrary values merge like named ones, as long as both sides carry the same value.
			name:     "arbitrary values",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-[3px] py-[3px]" />;`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			// Recursion: three merges to reach `m-1`, reported as three steps. The engine reports one
			// finding for the whole set; reporting each step is more actionable and does not change
			// what the author has to do.
			name:     "four margins collapse through three steps",
			fileName: "Component.tsx",
			source:   `const element = <div className="mt-1 mb-1 ml-1 mr-1" />;`,
			wantIds:  []string{"canonicalCollapse", "canonicalCollapse", "canonicalCollapse"},
		},
		{
			name:     "on a callee surface",
			fileName: "Component.tsx",
			source:   `const merged = mergeClassNames('px-4 py-4');`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			name:     "on a variable surface",
			fileName: "Styles.ts",
			source:   `const buttonClassName = 'w-8 h-8';`,
			wantIds:  []string{"canonicalCollapse"},
		},
		{
			// Unrelated neighbours must not prevent the merge or join it.
			name:     "collapse among other classes",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex px-4 items-center py-4 gap-2" />;`,
			wantIds:  []string{"canonicalCollapse"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, EnforceCanonicalClasses, testCase.fileName, testCase.source)
			ruletest.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// The clean half. The near-misses are the load-bearing ones: each differs from a real collapse in
// exactly one of the three preconditions, and relaxing any of them reports correct code.
func TestEnforceCanonicalClassesStaysSilent(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
	}{
		{
			// Values differ, so nothing merges.
			name:     "different values",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-4 py-2" />;`,
		},
		{
			name:     "different values on width and height",
			fileName: "Component.tsx",
			source:   `const element = <div className="w-8 h-4" />;`,
		},
		{
			// Variants differ, so the two apply in different states and cannot merge.
			name:     "one side carries a variant",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-4 sm:py-4" />;`,
		},
		{
			// Importance differs.
			name:     "one side is important",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-4 py-4!" />;`,
		},
		{
			// Roots that are not a family, however similar they look.
			name:     "unrelated roots",
			fileName: "Component.tsx",
			source:   `const element = <div className="ml-2 pt-2" />;`,
		},
		{
			name:     "single class",
			fileName: "Component.tsx",
			source:   `const element = <div className="ml-2" />;`,
		},
		{
			name:     "statics",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex items-center truncate" />;`,
		},
		{
			// A collapse is not a duplicate: `space-x-2` alone has nothing to merge with.
			name:     "no pair present",
			fileName: "Component.tsx",
			source:   `const element = <div className="space-x-2 text-left" />;`,
		},
		{
			name:     "unrelated attribute",
			fileName: "Component.tsx",
			source:   `const element = <div title="px-4 py-4" />;`,
		},
		{
			name:     "unrelated callee",
			fileName: "Component.tsx",
			source:   `const value = someOtherFunction('px-4 py-4');`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, EnforceCanonicalClasses, testCase.fileName, testCase.source)
			ruletest.ExpectClean(t, result)
		})
	}
}

// TestCanonicalMessageNamesTheShorterSpelling checks the part an author acts on.
//
// A finding that says only "these collapse" leaves the reader to work out into what, which for
// `w-8 h-8` is `size-8` and is not guessable from the class names.
func TestCanonicalMessageNamesTheShorterSpelling(t *testing.T) {
	testCases := []struct {
		source string
		want   string
	}{
		{source: `const element = <div className="px-4 py-4" />;`, want: "p-4"},
		{source: `const element = <div className="w-8 h-8" />;`, want: "size-8"},
		{source: `const element = <div className="border-l border-r" />;`, want: "border-x"},
		{source: `const element = <div className="sm:px-4 sm:py-4" />;`, want: "sm:p-4"},
		{source: `const element = <div className="px-[3px] py-[3px]" />;`, want: "p-[3px]"},
		{source: `const element = <div className="rounded-tl-md rounded-tr-md" />;`, want: "rounded-t-md"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			result := ruletest.Run(t, EnforceCanonicalClasses, "Component.tsx", testCase.source)
			if len(result.Diagnostics) == 0 {
				t.Fatal("expected a finding, got none")
			}
			description := result.Diagnostics[0].Message.Description
			if !strings.Contains(description, "\""+testCase.want+"\"") {
				t.Errorf("message does not name %q: %s", testCase.want, description)
			}
		})
	}
}

// TestCanonicalProposesNoFix guards a deliberate absence.
//
// The collapse is mechanical; splicing the replacement into a class list is not, because the
// resulting order is `enforce-consistent-class-order`'s concern and a rewrite that also reordered
// would fight it.
func TestCanonicalProposesNoFix(t *testing.T) {
	result := ruletest.Run(t, EnforceCanonicalClasses, "Component.tsx",
		`const element = <div className="px-4 py-4" />;`)

	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected one finding, got %d", len(result.Diagnostics))
	}
	if len(result.Diagnostics[0].Fixes) != 0 || len(result.Diagnostics[0].Suggestions) != 0 {
		t.Fatal("this rule reports the shorter spelling and leaves the rewrite to the author, because " +
			"splicing it in decides an ordering another rule owns")
	}
}

// TestPreconditionsAreCheckedNotAssumed is the known-dirty control.
//
// The tempting implementation looks up the two roots in the table and stops there. It passes every
// violation fixture above and reports `px-4 py-2`, `px-4 sm:py-4` and `px-4 py-4!`, all of which are
// correct code. A rule that fires on correct code is one somebody turns off, so the preconditions
// are the rule rather than a detail of it.
func TestPreconditionsAreCheckedNotAssumed(t *testing.T) {
	correctCode := []struct {
		name   string
		source string
	}{
		{name: "values differ", source: `const element = <div className="px-4 py-2" />;`},
		{name: "variants differ", source: `const element = <div className="px-4 sm:py-4" />;`},
		{name: "importance differs", source: `const element = <div className="px-4 py-4!" />;`},
	}

	for _, testCase := range correctCode {
		result := ruletest.Run(t, EnforceCanonicalClasses, "Component.tsx", testCase.source)
		if len(result.Diagnostics) != 0 {
			t.Errorf("%s: reported %d findings on correct code, so a precondition is not being checked",
				testCase.name, len(result.Diagnostics))
		}

		// And the control has to be able to fail: the same roots with everything matching must
		// report, or the silence above proves only that the table lookup is broken.
		matching := ruletest.Run(t, EnforceCanonicalClasses, "Component.tsx",
			`const element = <div className="px-4 py-4" />;`)
		if len(matching.Diagnostics) == 0 {
			t.Fatal("the rule stayed silent on a real collapse, so the silent cases above prove nothing")
		}
	}
}

// TestLongestRootWinsInCollapse pins the root resolution that has been wrong three times.
//
// `border-l` is a root in its own right, not `border` with value `l`. Taking the shorter match
// makes `border-l` and `border-r` look like the same root with different values, which fails the
// merge-key test and silently stops reporting `border-x`.
func TestLongestRootWinsInCollapse(t *testing.T) {
	parts, canParse := splitCandidate("border-l")
	if !canParse {
		t.Fatal("border-l did not resolve to a root at all")
	}
	if parts.Root != "border-l" {
		t.Errorf("border-l resolved to root %q with value %q, want root border-l and no value. "+
			"Taking the shorter match is what silently stopped border-x being reported.",
			parts.Root, parts.Value)
	}
	if parts.Value != "" {
		t.Errorf("border-l carries value %q, want none", parts.Value)
	}

	// And the value boundary: `p` must not claim `px-4`.
	pxParts, canParsePx := splitCandidate("px-4")
	if !canParsePx {
		t.Fatal("px-4 did not resolve to a root")
	}
	if pxParts.Root != "px" {
		t.Errorf("px-4 resolved to root %q, want px. A root matching across a value boundary would "+
			"conflate padding with padding-inline.", pxParts.Root)
	}
}

// TestIgnoredClassesAreExempt covers the option the oxlint configuration actually supplies.
//
// A rule declaring no options at all made oxlint refuse the whole plugin with "does not accept
// options", which failed as a silent zero-finding run rather than a crash.
func TestIgnoredClassesAreExempt(t *testing.T) {
	options := EnforceCanonicalClassesOptions{Ignore: []string{`^px-4$`}}

	result := ruletest.RunWithOptions(t, EnforceCanonicalClasses, "Component.tsx",
		`const element = <div className="px-4 py-4" />;`, options)
	if len(result.Diagnostics) != 0 {
		t.Errorf("an ignored class should not participate in a collapse, got %d findings",
			len(result.Diagnostics))
	}

	// Without the exemption the same source reports, so the test above is not passing because the
	// rule is broken.
	unignored := ruletest.Run(t, EnforceCanonicalClasses, "Component.tsx",
		`const element = <div className="px-4 py-4" />;`)
	if len(unignored.Diagnostics) == 0 {
		t.Fatal("the rule stayed silent without any ignore option, so the exemption proves nothing")
	}
}
