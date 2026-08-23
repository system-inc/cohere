package tailwind

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// Expectations measured by running `better-tailwindcss/no-conflicting-classes` over probe fixtures
// against the real plugin. Each line is a verdict, and the silent ones are the load-bearing half:
// three of them are cases a plausible port reports and upstream does not.
//
//	flex block                2 findings, one anchored at each class
//	px-4 px-8                 2 findings, on "padding-inline"
//	text-left text-right      2 findings, on "text-align"
//	hover:flex hover:block    2 findings, same variant
//	p-4 px-8                  silent, padding and padding-inline are different names
//	flex hover:block          silent, different states
//	w-8 h-8                   silent, width and height are different properties
//	mt-2 mb-2                 silent
//	flex items-center         silent

func TestNoConflictingClassesReportsSymmetrically(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
		wantIds  []string
	}{
		{
			// The headline case, and the one a table that lost `flex`'s static reading would miss.
			name:     "two display utilities",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex block" />;`,
			wantIds:  []string{"conflictingClasses", "conflictingClasses"},
		},
		{
			// Same property, different values.
			name:     "same functional root twice",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-4 px-8" />;`,
			wantIds:  []string{"conflictingClasses", "conflictingClasses"},
		},
		{
			name:     "two alignment utilities",
			fileName: "Component.tsx",
			source:   `const element = <div className="text-left text-right" />;`,
			wantIds:  []string{"conflictingClasses", "conflictingClasses"},
		},
		{
			// Same variant on both, so they genuinely collide in that state.
			name:     "conflict inside one variant",
			fileName: "Component.tsx",
			source:   `const element = <div className="hover:flex hover:block" />;`,
			wantIds:  []string{"conflictingClasses", "conflictingClasses"},
		},
		{
			name:     "on a callee surface",
			fileName: "Component.tsx",
			source:   `const merged = mergeClassNames('flex block');`,
			wantIds:  []string{"conflictingClasses", "conflictingClasses"},
		},
		{
			name:     "on a variable surface",
			fileName: "Styles.ts",
			source:   `const buttonClassName = 'px-4 px-8';`,
			wantIds:  []string{"conflictingClasses", "conflictingClasses"},
		},
		{
			// Three classes on one property is three findings, each naming the other two.
			name:     "three classes on one property",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex block grid" />;`,
			wantIds:  []string{"conflictingClasses", "conflictingClasses", "conflictingClasses"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, NoConflictingClasses, testCase.fileName, testCase.source)
			ruletest.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// The silent half, which for this rule is where the real difficulty is.
//
// Each of these is a case a reasonable port reports and upstream does not, so getting them wrong
// means firing on correct code, which is how a rule gets turned off.
func TestNoConflictingClassesStaysSilent(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
	}{
		{
			// Different properties under the same value. These collapse into `size-8`, which is
			// `enforce-canonical-classes`'s finding rather than this rule's.
			name:     "width and height",
			fileName: "Component.tsx",
			source:   `const element = <div className="w-8 h-8" />;`,
		},
		{
			// Shorthands are not normalised: `padding` and `padding-inline` are different names.
			name:     "padding and padding-inline",
			fileName: "Component.tsx",
			source:   `const element = <div className="p-4 px-8" />;`,
		},
		{
			// Different states, so both take effect where they belong. A port comparing bare class
			// names reports this and is wrong.
			name:     "same property under different variants",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex hover:block" />;`,
		},
		{
			name:     "different axes of margin",
			fileName: "Component.tsx",
			source:   `const element = <div className="mt-2 mb-2" />;`,
		},
		{
			name:     "unrelated properties",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex items-center gap-2" />;`,
		},
		{
			name:     "one class",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex" />;`,
		},
		{
			// A repeat is `no-duplicate-classes`'s finding. Pairing a class with itself would report
			// every duplicate in the tree as a conflict.
			name:     "the same class twice",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex flex" />;`,
		},
		{
			name:     "unrelated attribute",
			fileName: "Component.tsx",
			source:   `const element = <div title="flex block" />;`,
		},
		{
			name:     "unrelated callee",
			fileName: "Component.tsx",
			source:   `const value = someOtherFunction('flex block');`,
		},
		{
			// Different elements. `divide-*` emits under `:where(.CLASS > :not(:last-child))` and
			// styles children; `border-*` styles the element. Both declare `border-color` and they
			// do not collide. This is the pair that made the rule look engine-bound.
			name:     "divide against border",
			fileName: "Component.tsx",
			source:   `const element = <div className="divide-neutral-200 border-neutral-200" />;`,
		},
		{
			// Both declare `box-shadow` and both layer through their own custom properties, so they
			// compose rather than overwrite.
			name:     "shadow against ring",
			fileName: "Component.tsx",
			source:   `const element = <div className="shadow-lg ring-1" />;`,
		},
		{
			// `border` sets style plus width, `border-dashed` sets style alone. Overlapping is not
			// enough: the whole property set has to match.
			name:     "border against border-dashed",
			fileName: "Component.tsx",
			source:   `const element = <div className="border border-dashed" />;`,
		},
		{
			// `ring-inset` declares only a custom property, so it has nothing to collide with.
			name:     "ring against ring-inset",
			fileName: "Component.tsx",
			source:   `const element = <div className="ring-1 ring-inset" />;`,
		},
		{
			// `container` emits several rules and is deliberately absent from the tables, so it is
			// silent rather than wrong.
			name:     "container against w-full",
			fileName: "Component.tsx",
			source:   `const element = <div className="w-full container" />;`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, NoConflictingClasses, testCase.fileName, testCase.source)
			ruletest.ExpectClean(t, result)
		})
	}
}

// TestConflictingClassesProposeSuggestionsNotFixes guards the distinction that decides whether the
// rule silently rewrites markup.
//
// Upstream declares `autofix: true`. On our side removing either of two conflicting classes changes
// what renders, and only the author knows which one was meant, so this must be a Suggestion. The
// edit engine never applies suggestions, which makes the difference concrete: ported as a Fix it
// would silently pick one; ported as a suggestion a human chooses.
//
// This was caught by `@system_verify_lint_fix`'s research pass rather than by reading the source,
// where the `autofix` flag reads as an instruction.
func TestConflictingClassesProposeSuggestionsNotFixes(t *testing.T) {
	result := ruletest.Run(t, NoConflictingClasses, "Component.tsx",
		`const element = <div className="flex block" />;`)

	if len(result.Diagnostics) == 0 {
		t.Fatal("expected findings, got none")
	}
	for _, diagnostic := range result.Diagnostics {
		if len(diagnostic.Fixes) != 0 {
			t.Fatal("this rule must not propose an automatic fix: removing either conflicting class " +
				"changes what renders, and the edit engine would apply the choice unattended")
		}
	}
}

// TestPropertyLookupUsesTheLongestRoot is the known-dirty control.
//
// Resolving a class to its properties by taking the first dash-delimited segment is the obvious
// implementation and it is wrong in the direction that reports false conflicts: `border-l-4` would
// resolve as `border` rather than `border-l`, so it would be given `border-width` instead of
// `border-left-width` and would read as conflicting with `border-r-4`.
//
// That is the same shortcut, in a different guise, that silently stopped `border-x` being reported
// during the migration.
func TestPropertyLookupUsesTheLongestRoot(t *testing.T) {
	// The real lookup.
	left, canResolveLeft := resolveClassFacts("border-l-4")
	right, canResolveRight := resolveClassFacts("border-r-4")
	if !canResolveLeft || !canResolveRight {
		t.Fatal("border-l-4 or border-r-4 did not resolve, so this control proves nothing")
	}
	leftProperties, rightProperties := left.Properties, right.Properties

	if len(leftProperties) == 0 || len(rightProperties) == 0 {
		t.Fatal("border-l-4 or border-r-4 resolved to no properties, so this control proves nothing")
	}

	if len(sharedProperties(leftProperties, rightProperties)) != 0 {
		t.Errorf("border-l-4 declares %v and border-r-4 declares %v, which share a property. They "+
			"are different edges and must not be reported as conflicting.", leftProperties, rightProperties)
	}

	// And the shortcut it guards against: taking the first segment gives both the same root.
	if functionalRootOf("border-l-4") == "border" {
		t.Error("the root lookup returned the shortest match rather than the longest, which gives " +
			"every border edge the same properties")
	}

	// The lookup must also not let a shorter root match across a value boundary.
	if functionalRootOf("px-4") == "p" {
		t.Error("`p` matched `px-4`, so padding and padding-inline would be conflated and `p-4 px-8` " +
			"would report a conflict upstream does not report")
	}
}

// TestVariantsAreComparedNotStripped pins the distinction that decides false positives.
//
// Two classes conflict only in the same state. Comparing bare names reports `flex hover:block`,
// which is correct code, and a rule that fires on correct code is one somebody turns off.
func TestVariantsAreComparedNotStripped(t *testing.T) {
	sameVariant := ruletest.Run(t, NoConflictingClasses, "Component.tsx",
		`const element = <div className="hover:flex hover:block" />;`)
	if len(sameVariant.Diagnostics) == 0 {
		t.Fatal("two display utilities under the same variant do collide and must be reported")
	}

	differentVariant := ruletest.Run(t, NoConflictingClasses, "Component.tsx",
		`const element = <div className="flex hover:block" />;`)
	if len(differentVariant.Diagnostics) != 0 {
		t.Fatalf("classes in different states both take effect where they belong, so this is correct "+
			"code; got %d findings", len(differentVariant.Diagnostics))
	}
}
