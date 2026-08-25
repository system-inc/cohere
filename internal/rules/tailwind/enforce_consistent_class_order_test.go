package tailwind

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// Expectations measured by diffing the comparator against the engine's own sort over the whole
// corpus, not by reading upstream: upstream delegates the entire question to `getClassOrder` in
// three lines, so its source says nothing about how the order is built.

func TestEnforceConsistentClassOrderReportsMisordering(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
		wantIds  []string
	}{
		{
			name:     "two classes reversed",
			fileName: "Component.tsx",
			source:   `const element = <div className="items-center flex" />;`,
			wantIds:  []string{"inconsistentClassOrder"},
		},
		{
			// A variant class before its unprefixed counterpart. The engine groups by variant first,
			// so every bare class precedes every prefixed one.
			name:     "variant before base",
			fileName: "Component.tsx",
			source:   `const element = <div className="hover:px-8 px-4" />;`,
			wantIds:  []string{"inconsistentClassOrder"},
		},
		{
			// `group` is unranked and sorts first, so writing it last is the violation. The reverse
			// of this pair was asserted until the ecosystem convention was checked: Tailwind's
			// Prettier plugin and `better-tailwindcss` both hoist the markers.
			name:     "unranked class written last",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex group" />;`,
			wantIds:  []string{"inconsistentClassOrder"},
		},
		{
			// Two unranked classes keep the order they were written in. Alphabetical would rewrite
			// this one, and `better-tailwindcss` accepts it as written.
			name:     "two unranked classes in non-alphabetical source order",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex peer group" />;`,
			wantIds:  []string{"inconsistentClassOrder"},
		},
		{
			name:     "several classes out of order",
			fileName: "Component.tsx",
			source:   `const element = <div className="gap-2 items-center flex" />;`,
			wantIds:  []string{"inconsistentClassOrder"},
		},
		{
			name:     "on a callee surface",
			fileName: "Component.tsx",
			source:   `const merged = mergeClassNames('items-center flex');`,
			wantIds:  []string{"inconsistentClassOrder"},
		},
		{
			name:     "on a variable surface",
			fileName: "Styles.ts",
			source:   `const buttonClassName = 'items-center flex';`,
			wantIds:  []string{"inconsistentClassOrder"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, EnforceConsistentClassOrder, testCase.fileName, testCase.source)
			ruletest.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// The silent half. Several of these are shapes an earlier version of the comparator got wrong, and
// each one is a case where reporting would be reporting correct code.
func TestEnforceConsistentClassOrderStaysSilent(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
	}{
		{
			name:     "already ordered",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex items-center gap-2" />;`,
		},
		{
			name:     "base before variant",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-4 hover:px-8" />;`,
		},
		{
			// Variants group together, and within a group the classes order among themselves. An
			// earlier comparator interleaved them with the bare classes and agreed with the engine on
			// only 51% of the corpus.
			name:     "variant group kept together",
			fileName: "Component.tsx",
			source:   `const element = <div className="pointer-events-none hidden md:absolute md:inset-x-0 md:flex md:w-full" />;`,
		},
		{
			name:     "unranked class first",
			fileName: "Component.tsx",
			source:   `const element = <div className="group flex" />;`,
		},
		{
			// A root whose own prefix is also a root. `ring-offset` exists in the ordering table
			// and not in the conflict table, so resolving it through the latter answered with
			// `ring`'s properties and made `ring-offset-1` indistinguishable from `ring-1`. The
			// engine accepts this literal as written; the rule reported it until the lookup was
			// pointed at the table it reads.
			name:     "ring-offset resolves to its own root, not to ring",
			fileName: "Component.tsx",
			source: `const element = <div className="focus-visible:ring-1 focus-visible:ring-(--color-content-informative) ` +
				`focus-visible:ring-offset-1 focus-visible:outline-none" />;`,
		},
		{
			// Both spellings are accepted, which is what pins the tiebreak to source order rather
			// than to any ordering of our own. An alphabetical tiebreak passes the first and fails
			// the second, so the pair is the discriminator.
			name:     "two unranked classes, alphabetical source order",
			fileName: "Component.tsx",
			source:   `const element = <div className="group peer flex" />;`,
		},
		{
			name:     "two unranked classes, reverse-alphabetical source order",
			fileName: "Component.tsx",
			source:   `const element = <div className="peer group flex" />;`,
		},
		{
			// `font-mono` and `text-[10px]` share no root and their relative order is not guessable
			// from their names; it comes from the table.
			name:     "ordering the names do not suggest",
			fileName: "Component.tsx",
			source:   `const element = <div className="truncate font-mono text-[10px]" />;`,
		},
		{
			name:     "single class",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex" />;`,
		},
		{
			// A duplicate is `no-duplicate-classes`'s finding. Reporting an ordering defect here
			// would send the author to fix the wrong thing.
			name:     "duplicate class present",
			fileName: "Component.tsx",
			source:   `const element = <div className="items-center flex flex" />;`,
		},
		{
			// A class outside the design system cannot be placed, so the literal is left alone and
			// `no-unknown-classes` is the rule with something to say about it.
			name:     "unknown class present",
			fileName: "Component.tsx",
			source:   `const element = <div className="items-center flex ahralia-splash" />;`,
		},
		{
			name:     "unrelated attribute",
			fileName: "Component.tsx",
			source:   `const element = <div title="items-center flex" />;`,
		},
		{
			name:     "unrelated callee",
			fileName: "Component.tsx",
			source:   `const value = someOtherFunction('items-center flex');`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, EnforceConsistentClassOrder, testCase.fileName, testCase.source)
			ruletest.ExpectClean(t, result)
		})
	}
}

// TestClassOrderMessageNamesTheOrder checks the part an author acts on.
//
// The order is not guessable from the class names, which is the whole reason the rule needs a table.
// A finding that says only "these are misordered" leaves the reader to run the formatter and diff
// the result.
func TestClassOrderMessageNamesTheOrder(t *testing.T) {
	result := ruletest.Run(t, EnforceConsistentClassOrder, "Component.tsx",
		`const element = <div className="gap-2 items-center flex" />;`)

	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected one finding, got %d", len(result.Diagnostics))
	}
	description := result.Diagnostics[0].Message.Description
	if !strings.Contains(description, "\"flex items-center gap-2\"") {
		t.Errorf("message does not name the correct order: %s", description)
	}
}

// TestClassOrderProposesNoFix guards a deliberate absence, and this one is a closer call than the
// other rules' fixes.
//
// The rule knows the correct order, so a fix is mechanically available. It is left out because real
// class lists in this codebase wrap across lines with indentation, and a rewrite would reflow them:
// the diff of the repair would be larger than the defect. Reordering is the one finding here where
// the noise of fixing can exceed the cost of the problem.
func TestClassOrderProposesNoFix(t *testing.T) {
	result := ruletest.Run(t, EnforceConsistentClassOrder, "Component.tsx",
		`const element = <div className="items-center flex" />;`)

	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected one finding, got %d", len(result.Diagnostics))
	}
	if len(result.Diagnostics[0].Fixes) != 0 || len(result.Diagnostics[0].Suggestions) != 0 {
		t.Fatal("this rule reports the order and leaves the rewrite alone, because reflowing a wrapped " +
			"class list produces a diff larger than the defect")
	}
}

// TestComparatorDimensionsAreAllUsed is the known-dirty control.
//
// Three simpler comparators each looked right and each was measured wrong against the engine:
// ordering by root alone agreed on 51% of the corpus, adding variants took it to 58% while
// representatives still carried their own prefixes, and unprefixing them took it to 91%. The last
// 9% was unranked classes being stored with a position instead of being separated out entirely.
//
// So each dimension is exercised by a pair that only it can order correctly.
func TestComparatorDimensionsAreAllUsed(t *testing.T) {
	testCases := []struct {
		name      string
		first     string
		second    string
		dimension string
	}{
		{
			name:      "class position",
			first:     "flex",
			second:    "items-center",
			dimension: "the per-class table; their roots share no ordering that names would suggest",
		},
		{
			name:      "variant grouping",
			first:     "px-4",
			second:    "hover:px-8",
			dimension: "variant position; without it the two interleave by class position alone",
		},
		{
			name:      "unranked first",
			first:     "group",
			second:    "flex",
			dimension: "the unranked check; `group` has no position and must not sort by one",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if !classSortsBefore(testCase.first, testCase.second) {
				t.Errorf("%q should sort before %q, decided by %s",
					testCase.first, testCase.second, testCase.dimension)
			}
			if classSortsBefore(testCase.second, testCase.first) {
				t.Errorf("the comparator is not antisymmetric for %q and %q, so a sort using it is "+
					"not well defined", testCase.first, testCase.second)
			}
		})
	}
}

// TestVariantComparisonDimensions pins the four things that decide variant order, each of which was
// a real divergence against the engine before it was handled.
//
// The corpus differential is the measurement behind every one: 2,761 real literals and 2,385
// shuffled ones, and each of these cost between one and four of them.
func TestVariantComparisonDimensions(t *testing.T) {
	testCases := []struct {
		name   string
		before string
		after  string
		why    string
	}{
		{
			name:   "compound resolves on its inner segment",
			before: "dark:placeholder:",
			after:  "dark:focus:",
			why:    "the table holds single variants only, so a compound has no position of its own and both tie",
		},
		{
			// Compared against a DIFFERENT unnamed variant, not against a compound of itself. Pairing
			// it with `dark:group-hover/csv-download:` passes on depth alone, so the mutant that
			// stops stripping the name survives. `group-focus:` is position 30 and `group-hover:` is
			// 29, so this pair can only order correctly if the name is stripped first.
			name:   "named group sorts where its unnamed form does",
			before: "group-hover/csv-download:",
			after:  "group-focus:",
			why:    "an unstripped name falls to the unknown position and sorts after every ranked variant",
		},
		{
			name:   "unknown variants order by their own text",
			before: "data-[show=false]:",
			after:  "data-[show=true]:",
			why:    "two unrankable variants tie on position, and the engine still groups each one's classes",
		},
		{
			// The pair that only depth resolves. Both start with `group-hover:`, so a comparison that
			// walks segments first finds them equal on segment 0 and then compares `disabled:`
			// against nothing, which is the case depth exists for.
			name:   "depth before segments",
			before: "group-hover:",
			after:  "group-hover:disabled:",
			why:    "a stacked variant narrows an already-narrowed selector and lands in a later layer",
		},
		{
			// And depth must beat a lower first segment: `disabled:` is position 118 and
			// `group-hover:` is 29, so segment-first ordering would put the compound first.
			name:   "depth outranks a lower first segment",
			before: "disabled:",
			after:  "group-hover:disabled:",
			why:    "every single variant precedes every stacked one, whatever it starts with",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if compareVariants(testCase.before, testCase.after) >= 0 {
				t.Errorf("%q should sort before %q: %s", testCase.before, testCase.after, testCase.why)
			}
			if compareVariants(testCase.after, testCase.before) <= 0 {
				t.Errorf("the comparison is not antisymmetric for %q and %q, so a sort using it is not "+
					"well defined", testCase.before, testCase.after)
			}
		})
	}
}

// TestOrderingPropertiesKeepCustomProperties pins the distinction between the two property tables.
//
// The conflict tables strip `--tw-*`, because two classes both setting `--tw-border-style` are not
// in conflict about anything an author sees. Ordering indexes them, and they are what separates
// classes sharing a visible property: `shadow-lg` emits `--tw-shadow` then `box-shadow` while
// `ring-1` emits `--tw-ring-shadow` then `box-shadow`. Reading the conflict tables here left both
// with the key `[box-shadow]` and ten real class lists came out wrong.
func TestOrderingPropertiesKeepCustomProperties(t *testing.T) {
	shadow := declaredPropertiesForOrdering("shadow-lg")
	ring := declaredPropertiesForOrdering("ring-1")

	if len(shadow) < 2 || len(ring) < 2 {
		t.Fatalf("expected both to declare a custom property and box-shadow, got %v and %v", shadow, ring)
	}
	if shadow[0] == ring[0] {
		t.Errorf("shadow-lg and ring-1 lead with the same property %q, so they cannot be separated",
			shadow[0])
	}
	if !strings.HasPrefix(shadow[0], "--") || !strings.HasPrefix(ring[0], "--") {
		t.Errorf("the ordering tables should keep custom properties, got %v and %v", shadow, ring)
	}
}

// TestUnrankedClassesSortFirstRegardlessOfVariant covers the interaction that produced the final
// three corpus divergences.
//
// `group` is unranked and a variant class has a position, so a comparator checking variants before
// rankedness puts `group` in the middle of the list. It belongs at the front, ahead of both.
func TestUnrankedClassesSortFirstRegardlessOfVariant(t *testing.T) {
	if !classSortsBefore("group", "hover:background--5") {
		t.Error("an unranked class must sort before a variant-prefixed ranked one, which means the " +
			"rankedness check has to come before the variant check")
	}
	if classSortsBefore("hover:background--5", "group") {
		t.Error("the comparator disagrees with itself on the same pair")
	}
}

// TestOrderingRootsResolveInTheOrderingTable is the known-dirty control for the table-choice bug.
//
// 22 roots exist in the ordering table and not in the conflict table, so a lookup that resolves an
// ordering question through `functionalRootOf` silently answers with a shorter root's properties.
// Two classes the engine separates then collapse onto one key, which reads as agreement rather
// than as a miss.
//
// The assertion is that a shadowed root answers with its own properties and not with its prefix's.
// Both halves are needed: without the second, a lookup returning the prefix's row passes.
func TestOrderingRootsResolveInTheOrderingTable(t *testing.T) {
	if root := orderingRootOf("ring-offset-1"); root != "ring-offset" {
		t.Errorf("ring-offset-1 should resolve to root %q, got %q", "ring-offset", root)
	}

	offset := declaredPropertiesForOrdering("ring-offset-1")
	ring := declaredPropertiesForOrdering("ring-1")
	if len(offset) == 0 || len(ring) == 0 {
		t.Fatalf("both should declare properties, got %v and %v", offset, ring)
	}
	if offset[0] == ring[0] {
		t.Errorf("ring-offset-1 and ring-1 lead with the same property %q, so the comparator cannot "+
			"separate them and the engine does", offset[0])
	}
}

// TestUnrankedClassesKeepSourceOrder pins the tiebreak.
//
// The comparator must express no preference between two unranked classes, in either direction, so
// that a stable sort leaves them as written. An alphabetical tiebreak satisfies the first assertion
// and fails the second, which is exactly the bug a blind reversal of the rankedness check leaves
// behind: `better-tailwindcss` accepts both `peer group` and `group peer`.
func TestUnrankedClassesKeepSourceOrder(t *testing.T) {
	if classSortsBefore("group", "peer") {
		t.Error("the comparator must not order two unranked classes; alphabetical would rewrite " +
			"`peer group`, which the reference accepts as written")
	}
	if classSortsBefore("peer", "group") {
		t.Error("the comparator must not order two unranked classes in either direction")
	}
}
