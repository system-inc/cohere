package tailwind

import (
	"testing"

	"github.com/system-inc/cohere/internal/rule_testing"
)

// Expectations measured by running `better-tailwindcss/no-deprecated-classes` over probe fixtures
// against the real plugin, including the exact replacement text it proposes.
//
//	flex-shrink-0            -> shrink-0
//	hover:flex-shrink-0      -> hover:shrink-0
//	flex-shrink-0!           -> shrink-0!
//	sm:hover:flex-grow-2     -> sm:hover:grow-2
//	bg-opacity-50             irreplaceable, no fix offered
//	overflow-ellipsis        -> text-ellipsis
//	shadow                   -> shadow-sm
//	rounded                  -> rounded-sm
//	bg-left-top              -> bg-top-left
//	shrink-0                  silent
//	flex items-center         silent

func TestNoDeprecatedClassesReportsRenames(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
		wantIds  []string
	}{
		{
			name:     "renamed utility",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex-shrink-0" />;`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			// The case a port matching the raw class name would miss, and prefixed uses are most of
			// real markup.
			name:     "renamed utility behind a variant",
			fileName: "Component.tsx",
			source:   `const element = <div className="hover:flex-shrink-0" />;`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			name:     "renamed utility marked important",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex-shrink-0!" />;`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			name:     "renamed utility behind stacked variants",
			fileName: "Component.tsx",
			source:   `const element = <div className="sm:hover:flex-grow-2" />;`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			// Removed rather than renamed: opacity moved into the color.
			name:     "removed utility with no replacement",
			fileName: "Component.tsx",
			source:   `const element = <div className="bg-opacity-50" />;`,
			wantIds:  []string{"deprecatedClassIrreplaceable"},
		},
		{
			name:     "renamed to a different family",
			fileName: "Component.tsx",
			source:   `const element = <div className="overflow-ellipsis" />;`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			// The scale shift, which is the surprising kind: `shadow` still resolves, to a different
			// size than it used to.
			name:     "unsuffixed scale step",
			fileName: "Component.tsx",
			source:   `const element = <div className="shadow" />;`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			name:     "rounded is the same shift",
			fileName: "Component.tsx",
			source:   `const element = <div className="rounded" />;`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			// Deprecated in 4.1 rather than 4.0, so this also exercises the version gate.
			name:     "position renamed in a later minor",
			fileName: "Component.tsx",
			source:   `const element = <div className="bg-left-top" />;`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			name:     "on a callee surface",
			fileName: "Component.tsx",
			source:   `const merged = mergeClassNames('flex-shrink-0');`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			name:     "on a variable surface",
			fileName: "Styles.ts",
			source:   `const buttonClassName = 'overflow-ellipsis';`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			// Two deprecated classes in one literal are two findings, not one.
			name:     "two deprecated classes",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex-shrink-0 overflow-ellipsis" />;`,
			wantIds:  []string{"deprecatedClassReplaceable", "deprecatedClassReplaceable"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, NoDeprecatedClasses, testCase.fileName, testCase.source)
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

func TestNoDeprecatedClassesStaysSilent(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
	}{
		{
			// The current spelling of a renamed utility must not be reported as deprecated itself.
			name:     "the replacement is not deprecated",
			fileName: "Component.tsx",
			source:   `const element = <div className="shrink-0" />;`,
		},
		{
			name:     "current scale spelling",
			fileName: "Component.tsx",
			source:   `const element = <div className="shadow-sm rounded-sm" />;`,
		},
		{
			name:     "unrelated classes",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex items-center gap-2" />;`,
		},
		{
			// A near-miss: `shrink` is current, and the pattern for `flex-shrink` must not reach it.
			name:     "shrink without the flex prefix",
			fileName: "Component.tsx",
			source:   `const element = <div className="shrink grow" />;`,
		},
		{
			// The renamed position in its current order.
			name:     "current position spelling",
			fileName: "Component.tsx",
			source:   `const element = <div className="bg-top-left object-bottom-right" />;`,
		},
		{
			name:     "unrelated attribute",
			fileName: "Component.tsx",
			source:   `const element = <div title="flex-shrink-0" />;`,
		},
		{
			name:     "unrelated callee",
			fileName: "Component.tsx",
			source:   `const value = someOtherFunction('flex-shrink-0');`,
		},
		{
			name:     "unrelated variable",
			fileName: "Styles.ts",
			source:   `const description = 'flex-shrink-0';`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, NoDeprecatedClasses, testCase.fileName, testCase.source)
			rule_testing.ExpectClean(t, result)
		})
	}
}

// TestNoDeprecatedClassesFixMatchesUpstream checks the rewrite, which is the half that lands
// unattended.
//
// The variant and importance cases are the ones worth pinning: the rule matches a stripped base and
// has to put the prefix back, so a port that got the reassembly wrong would produce a class that
// renders differently rather than one that fails to compile.
func TestNoDeprecatedClassesFixMatchesUpstream(t *testing.T) {
	testCases := []struct {
		name     string
		source   string
		wantText string
	}{
		{
			name:     "bare rename",
			source:   `const element = <div className="flex-shrink-0" />;`,
			wantText: "shrink-0",
		},
		{
			name:     "variant is preserved",
			source:   `const element = <div className="hover:flex-shrink-0" />;`,
			wantText: "hover:shrink-0",
		},
		{
			name:     "importance is preserved",
			source:   `const element = <div className="flex-shrink-0!" />;`,
			wantText: "shrink-0!",
		},
		{
			name:     "stacked variants are preserved",
			source:   `const element = <div className="sm:hover:flex-grow-2" />;`,
			wantText: "sm:hover:grow-2",
		},
		{
			name:     "neighbours are left alone",
			source:   `const element = <div className="flex flex-shrink-0 gap-2" />;`,
			wantText: "flex shrink-0 gap-2",
		},
		{
			name:     "capture is carried into the replacement",
			source:   `const element = <div className="flex-grow-2" />;`,
			wantText: "grow-2",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, NoDeprecatedClasses, "Component.tsx", testCase.source)
			if len(result.Diagnostics) == 0 {
				t.Fatal("expected a finding, got none")
			}
			fixes := result.Diagnostics[0].Fixes
			if len(fixes) != 1 {
				t.Fatalf("expected one fix, got %d", len(fixes))
			}
			if fixes[0].Text != testCase.wantText {
				t.Errorf("fix writes %q, want %q", fixes[0].Text, testCase.wantText)
			}
		})
	}
}

// TestRemovedUtilitiesProposeNoFix guards a deliberate absence.
//
// `bg-opacity-50` has no mechanical replacement: the opacity moved into the color, so the repair is
// `bg-black/50` and requires knowing which color the author meant. A fix here would have to invent
// one.
func TestRemovedUtilitiesProposeNoFix(t *testing.T) {
	result := rule_testing.Run(t, NoDeprecatedClasses, "Component.tsx",
		`const element = <div className="bg-opacity-50" />;`)

	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected one finding, got %d", len(result.Diagnostics))
	}
	if len(result.Diagnostics[0].Fixes) != 0 {
		t.Fatal("a removed utility has no mechanical replacement, so proposing a fix would invent one")
	}
}

// TestMatchingTheRawClassNameLosesFindings is the known-dirty control.
//
// The obvious implementation matches the deprecation patterns against the class as written. It
// passes every bare fixture and silently misses every prefixed one, which in real markup is most of
// them. This asserts the real rule catches what that shortcut drops.
func TestMatchingTheRawClassNameLosesFindings(t *testing.T) {
	// Variant-prefixed only. `flex-shrink-0!` is deliberately absent: its `!` is swallowed by the
	// `(.*)` capture, so the raw match happens to hit it and it would not demonstrate the defect.
	// It would also produce a wrong replacement (`shrink-0!` via the capture rather than via
	// reassembly), which is a different bug this control is not about.
	prefixed := []string{
		"hover:flex-shrink-0",
		"sm:hover:flex-grow-2",
		"md:overflow-ellipsis",
	}

	for _, className := range prefixed {
		// The shortcut: match the whole class against the patterns.
		matchedRaw := false
		for _, entry := range deprecations {
			if entry.Pattern.MatchString(className) {
				matchedRaw = true
				break
			}
		}
		if matchedRaw {
			t.Fatalf("%q matched a deprecation pattern raw, so this control no longer demonstrates "+
				"the defect it exists to demonstrate", className)
		}

		// The real rule, which dissects first.
		if _, isDeprecated := deprecationFor(className); !isDeprecated {
			t.Errorf("the real rule missed %q, which is the prefixed case the raw match loses", className)
		}
	}
}

// TestVersionGateHoldsDeprecationsBack covers the case where a project is on an older Tailwind.
//
// A 4.1 rename is not a defect for someone on 4.0, and reporting it would be noise on correct code.
// The gate is exercised by pinning the version rather than by installing another Tailwind.
func TestVersionGateHoldsDeprecationsBack(t *testing.T) {
	original := tailwindVersionForDeprecations
	defer func() { tailwindVersionForDeprecations = original }()

	// `bg-left-top` was deprecated in 4.1, `flex-shrink-0` in 4.0.
	tailwindVersionForDeprecations = func() string { return "4.0.0" }

	if _, isDeprecated := deprecationFor("bg-left-top"); isDeprecated {
		t.Error("a 4.1 rename should not be reported against Tailwind 4.0")
	}
	if _, isDeprecated := deprecationFor("flex-shrink-0"); !isDeprecated {
		t.Error("a 4.0 rename should still be reported against Tailwind 4.0")
	}

	// An unreadable version disables every deprecation rather than enabling all of them, because
	// silence is a gap someone notices on upgrade while false renames are noise on correct code.
	tailwindVersionForDeprecations = func() string { return "not-a-version" }
	if _, isDeprecated := deprecationFor("flex-shrink-0"); isDeprecated {
		t.Error("an unreadable version should disable deprecations rather than enable them")
	}
}

// TestImportanceIsStrippedBeforeMatching pins a case that produces a wrong answer rather than no
// answer, which is the more dangerous of the two.
//
// `flex-shrink-0!` matches `^flex-shrink-(.*)$` even without dissection, because the `(.*)` capture
// swallows the importance marker. So the raw-match shortcut does not lose this finding; it reports
// it with the wrong replacement, expanding the capture to `0!` and producing `shrink-0!` by
// coincidence. Coincidence is not a property, and it breaks on any pattern whose replacement is not
// a suffix append: a raw match on `md:overflow-ellipsis` yields nothing at all.
//
// Asserting the dissection directly means a future change to the patterns cannot quietly start
// relying on the capture to carry punctuation.
func TestImportanceIsStrippedBeforeMatching(t *testing.T) {
	variants, base, important := dissectClass("sm:hover:flex-grow-2!")

	if variants != "sm:hover:" {
		t.Errorf("variants %q, want %q", variants, "sm:hover:")
	}
	if base != "flex-grow-2" {
		t.Errorf("base %q, want %q — the deprecation patterns match the base, so punctuation left "+
			"here would be carried into a capture", base, "flex-grow-2")
	}
	if !important {
		t.Error("the importance marker was not recognised, so the fix would drop it")
	}

	if rebuilt := buildClass(variants, base, important); rebuilt != "sm:hover:flex-grow-2!" {
		t.Errorf("reassembly produced %q, so a fix would rewrite the class into something the author "+
			"did not write", rebuilt)
	}
}
