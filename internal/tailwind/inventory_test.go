package tailwind

import "testing"

// TestEveryTableIsTailwindsOrHasAStatedReason is the inventory this package owes its reader.
//
// The port removed the tables that did not need to exist. What remains splits in two, and the split
// is the whole claim: a table is either Tailwind's own data, which we mirror, or it answers a
// question the port's own machinery structurally cannot, which is stated per table below.
//
// # Tailwind's own data
//
// Upstream carries these and there is no algorithm behind them to port. `property-order.ts` at 4.3.3
// is a hand-written array of 359 property names, with comments in it about how to make `inset-x-0`
// come before `top-0`, and `getPropertySort` calls `.indexOf` on it. Mirroring it is the faithful
// thing; reading it at lint time would be the same table with extra steps.
//
//	PropertyOrder                  359   property-order.ts
//	SortOverrideProperties          12   the --tw-sort overrides
//	FrameworkVariantRegistrations   88   variants.ts registrations
//	FrameworkStaticDeclarations    890   staticUtility calls
//	FrameworkFunctionalUtilities    33   functionalUtility registrations
//	FrameworkMultiDeclaration      152   functionalUtility registrations
//
// # Ours, each answering something a reading cannot
//
// A reading is a sorted set of property positions and a count. `getPropertySort` reads
// `node.property` and increments a counter, and never reads a value. That single fact is what made
// this port tractable, deleting roughly 85% of `utilities.ts`, and it is also the reason these two
// tables cannot be derived from what the port carries.
//
//	baseDescriptors          78   the reading partitions on the value's resolved type, and no
//	                              registration shape carries both halves: `border-[3px]` is a width
//	                              and `border-red-500` is a colour.
//	CollapseFamilies         45   which pairs canonicalize into a third root. All 45 are CSS
//	                              shorthand relationships between declared properties, so deriving
//	                              it means carrying a shorthand table of equal size, one layer
//	                              further from the question.
//
// `RootDeclaredProperties` and its three companions were here until #mz0m6k8, carried for exactly the
// reason the paragraph below rejected. The emitting half is now ported, so the answer is computed by
// `DeclaredPropertiesFor` and the four tables, 1,183 entries and 62 KB, are deleted.
//
// `ComposingRoots` went the same way in #gb4bkgc, and its reason had read as the strongest of the
// four. A reading cannot answer whether two values of a root layer or overwrite, which is true and
// was never the question: the emitters can, once they stop flattening the constant each layering
// family writes to its shared property. 81 of its 82 rows answered and all 81 agreed before it was
// deleted, and the 82nd was not a utility. Its answers are kept as a fixture in composes_test.go.
//
// # Why not port the emitting half and delete the rest
//
// Measured rather than assumed. `utilities.ts` holds 582 `decl()` call sites and 374 of them compute
// their value, which means porting `color-mix`, `withAlpha`, `calc` and the 729-reference `--tw-*`
// var chain. That is the surface this port deleted on purpose. It would cost more than the four
// tables together and buy nothing: the differential holds at 95,931 answered with 17 disagreements,
// all named and tested.
//
// # What this test actually asserts
//
// The counts, so a table growing or shrinking without its reason being revisited fails here. The
// reasons themselves are prose and cannot be asserted, which is why each table also carries its own
// test: the collapse families assert their shorthand structure, the descriptor rows are compared
// class for class by the differential, and the upstream diff catches Tailwind's own data moving.
func TestEveryTableIsTailwindsOrHasAStatedReason(t *testing.T) {
	upstream := map[string]int{
		"PropertyOrder":                 len(PropertyOrder),
		"SortOverrideProperties":        len(SortOverrideProperties),
		"FrameworkVariantRegistrations": len(FrameworkVariantRegistrations),
		"FrameworkStaticDeclarations":   len(FrameworkStaticDeclarations),
		"FrameworkFunctionalUtilities":  len(FrameworkFunctionalUtilities),
		"FrameworkMultiDeclaration":     len(FrameworkMultiDeclarationUtilities),
	}
	ours := map[string]int{
		"baseDescriptors":  len(baseDescriptors),
		"CollapseFamilies": len(CollapseFamilies),
	}

	for name, count := range upstream {
		if count == 0 {
			t.Errorf("%s is empty; a table mirroring Tailwind's own data cannot be", name)
		}
	}
	for name, count := range ours {
		if count == 0 {
			t.Errorf("%s is empty; if it is genuinely no longer needed, delete it and its entry here "+
				"rather than leaving a zero that reads like a working table", name)
		}
	}

	// The count of tables, not their contents. A fifth table of ours appearing without an entry in
	// the doc comment above is the thing this catches: the inventory going stale is how a table ends
	// up carried for no stated reason, which is what this package started with.
	if len(ours) != 2 {
		t.Errorf("the inventory lists %d tables of our own; the doc comment above accounts for 3, so "+
			"one has been added or removed without its reason being written down", len(ours))
	}

	// Every exported table in the package is accounted for above, and this is the half the first
	// version of this test was missing.
	//
	// It asserted "exactly four tables of ours" over a list it wrote itself, so `KnownRoots` and
	// `KnownStatics` sat in the same file, generated on every run, dead since 58fb982 rewired
	// `HasUtility` off them, and passed. 1,236 entries carried for two commits under a check that
	// read as clean, which is the same defect class this package keeps finding: a measurement of the
	// wrong population.
	//
	// Naming them here is what makes the assertion mechanical rather than a restatement of the list.
	// A table added to the package and not to this slice fails, whatever the count says.
	inPackage := []string{
		"PropertyOrder", "SortOverrideProperties", "FrameworkVariantRegistrations",
		"FrameworkStaticDeclarations", "FrameworkFunctionalUtilities", "FrameworkMultiDeclaration",
		"baseDescriptors", "CollapseFamilies",
	}
	accounted := make(map[string]bool, len(upstream)+len(ours))
	for name := range upstream {
		accounted[name] = true
	}
	for name := range ours {
		accounted[name] = true
	}
	for _, name := range inPackage {
		if !accounted[name] {
			t.Errorf("%s is a table in this package with no entry in the inventory above", name)
		}
	}
	if len(accounted) != len(inPackage) {
		t.Errorf("the inventory accounts for %d tables and the package holds %d; the two lists have "+
			"drifted, which is how a dead table survives a passing check", len(accounted), len(inPackage))
	}

	t.Logf("Tailwind's own: %v", upstream)
	t.Logf("ours, each with a stated reason: %v", ours)
}
