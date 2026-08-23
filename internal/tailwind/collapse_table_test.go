package tailwind

import (
	"testing"
)

// The table replaces a search, so the tests have to guard what the search would have gotten right.
//
// `enforce-canonical-classes` was rejected as a Go port twice, because `canonicalizeCandidates` is a
// signature-equivalence search over the whole utility registry and porting it means porting the
// theme resolver, the compiler and the printer. The table is the search's output rather than its
// mechanism, which is only safe while three things hold: it contains the families that exist, it
// contains nothing that does not, and it was built from the registry rather than from whatever
// classes this codebase happens to use.

// TestTableContainsTheKnownFamilies pins the families the migration corpus earned.
//
// Each of these was a real finding at some point, several of them the specific finding a cheaper
// approach silently lost. A regeneration that drops one is a regression in what the rule can see,
// and without this test it would land as a quiet diff in a generated file.
func TestTableContainsTheKnownFamilies(t *testing.T) {
	required := []CollapseFamily{
		// The canonical example, and the one that proves collapsing is on at all.
		{First: "px", Second: "py", Output: "p"},
		// Two roots declaring different CSS properties. Filtering pairs by declared property family
		// measured fifteen times faster and silently stopped reporting this one.
		{First: "h", Second: "w", Output: "size"},
		// `column-gap` and `row-gap` merging into `gap`, the other half of that same lesson.
		{First: "gap-x", Second: "gap-y", Output: "gap"},
		// Roots with no value at all. A hand-written dash-splitter read `border-l` as root `border`
		// with value `l`, bucketed it away from `border-r`, and stopped reporting this.
		{First: "border-l", Second: "border-r", Output: "border-x"},
		// Negative roots are their own roots, not a sign flag on a positive one.
		{First: "-mb", Second: "-mt", Output: "-my"},
		// A family this codebase has never written, which is the point of enumerating from the
		// registry rather than from the corpus. A corpus-derived table missed every scroll family.
		{First: "scroll-px", Second: "scroll-py", Output: "scroll-p"},
		// Recursion: `mx + my => m` only completes a four-class collapse when `mb + mt => my` and
		// `ml + mr => mx` are also present, so all three are required together.
		{First: "mb", Second: "mt", Output: "my"},
		{First: "ml", Second: "mr", Output: "mx"},
		{First: "mx", Second: "my", Output: "m"},
	}

	for _, want := range required {
		if !tableContains(want) {
			t.Errorf("the table is missing %s + %s => %s, so the rule can no longer report it",
				want.First, want.Second, want.Output)
		}
	}
}

// TestTableHasNoSoloRewrites is the known-dirty control, and it caught a real defect.
//
// Some classes are non-canonical on their own: `start-4` is `inset-s-4`, `z-[1]` is `z-1`. Those
// rewrites have nothing to do with pairing. A first version of the generator counted invented class
// names rather than testing whether both inputs were load-bearing, and reported 1,326 families
// including `-bg-conic + -end => -inset-e`, which is not a family at all: `-end-4` rewrites alone
// and `-bg-conic-4` was an unrelated neighbour.
//
// The signature of that defect is a table far larger than the real one, full of entries whose two
// roots share no axis. This asserts the size, because 1,326 and 38 are not close enough to be
// confused by anyone reading the number, and a future generator change that reintroduces the bug
// will fail here rather than ship.
func TestTableHasNoSoloRewrites(t *testing.T) {
	if len(CollapseFamilies) == 0 {
		t.Fatal("the table is empty, so every test in this file passes for the wrong reason")
	}

	// The real table is a few dozen entries. An order of magnitude more means the pair test has
	// stopped requiring both inputs to matter.
	if len(CollapseFamilies) > 200 {
		t.Fatalf("the table holds %d families, which is far more than the real count. That is the "+
			"signature of counting invented class names instead of testing that both inputs are "+
			"load-bearing, which produced 1,326 entries including `-bg-conic + -end`.",
			len(CollapseFamilies))
	}

	// A family's two inputs must be distinct roots that are not each other's output. The stronger
	// property, that the pair genuinely shares an axis, is not checkable by name: `h + w => size`,
	// `bottom + top => inset-y` and `mb + mt => my` all share no common substring and are all real.
	// A first version of this test tried a stem heuristic and reported eleven of the thirty-eight
	// real families as defects, which is worse than no check.
	//
	// What actually separates the real table from the broken one is size and provenance, both
	// asserted above and below. The 1,326-entry version paired roots that never share an axis, and
	// it was an order of magnitude too large rather than subtly wrong.
	for _, entry := range CollapseFamilies {
		if entry.First == entry.Second {
			t.Errorf("%+v pairs a root with itself", entry)
		}
	}
}

// TestTableEntriesAreWellFormed guards the shape rather than the content.
func TestTableEntriesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}

	for _, entry := range CollapseFamilies {
		if entry.First == "" || entry.Second == "" || entry.Output == "" {
			t.Errorf("incomplete entry %+v", entry)
			continue
		}
		if entry.First == entry.Second {
			t.Errorf("%+v pairs a root with itself", entry)
		}
		if entry.Output == entry.First || entry.Output == entry.Second {
			t.Errorf("%+v collapses into one of its own inputs, which is a rewrite rather than a merge", entry)
		}

		key := entry.First + " " + entry.Second
		if seen[key] {
			t.Errorf("%s appears twice, so the table would answer the same question twice", key)
		}
		seen[key] = true
	}

	if TailwindVersion == "" {
		t.Fatal("the table records no Tailwind version, so a reader cannot tell which engine's opinion it holds")
	}
}

// tableContains reports whether the table holds a family, in either input order.
//
// Order-insensitive because the generator sorts its inputs and a caller asking about a pair should
// not have to know which way round they were enumerated.
func tableContains(want CollapseFamily) bool {
	for _, entry := range CollapseFamilies {
		if entry.Output != want.Output {
			continue
		}
		if entry.First == want.First && entry.Second == want.Second {
			return true
		}
		if entry.First == want.Second && entry.Second == want.First {
			return true
		}
	}
	return false
}

// TestDeclaredPropertiesCoverBothReadings guards a bug that was silent and shipped-adjacent.
//
// A utility name can be both static and functional. `flex` is the static utility `display: flex`
// and also the functional root of `flex-4`, which is `flex: 4`; `block` is `display: block` and the
// root of `block-4`, which is `block-size`. A first version of the generator classified each name
// as one or the other, checked functional first and returned early, so `flex` and `block` were
// recorded with their functional properties and disappeared from the static table entirely.
//
// Nothing failed. The table built, contained 250 root entries and 273 static ones, and looked
// complete. What it would have done is silently stop `flex block` being reported as a display
// conflict, which is the headline case of the rule it feeds.
//
// So this asserts the both-readings names specifically, and that the display utilities agree with
// each other, because a table where `flex` and `block` disagree about their property cannot report
// the conflict between them however correct each entry looks alone.
func TestDeclaredPropertiesCoverBothReadings(t *testing.T) {
	if len(StaticDeclaredProperties) == 0 || len(RootDeclaredProperties) == 0 {
		t.Fatal("a property table is empty, so every assertion below passes for the wrong reason")
	}

	// Names that are genuinely both. Their static reading is what `no-conflicting-classes` needs.
	bothReadings := []string{"flex", "block"}
	for _, name := range bothReadings {
		staticProperties, hasStatic := StaticDeclaredProperties[name]
		if !hasStatic {
			t.Errorf("%q is missing from the static table. It is both a static utility and a "+
				"functional root, and a generator that classifies rather than records loses whichever "+
				"reading it checks second.", name)
			continue
		}
		if !containsProperty(staticProperties, "display") {
			t.Errorf("%q declares %v as a static utility, want display", name, staticProperties)
		}

		// The functional reading should also be present, and should NOT be display.
		if functionalProperties, hasFunctional := RootDeclaredProperties[name]; hasFunctional {
			if containsProperty(functionalProperties, "display") {
				t.Errorf("%q as a functional root declares display, which means the static reading "+
					"overwrote the functional one rather than both being recorded", name)
			}
		}
	}

	// Every display utility must agree, or a conflict between two of them cannot be detected.
	displayUtilities := []string{"flex", "block", "hidden", "grid", "inline"}
	for _, name := range displayUtilities {
		properties, isPresent := StaticDeclaredProperties[name]
		if !isPresent {
			t.Errorf("%q is missing from the static table, so a conflict involving it is invisible", name)
			continue
		}
		if !containsProperty(properties, "display") {
			t.Errorf("%q declares %v, want display. Two display utilities that disagree about their "+
				"property cannot be reported as conflicting.", name, properties)
		}
	}

	// The property-name-not-value distinction, which inverts two answers if it is lost.
	width, hasWidth := RootDeclaredProperties["w"]
	height, hasHeight := RootDeclaredProperties["h"]
	if !hasWidth || !hasHeight {
		t.Fatal("w or h is missing from the root table")
	}
	if containsProperty(width, "height") || containsProperty(height, "width") {
		t.Error("w and h share a property, so `w-8 h-8` would be reported as a conflict when it is a " +
			"collapse into `size-8`")
	}

	// And the shorthand distinction upstream deliberately does not normalise.
	padding, hasPadding := RootDeclaredProperties["p"]
	paddingInline, hasPaddingInline := RootDeclaredProperties["px"]
	if !hasPadding || !hasPaddingInline {
		t.Fatal("p or px is missing from the root table")
	}
	for _, property := range padding {
		if containsProperty(paddingInline, property) {
			t.Errorf("p and px share the property %q, so `p-4 px-8` would be reported as a conflict. "+
				"Upstream reports no conflict there: `padding` and `padding-inline` are different "+
				"property names even though they visually overlap.", property)
		}
	}
}

func containsProperty(properties []string, want string) bool {
	for _, property := range properties {
		if property == want {
			return true
		}
	}
	return false
}

// TestPropertiesExcludeAtRuleDescriptors guards a leak that produced false conflicts.
//
// `border-l-4` compiles to its own two declarations followed by an `@property --tw-border-style`
// block, whose descriptors are `syntax`, `inherits` and `initial-value`. A first version of the
// generator scanned the whole compiled output, so every class touching border style appeared to
// declare those three, and any two of them read as conflicting on three shared properties.
//
// The tell was that `border-l-4` and `border-r-4` shared properties, which they must not: they are
// different edges. Caught by the rule's own known-dirty control rather than by reading the table,
// which is the order this should happen in.
func TestPropertiesExcludeAtRuleDescriptors(t *testing.T) {
	descriptors := []string{"syntax", "inherits", "initial-value"}

	for _, table := range []struct {
		name    string
		entries map[string][]string
	}{
		{name: "root", entries: RootDeclaredProperties},
		{name: "static", entries: StaticDeclaredProperties},
	} {
		for utility, properties := range table.entries {
			for _, descriptor := range descriptors {
				if containsProperty(properties, descriptor) {
					t.Errorf("%s table: %q declares %q, which is an @property descriptor rather than a "+
						"CSS property. Every utility carrying it would falsely conflict with every other one.",
						table.name, utility, descriptor)
				}
			}
		}
	}

	// The specific pair that exposed it.
	left, hasLeft := RootDeclaredProperties["border-l"]
	right, hasRight := RootDeclaredProperties["border-r"]
	if !hasLeft || !hasRight {
		t.Fatal("border-l or border-r is missing from the root table")
	}
	for _, property := range left {
		if containsProperty(right, property) {
			t.Errorf("border-l and border-r share %q, so opposite edges would be reported as "+
				"conflicting", property)
		}
	}
}
