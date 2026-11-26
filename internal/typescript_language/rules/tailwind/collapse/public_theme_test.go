package tailwind

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/typescript_language/rules/tailwind/vendored"
)

// Tailwind assertions that need a shape only a repository's own stylesheet has (#f598zk0, tier B), asked of
// testdata/public_theme, which declares each shape on purpose, so they run everywhere. They began as twins of
// originals that read a consumer's checkout, and replaced them when cohere's tests stopped reading other
// repositories (#5wj23dd).

// publicThemeSystem is testdata/public_theme's design system, over the vendored tailwindcss.
func publicThemeSystem(test *testing.T) *LoadedDesignSystem {
	test.Helper()
	entryPoint, absolutePathError := filepath.Abs(filepath.Join("testdata", "public_theme", "theme.css"))
	if absolutePathError != nil {
		test.Fatalf("resolving the public theme: %v", absolutePathError)
	}
	system, loadDesignSystemError := LoadDesignSystem(
		LoadOptions{EntryPoint: entryPoint, TailwindPackageRoot: vendored.TailwindPackageRootForTest(test)},
	)
	if loadDesignSystemError != nil {
		test.Fatalf("loading the public theme: %v", loadDesignSystemError)
	}
	return system
}

// TestARootDeclaredBothWaysKeepsBothKindsOnThePublicTheme: a root declared both as a static and as a functional
// `@utility` keeps both kinds, in both orders: `pane` is declared static first and `rail` functional first. A map
// from root to one kind once let the second block overwrite the first, so a consumer's `fade-in` was answerable
// by neither the parser nor the evaluator.
func TestARootDeclaredBothWaysKeepsBothKindsOnThePublicTheme(test *testing.T) {
	test.Parallel()
	system := publicThemeSystem(test)
	for _, root := range []string{"pane", "rail"} {
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

// TestTableCarriesTheThemeOnThePublicTheme asserts the per-repository half of a table is present
// (checkTableCarriesTheTheme). The public theme declares four static roots and two functional ones, so a
// composition that drops either kind fails.
func TestTableCarriesTheThemeOnThePublicTheme(test *testing.T) {
	test.Parallel()
	system := publicThemeSystem(test)
	table := NewTable(system)
	checkTableCarriesTheTheme(test, "public", system, table)
	for _, root := range []string{"surface-card", "glow-soft", "pane", "rail"} {
		if _, found := table.Statics[root]; !found {
			test.Errorf("static `@utility %s` is missing from the table's statics", root)
		}
	}
	for _, root := range []string{"pane", "rail"} {
		if _, found := table.Descriptors[root]; !found {
			test.Errorf("functional `@utility %s-*` is missing from the table's descriptors", root)
		}
	}
}
