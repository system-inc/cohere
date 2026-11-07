package tailwind

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
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

func TestNoDeprecatedClassesReportsRenames(test *testing.T) {
	test.Parallel()
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
			source:   `const merged = cn('flex-shrink-0');`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			name:     "on a variable surface",
			fileName: "Styles.ts",
			source:   `const className = 'overflow-ellipsis';`,
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			// Two deprecated classes in one literal are two findings, not one.
			name:     "two deprecated classes",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex-shrink-0 overflow-ellipsis" />;`,
			wantIds:  []string{"deprecatedClassReplaceable", "deprecatedClassReplaceable"},
		},
		{
			// ahra's AdhocProposalPanel.tsx:150, which only ESLint reported until the rule read runs.
			name:     "in a template with a hole",
			fileName: "Component.tsx",
			source:   "const element = <div className={`self-start rounded px-2 ${tone ?? ''}`} />;",
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			name:     "glued to a hole",
			fileName: "Component.tsx",
			source:   "const element = <div className={`rounded${size} px-2`} />;",
			wantIds:  []string{"deprecatedClassReplaceable"},
		},
		{
			// Every run is read, the removed kind included.
			name:     "in each run of a template",
			fileName: "Component.tsx",
			source:   "const element = <div className={`flex ${x} rounded flex-grow ${y} bg-opacity-50`} />;",
			wantIds: []string{
				"deprecatedClassReplaceable",
				"deprecatedClassReplaceable",
				"deprecatedClassIrreplaceable",
			},
		},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoDeprecatedClasses, testCase.fileName, testCase.source)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

func TestNoDeprecatedClassesStaysSilent(test *testing.T) {
	test.Parallel()
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
		{
			name:     "a template with a hole and nothing deprecated",
			fileName: "Component.tsx",
			source:   "const element = <div className={`flex ${x} px-2 rounded-sm`} />;",
		},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoDeprecatedClasses, testCase.fileName, testCase.source)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestNoDeprecatedClassesFixMatchesUpstream checks the rewrite, which is the half that lands
// unattended.
//
// The variant and importance cases are the ones worth pinning: the rule matches a stripped base and
// has to put the prefix back, so a port that got the reassembly wrong would produce a class that
// renders differently rather than one that fails to compile.
func TestNoDeprecatedClassesFixMatchesUpstream(test *testing.T) {
	test.Parallel()
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
			// Two findings, each renaming its own class. One rewrite of the whole literal attached to
			// both proposed the same edit twice, and the engine refused the second.
			name:     "two deprecated classes, two disjoint renames",
			source:   `const element = <div className="flex-shrink-0 flex flex-grow" />;`,
			wantText: "shrink-0 flex grow",
		},
		{
			// A repeat is `no-duplicate-classes`'s to delete, so it is reported and not renamed.
			name:     "a repeated deprecated class is renamed once",
			source:   `const element = <div className="flex-shrink-0 flex-shrink-0" />;`,
			wantText: "shrink-0 flex-shrink-0",
		},
		{
			name:     "capture is carried into the replacement",
			source:   `const element = <div className="flex-grow-2" />;`,
			wantText: "grow-2",
		},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoDeprecatedClasses, "Component.tsx", testCase.source)
			if len(result.Diagnostics) == 0 {
				test.Fatal("expected a finding, got none")
			}
			// The literal's contents after the fix, read back out of the rewritten source so the
			// cases stay about class text rather than about the JSX around it.
			const opening = `className="`
			start := strings.Index(testCase.source, opening) + len(opening)
			end := strings.LastIndex(testCase.source, `"`)
			want := testCase.source[:start] + testCase.wantText + testCase.source[end:]
			rule_testing.ExpectFixedSource(test, result, want)
		})
	}
}

// TestNoDeprecatedClassesInTemplateRuns pins where a template's findings land and which are fixed.
//
// Measured against eslint-plugin-better-tailwindcss 4.9.1 under ESLint on 2026-11-06, with ahra's
// theme: each class is reported at its own range and renamed in place.
//
//	`self-start rounded px-2 ${tone ?? ''}`               rounded, fixed to rounded-sm
//	`rounded${x} px-2`                                     rounded, fixed to rounded-sm${x}
//	`px-2 ${x}rounded`                                     rounded, fixed to ${x}rounded-sm
//	`flex ${x} rounded flex-grow ${y} bg-opacity-50`       three findings, two fixed
//	`flex ${x} px-2`                                       silent
//
// The glued rows are where cohere reports and does not fix, on purpose: the hole completes the class,
// so the rename can turn `rounded-lg` into `rounded-sm-lg` (reportTemplateDeprecations says why).
func TestNoDeprecatedClassesInTemplateRuns(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name    string
		source  string
		classes []string
		fixed   string
	}{
		{
			name:    "a run beside a hole",
			source:  "const element = <div className={`self-start rounded px-2 ${tone ?? ''}`} />;",
			classes: []string{"rounded"},
			fixed:   "const element = <div className={`self-start rounded-sm px-2 ${tone ?? ''}`} />;",
		},
		{
			name:    "glued before a hole",
			source:  "const element = <div className={`rounded${size} px-2`} />;",
			classes: []string{"rounded"},
		},
		{
			name:    "glued after a hole",
			source:  "const element = <div className={`px-2 ${size}rounded`} />;",
			classes: []string{"rounded"},
		},
		{
			name:    "every run, each class renamed in place",
			source:  "const element = <div className={`flex ${x} rounded flex-grow ${y} bg-opacity-50`} />;",
			classes: []string{"rounded", "flex-grow", "bg-opacity-50"},
			fixed:   "const element = <div className={`flex ${x} rounded-sm grow ${y} bg-opacity-50`} />;",
		},
		{
			// The repeat is no-duplicate-classes's to delete, so only the first is renamed.
			name:    "a repeat in one run",
			source:  "const element = <div className={`rounded rounded ${x}`} />;",
			classes: []string{"rounded", "rounded"},
			fixed:   "const element = <div className={`rounded-sm rounded ${x}`} />;",
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoDeprecatedClasses, "Component.tsx", testCase.source)
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
			if testCase.fixed == "" {
				for _, diagnostic := range result.Diagnostics {
					if len(diagnostic.Fixes) != 0 {
						test.Errorf(
							"a class glued to a hole was given a fix, which can rename a class the hole completes",
						)
					}
				}
				return
			}
			rule_testing.ExpectFixedSource(test, result, testCase.fixed)
		})
	}
}

// TestRemovedUtilitiesProposeNoFix guards a deliberate absence.
//
// `bg-opacity-50` has no mechanical replacement: the opacity moved into the color, so the repair is
// `bg-black/50` and requires knowing which color the author meant. A fix here would have to invent
// one.
func TestRemovedUtilitiesProposeNoFix(test *testing.T) {
	test.Parallel()
	result := rule_testing.Run(test, NoDeprecatedClasses, "Component.tsx",
		`const element = <div className="bg-opacity-50" />;`)

	if len(result.Diagnostics) != 1 {
		test.Fatalf("expected one finding, got %d", len(result.Diagnostics))
	}
	if len(result.Diagnostics[0].Fixes) != 0 {
		test.Fatal("a removed utility has no mechanical replacement, so proposing a fix would invent one")
	}
}

// TestMatchingTheRawClassNameLosesFindings is the known-dirty control.
//
// The obvious implementation matches the deprecation patterns against the class as written. It
// passes every bare fixture and silently misses every prefixed one, which in real markup is most of
// them. This asserts the real rule catches what that shortcut drops.
func TestMatchingTheRawClassNameLosesFindings(test *testing.T) {
	test.Parallel()
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
			test.Fatalf("%q matched a deprecation pattern raw, so this control no longer demonstrates "+
				"the defect it exists to demonstrate", className)
		}

		// The real rule, which dissects first.
		if _, isDeprecated := deprecationFor(className); !isDeprecated {
			test.Errorf("the real rule missed %q, which is the prefixed case the raw match loses", className)
		}
	}
}

// TestVersionGateHoldsDeprecationsBack covers the case where a project is on an older Tailwind.
//
// A 4.1 rename is not a defect for someone on 4.0, and reporting it would be noise on correct code.
// The gate is exercised by pinning the version rather than by installing another Tailwind.
// Not parallel: it swaps the package variable tailwindVersionForDeprecations, which NoDeprecatedClasses
// reads on every run, and restores it with a defer.
func TestVersionGateHoldsDeprecationsBack(test *testing.T) {
	original := tailwindVersionForDeprecations
	defer func() { tailwindVersionForDeprecations = original }()

	// `bg-left-top` was deprecated in 4.1, `flex-shrink-0` in 4.0.
	tailwindVersionForDeprecations = func() string { return "4.0.0" }

	if _, isDeprecated := deprecationFor("bg-left-top"); isDeprecated {
		test.Error("a 4.1 rename should not be reported against Tailwind 4.0")
	}
	if _, isDeprecated := deprecationFor("flex-shrink-0"); !isDeprecated {
		test.Error("a 4.0 rename should still be reported against Tailwind 4.0")
	}

	// An unreadable version disables every deprecation rather than enabling all of them, because
	// silence is a gap someone notices on upgrade while false renames are noise on correct code.
	tailwindVersionForDeprecations = func() string { return "not-a-version" }
	if _, isDeprecated := deprecationFor("flex-shrink-0"); isDeprecated {
		test.Error("an unreadable version should disable deprecations rather than enable them")
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
func TestImportanceIsStrippedBeforeMatching(test *testing.T) {
	test.Parallel()
	variants, base, important := dissectClass("sm:hover:flex-grow-2!")

	if variants != "sm:hover:" {
		test.Errorf("variants %q, want %q", variants, "sm:hover:")
	}
	if base != "flex-grow-2" {
		test.Errorf("base %q, want %q \u2014 the deprecation patterns match the base, so punctuation left "+
			"here would be carried into a capture", base, "flex-grow-2")
	}
	if !important {
		test.Error("the importance marker was not recognised, so the fix would drop it")
	}

	if rebuilt := buildClass(variants, base, important); rebuilt != "sm:hover:flex-grow-2!" {
		test.Errorf("reassembly produced %q, so a fix would rewrite the class into something the author "+
			"did not write", rebuilt)
	}
}

// Not parallel: testing.AllocsPerRun refuses to run in a parallel test, because other tests'
// allocations would be counted as this one's.
//
// TestParseTailwindVersionReadsTwoFieldsWithoutAllocating pins what the version parse answers and that
// it answers without allocating, since it runs for every deprecation entry against every class the
// rule reads (#smshtp5: 670K objects a cold ahra run when it split the string each time).
func TestParseTailwindVersionReadsTwoFieldsWithoutAllocating(test *testing.T) {
	testCases := []struct {
		version      string
		major, minor int
	}{
		{"4.3.3", 4, 3},
		{"4.1", 4, 1},
		{"10.20.30-beta.1", 10, 20},
		{"4", 0, 0},
		{"", 0, 0},
		{"not-a-version", 0, 0},
		{"4.x", 0, 0},
		{"4..1", 0, 0},
		{".4", 0, 0},
	}
	for _, testCase := range testCases {
		major, minor := parseTailwindVersion(testCase.version)
		if major != testCase.major || minor != testCase.minor {
			test.Errorf("%q: read %d.%d, want %d.%d", testCase.version, major, minor, testCase.major, testCase.minor)
		}
	}

	if allocations := testing.AllocsPerRun(100, func() { parseTailwindVersion("4.3.3") }); allocations != 0 {
		test.Errorf("parsing a version allocated %.0f times per call, want 0", allocations)
	}
}
