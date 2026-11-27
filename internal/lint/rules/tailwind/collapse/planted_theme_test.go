package tailwind

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rules/tailwind/vendored"
)

// Tailwind assertions asked of the planted repository's theme, testdata/planted_repository/styles, the
// second design system written in cohere (#pg78xtp). Several engine assertions once ran on two consumer
// repositories, because a fact one design system shows may be a fact about that system rather than about
// the engine, and a single theme lets a per-repository token pass as a framework fact. The public theme
// is the first of the two; this file asks the same questions of the second, and holds the two apart.

// plantedThemeSystem is testdata/planted_repository/styles' design system, over the vendored tailwindcss.
func plantedThemeSystem(test *testing.T) *LoadedDesignSystem {
	test.Helper()
	entryPoint, absolutePathError := filepath.Abs(
		filepath.Join("testdata", "planted_repository", "styles", "theme.css"),
	)
	if absolutePathError != nil {
		test.Fatalf("resolving the planted theme: %v", absolutePathError)
	}
	system, loadDesignSystemError := LoadDesignSystem(
		LoadOptions{EntryPoint: entryPoint, TailwindPackageRoot: vendored.TailwindPackageRootForTest(test)},
	)
	if loadDesignSystemError != nil {
		test.Fatalf("loading the planted theme: %v", loadDesignSystemError)
	}
	return system
}

// TestARootDeclaredBothWaysKeepsBothKindsOnThePlantedTheme is the public theme's assertion on the second
// theme, with the orders the other way round from its names: `stack` is declared functional first and
// `cluster` static first. A map from root to one kind once let the second block overwrite the first.
func TestARootDeclaredBothWaysKeepsBothKindsOnThePlantedTheme(test *testing.T) {
	test.Parallel()
	system := plantedThemeSystem(test)
	for _, root := range []string{"stack", "cluster"} {
		staticForm := system.HasUtility(root, UtilityKindStatic)
		functionalForm := system.HasUtility(root, UtilityKindFunctional)
		if !staticForm || !functionalForm {
			test.Errorf(
				"`@utility %s` and `@utility %s-*` are both declared and the system reports static=%v functional=%v",
				root,
				root,
				staticForm,
				functionalForm,
			)
		}
	}
}

// TestTableCarriesTheThemeOnThePlantedTheme asserts the per-repository half of a table is present on the
// second theme (checkTableCarriesTheTheme). The planted theme declares five static roots, one of them with
// nested rules only and one with only a custom property, and five functional ones, two of them roots that
// end in a dash, so a composition that drops any of those shapes fails.
func TestTableCarriesTheThemeOnThePlantedTheme(test *testing.T) {
	test.Parallel()
	system := plantedThemeSystem(test)
	table := NewTable(system)
	checkTableCarriesTheTheme(test, "planted", system, table)
	for _, root := range []string{"card-frame", "scroll-quiet", "halo-on", "stack", "cluster"} {
		if _, found := table.Statics[root]; !found {
			test.Errorf("static `@utility %s` is missing from the table's statics", root)
		}
	}
	for _, root := range []string{"wash-", "line-", "stack", "cluster", "tint"} {
		if _, found := table.Descriptors[root]; !found {
			test.Errorf("functional `@utility %s-*` is missing from the table's descriptors", root)
		}
	}
}

// TestEachThemeAnswersOnlyItsOwnTokens holds the two themes apart: a class built on one theme's own keys
// or `@utility` blocks resolves against that theme and not against the other. This is the defect the
// per-repository half exists to remove. A design system that leaked between loads, or a table that
// carried one repository's tokens as framework facts, answers every class in both columns, and one
// theme alone cannot show it.
func TestEachThemeAnswersOnlyItsOwnTokens(test *testing.T) {
	test.Parallel()
	public := publicThemeSystem(test)
	planted := plantedThemeSystem(test)

	cases := []struct {
		className string
		owner     string
	}{
		{"bg-moss", "planted"},
		{"p-gutter", "planted"},
		{"rounded-pill", "planted"},
		{"shadow-float", "planted"},
		{"animate-wobble", "planted"},
		{"max-w-column", "planted"},
		{"wash--3", "planted"},
		{"tint-moss/40", "planted"},
		{"card-frame", "planted"},
		{"bg-brand", "public"},
		{"pane-wide", "public"},
		{"rail-thick", "public"},
		{"surface-card", "public"},
		{"tone--4", "public"},
	}

	for _, testCase := range cases {
		for _, side := range []struct {
			name   string
			system *LoadedDesignSystem
		}{{"public", public}, {"planted", planted}} {
			want := side.name == testCase.owner
			got := false
			if candidates := ParseCandidate(testCase.className, side.system); len(candidates) > 0 {
				got = ClassValueResolvesIn(&candidates[0], side.system)
			}
			if got != want {
				test.Errorf(
					"%s resolves=%v against the %s theme, want %v: it is built on the %s theme's own tokens",
					testCase.className,
					got,
					side.name,
					want,
					testCase.owner,
				)
			}
		}
	}
	test.Logf("%d classes, each resolving against its own theme alone", len(cases))
}
