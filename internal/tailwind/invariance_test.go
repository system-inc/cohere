package tailwind

import (
	"os"
	"path/filepath"
	"testing"
)

// The two tables in this package that ship as framework facts, and the mechanism that lets them.
//
// `CollapseFamilies` and `FrameworkVariantRegistrations` are both generated once and linted against
// three repositories, so each is a claim that no repository can move it. Both claims were once
// verified by generating against ~/Projects/ahra and ~/Projects/connected/www-connected-app and
// diffing to zero, and that instrument could not support them: both vendor the Structure submodule
// and import its `global.css`, so they share its `@theme` and `@utility` blocks and are one
// observation wearing two names. Two design systems that cannot differ cannot detect a table that
// varies.
//
// Re-measured against `tools/gen_tailwind_descriptor_base/testdata/independent_theme.css`, which
// shares no submodule, both claims held: the same 44 families down to the value each was discovered
// at, over a registry differing by 25 functional roots, and the same 88 registrations across four
// design systems. The generators now carry `-verify-invariance-against` so that measurement is
// repeatable rather than a thing someone did once.
//
// What these tests add is the half a generator flag cannot cover. A flag only runs when an operator
// passes it; these run on every `go test`, and they pin the *mechanism* rather than the numbers, so
// a Tailwind release that changed how a repository interacts with the registry fails here even if
// nobody regenerates. The numbers stay in the generated headers, where regeneration updates them.

// TestRepositoryVariantsCannotMoveFrameworkRegistrations pins the mechanism the variant table rests
// on.
//
// The table is only safe to bake because a repository cannot move a framework registration. Upstream
// `Variants.set` assigns kind and applyFn onto an existing record and never touches `order`, so a
// repository redefining `dark` changes the selector it emits without changing where it sorts, and a
// `@custom-variant` under a new name appends instead of displacing. Measured on the engine, two new
// names appended at 83 and 84 and moved nothing.
//
// If a release ever made a redefinition renumber, every class carrying a variant would sort against
// a stale table and the differential would not catch it, because both sides would be reading the
// same wrong number. This test is the thing that would catch it.
func TestRepositoryVariantsCannotMoveFrameworkRegistrations(t *testing.T) {
	baseline, packageRoot := frameworkVariantStylesheet(t, `@import "tailwindcss";`)

	baselineSystem, err := LoadDesignSystem(LoadOptions{EntryPoint: baseline, TailwindPackageRoot: packageRoot})
	if err != nil {
		t.Fatalf("loading the baseline: %v", err)
	}

	// A repository that redefines a framework variant and adds two of its own, which is the shape
	// every corpus repository has: all three declare `@custom-variant dark`.
	redefining := filepath.Join(filepath.Dir(baseline), "redefining.css")
	if err := os.WriteFile(redefining, []byte(`@import "tailwindcss";
@custom-variant dark (&:where(.dark, .dark *));
@custom-variant totally-new-variant (&:hover);
@custom-variant another-new-one (&:focus);
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	redefiningSystem, err := LoadDesignSystem(LoadOptions{EntryPoint: redefining, TailwindPackageRoot: packageRoot})
	if err != nil {
		t.Fatalf("loading the redefining system: %v", err)
	}

	baselineOrders := make(map[string]int)
	for _, registration := range baselineSystem.Variants().Registrations() {
		baselineOrders[registration.Name] = registration.Order
	}

	added := make([]string, 0, 2)
	for _, registration := range redefiningSystem.Variants().Registrations() {
		baselineOrder, wasInBaseline := baselineOrders[registration.Name]
		if !wasInBaseline {
			added = append(added, registration.Name)
			continue
		}
		if registration.Order != baselineOrder {
			t.Errorf(
				"%q moved from order %d to %d when the repository redefined a variant; the baked table cannot survive that",
				registration.Name, baselineOrder, registration.Order,
			)
		}
	}

	// The control on the zero above. A test that only asserted "nothing moved" would pass just as
	// well against a loader that ignored the stylesheet entirely, which is precisely the failure
	// that put a false invariance claim into the generated header: the enumeration it cited never
	// read a repository stylesheet at all.
	if len(added) != 2 {
		t.Errorf("expected the two new @custom-variant names to register, got %v", added)
	}
	if !redefiningSystem.HasVariant("totally-new-variant") {
		t.Error("a repository's own @custom-variant did not reach the registry, so this test proves nothing about repositories")
	}
}

// TestCollapseFamiliesHoldForRootsTheFrameworkOwns pins what makes the collapse table a fact about
// the framework rather than about a design system.
//
// Every family names two roots and an output, and all three are framework roots in every case. A
// repository's `@utility` block can add roots, and measured on an independent design system it adds
// no families: three synthetic roots sharing a padding shorthand produced 44 families, the same 44.
// The reason is structural rather than lucky, and it is what this asserts: the outputs are roots the
// framework itself registers, so a table of them cannot pick up a repository token the way
// KnownStatics did.
func TestCollapseFamiliesHoldForRootsTheFrameworkOwns(t *testing.T) {
	if len(CollapseFamilies) == 0 {
		t.Fatal("no collapse families, which is a broken table rather than a Tailwind with no shorthands")
	}

	// A negative-prefixed root is registered under its bare name, so `-mx` is `mx` in the registry.
	registryName := func(root string) string {
		if len(root) > 0 && root[0] == '-' {
			return root[1:]
		}
		return root
	}

	for _, family := range CollapseFamilies {
		for _, root := range []string{family.First, family.Second, family.Output} {
			if !KnownRoots[registryName(root)] {
				t.Errorf(
					"family %s + %s => %s names %q, which is not a root the design system registers; a family over an unregistered root is a repository token in a framework table",
					family.First, family.Second, family.Output, root,
				)
			}
		}
	}
}

// TestKnownStaticsCarriesRepositoryTokens is the counterexample that keeps the file honest.
//
// `collapse_table.go` says "Source: Tailwind 4.3.3" at the top and `KnownStatics` holds
// `markdown-content`, one of ahra's own `@utility` blocks. That is already known and tracked as
// replace-with-live-system, and it is asserted here rather than left as prose because the two
// tables sit in one file under one header: a reader who checked `CollapseFamilies`, found it
// genuinely invariant, and generalised to the file would be wrong.
//
// If this test ever fails because the token is gone, the fix is to delete the test along with the
// header caveat, not to loosen it.
func TestKnownStaticsCarriesRepositoryTokens(t *testing.T) {
	repositoryToken := "markdown-content"

	if KnownStatics[repositoryToken] {
		return
	}

	t.Errorf(
		"%q is no longer in KnownStatics; the file's caveat about carrying repository tokens may now be stale rather than the table being fixed",
		repositoryToken,
	)
}
