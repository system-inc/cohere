package tailwind

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tailwindcollapse "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
	"github.com/system-inc/cohere/internal/lint/rules/tailwind/vendored"
)

// The acceptance suite for the live class-order sort, measured against Tailwind's own answers.
//
// Every helper here is prefixed `classOrderLive` for the reason design_system_test.go states: several
// agents work in this package at once, and a helper whose name does not say which component owns it
// is a collision waiting to happen.
//
// # What this measures, and the trap it is built to avoid
//
// The regression check on this task was "15 disagreeing pairs of 4,265 must go to zero." That number
// is satisfiable two ways: by the rule becoming correct, or by the rule declining to have an opinion,
// and a differential scores both as agreement because both sides go silent. A previous agent refused
// to build against this task for exactly that reason, and was right to.
//
// So this suite reports a denominator alongside every rate. `classOrderLiveMeasure` counts the classes
// the rule actually placed, and the tests below assert that count against a floor as well as asserting
// the disagreement count against a ceiling. A run that placed nothing scores zero disagreements over
// zero pairs and fails on the floor rather than passing on the empty diff.
//
// # The ground truth
//
// `internal/lint/rules/tailwind/collapse/testdata/variant_fixtures.json` holds synthetic cases built to
// separate rules that agree on ordinary input, each class list captured from
// `designSystem.getClassOrder(classes)`. `sorted` is what Tailwind returned. Nothing here asserts
// against what the Go code produces; the fixture is the authority and the Go side is the thing under
// test. Each case carries its own stylesheet, so the suite runs on every machine. The class lists two
// consumer repositories wrote left the fixture when cohere's tests stopped reading other repositories
// (#5wj23dd).

// classOrderLiveCorpus is the slice of variant_fixtures.json this suite reads.
type classOrderLiveCorpus struct {
	TailwindVersion string `json:"tailwindVersion"`
	Cases           []struct {
		Name       string `json:"name"`
		Source     string `json:"source"`
		EntryPath  string `json:"entryPath"`
		Input      string `json:"input"`
		ClassOrder *struct {
			Sorted []string `json:"sorted"`
		} `json:"classOrder"`
		ClassLists []struct {
			Name       string `json:"name"`
			ClassOrder struct {
				Sorted []string `json:"sorted"`
			} `json:"classOrder"`
		} `json:"classLists"`
	} `json:"cases"`
}

// classOrderLiveList is one Tailwind-sorted class list, with the design system it was captured against:
// a repository's stylesheet for a corpus case, or the stylesheet a synthetic case carries as text.
type classOrderLiveList struct {
	name      string
	entryPath string
	input     string
	sorted    []string
}

// systemKey names the design system a list was sorted against, so the measurement finds it whichever
// way it was built.
func (list classOrderLiveList) systemKey() string {
	if list.entryPath != "" {
		return list.entryPath
	}
	return "input:" + list.input
}

// classOrderLiveLoadCorpus reads the fixture and flattens it to the lists this suite sorts.
//
// Both shapes are collected: the synthetic `classOrder` on a case, and the `classLists` array a
// repository case carries. They answer different questions and dropping either would leave a gap the
// count could not show, since the synthetic cases are the only ones built to separate the depth-first
// rule from the mask and the repository lists are the only ones that prove it on code people wrote.
func classOrderLiveLoadCorpus(test *testing.T) []classOrderLiveList {
	test.Helper()

	path := filepath.Join("collapse", "testdata", "variant_fixtures.json")
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatalf("read %s: %v", path, readError)
	}

	var fixture classOrderLiveCorpus
	if decodeError := json.Unmarshal(contents, &fixture); decodeError != nil {
		test.Fatalf("parse %s: %v", path, decodeError)
	}
	if fixture.TailwindVersion != "4.3.3" {
		test.Fatalf("the fixture was captured from Tailwind %s and this port targets 4.3.3",
			fixture.TailwindVersion)
	}

	var lists []classOrderLiveList
	for _, aCase := range fixture.Cases {
		if aCase.ClassOrder != nil && len(aCase.ClassOrder.Sorted) > 1 {
			lists = append(lists, classOrderLiveList{
				name:      aCase.Name,
				entryPath: aCase.EntryPath,
				input:     aCase.Input,
				sorted:    aCase.ClassOrder.Sorted,
			})
		}
		for _, classList := range aCase.ClassLists {
			if len(classList.ClassOrder.Sorted) < 2 {
				continue
			}
			lists = append(lists, classOrderLiveList{
				name:      classList.Name,
				entryPath: aCase.EntryPath,
				input:     aCase.Input,
				sorted:    classList.ClassOrder.Sorted,
			})
		}
	}

	if len(lists) == 0 {
		test.Fatal("the fixture yielded no multi-class lists, so every assertion below would pass vacuously")
	}
	return lists
}

// classOrderLiveMeasurement is what one pass over the corpus found.
//
// Placed and skipped are carried beside the disagreement counts for the reason in the file comment: a
// disagreement count means nothing without the population it was measured over, and the failure this
// task is guarded against is a zero produced by an empty population.
type classOrderLiveMeasurement struct {
	// lists is how many class lists the rule produced an order for.
	lists int
	// listsSkipped is how many it declined, because a class in them could not be placed.
	listsSkipped int
	// placedClasses is every class the rule assigned a key to. The denominator that matters.
	placedClasses int
	// comparedPairs is every ordered pair within a sorted list, which is what a disagreement is
	// counted against.
	comparedPairs int
	// disagreeingPairs is the pairs the rule ordered the other way from Tailwind.
	disagreeingPairs int
	// disagreeingLists is how many lists held at least one such pair.
	disagreeingLists int
	// examples holds up to eight readable disagreements.
	examples []string
}

// classOrderLiveMeasure sorts every corpus list with the live rule and scores it against Tailwind.
//
// The list is shuffled into reverse before sorting rather than being handed to the rule already
// correct. A sort fed its own output is a sort that only has to be stable to pass, which would let a
// comparator that expresses no opinion at all score a hundred percent.
func classOrderLiveMeasure(
	test *testing.T,
	lists []classOrderLiveList,
	systems map[string]DesignSystemResult,
) classOrderLiveMeasurement {
	test.Helper()

	measurement := classOrderLiveMeasurement{}
	for _, list := range lists {
		designSystem, hasSystem := systems[list.systemKey()]
		if !hasSystem || designSystem.Error != nil {
			continue
		}

		unranked, placeable := partitionUnranked(classOrderLiveReversed(list.sorted), designSystem)
		keys, _, resolved := classOrderKeys(placeable, designSystem.System, designSystem.Table)
		if !resolved {
			measurement.listsSkipped++
			continue
		}

		measurement.lists++
		measurement.placedClasses += len(placeable)

		ordered := append(unranked, sortClassesByKey(placeable, keys)...)
		position := make(map[string]int, len(ordered))
		for index, className := range ordered {
			position[className] = index
		}
		// Two nulls keep source order, and the source here is the reversal, so Tailwind's order
		// between them is no answer at all. Every other pair, a null against a ranked class
		// included, is scored.
		isUnranked := make(map[string]bool, len(unranked))
		for _, className := range unranked {
			isUnranked[className] = true
		}

		// Scored pairwise against Tailwind's own sequence rather than by string equality, so a
		// single misplaced class is one disagreement rather than a whole list, and the number stays
		// comparable to the 15-of-4,265 baseline, which was also counted in pairs.
		listDisagrees := false
		for left := 0; left < len(list.sorted); left++ {
			for right := left + 1; right < len(list.sorted); right++ {
				leftClass, rightClass := list.sorted[left], list.sorted[right]
				leftPosition, leftPlaced := position[leftClass]
				rightPosition, rightPlaced := position[rightClass]
				if !leftPlaced || !rightPlaced || (isUnranked[leftClass] && isUnranked[rightClass]) {
					continue
				}
				measurement.comparedPairs++
				// Tailwind put leftClass first, by construction of `sorted`.
				if leftPosition > rightPosition {
					measurement.disagreeingPairs++
					listDisagrees = true
					if len(measurement.examples) < 8 {
						measurement.examples = append(measurement.examples, fmt.Sprintf(
							"%s: Tailwind puts %q before %q; the rule reverses them",
							list.name, leftClass, rightClass,
						))
					}
				}
			}
		}
		if listDisagrees {
			measurement.disagreeingLists++
		}
	}
	return measurement
}

// TestClassOrderLiveMatchesTailwindOverTheSyntheticCases is the half of the acceptance test the corpus
// never ran (#f598zk0). The synthetic cases are the ones built to separate the depth-first rule from the
// mask, and they carry their design system as stylesheet text rather than an entry path, so the corpus
// test, which loads systems by entry path, dropped every one of them: they were captured and never
// compared. Each is loaded here from its own stylesheet over the vendored tailwindcss, so they run on
// every machine.
func TestClassOrderLiveMatchesTailwindOverTheSyntheticCases(test *testing.T) {
	test.Parallel()
	var synthetic []classOrderLiveList
	for _, list := range classOrderLiveLoadCorpus(test) {
		if list.entryPath == "" {
			synthetic = append(synthetic, list)
		}
	}
	if len(synthetic) == 0 {
		test.Fatal("the fixture holds no synthetic class lists, so this compares nothing")
	}

	systems := map[string]DesignSystemResult{}
	for _, list := range synthetic {
		if _, built := systems[list.systemKey()]; built {
			continue
		}
		if list.input == "" {
			test.Fatalf(
				"%s has neither an entry path nor a stylesheet, so nothing says which system sorted it",
				list.name,
			)
		}
		entryPoint := filepath.Join(test.TempDir(), "theme.css")
		if writeError := os.WriteFile(entryPoint, []byte(list.input), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		system, loadDesignSystemError := tailwindcollapse.LoadDesignSystem(tailwindcollapse.LoadOptions{
			EntryPoint:          entryPoint,
			TailwindPackageRoot: vendored.TailwindPackageRootForTest(test),
		})
		if loadDesignSystemError != nil {
			test.Fatalf("%s: loading its stylesheet: %v", list.name, loadDesignSystemError)
		}
		systems[list.systemKey()] = loadedDesignSystemResult(system, entryPoint)
	}

	measurement := classOrderLiveMeasure(test, synthetic, systems)
	test.Logf(
		"synthetic cases vs Tailwind: %d disagreeing pairs of %d compared pairs across %d lists on %d stylesheets (%d classes placed, %d lists declined)",
		measurement.disagreeingPairs,
		measurement.comparedPairs,
		measurement.lists,
		len(systems),
		measurement.placedClasses,
		measurement.listsSkipped,
	)
	for _, example := range measurement.examples {
		test.Log("  " + example)
	}

	// Every synthetic list is placed, or the zero below would be measuring the ones the rule gave up on.
	if measurement.lists != len(synthetic) || measurement.listsSkipped != 0 {
		test.Errorf("the rule placed %d of %d synthetic lists and declined %d; every one of them is built to be placed",
			measurement.lists, len(synthetic), measurement.listsSkipped)
	}
	if measurement.disagreeingPairs != 0 {
		test.Errorf("the live rule disagrees with Tailwind on %d of %d pairs across %d synthetic lists",
			measurement.disagreeingPairs, measurement.comparedPairs, measurement.disagreeingLists)
	}
}

// classOrderLiveReversed returns a class list back to front.
//
// Reversing rather than shuffling, because a shuffle needs a seed to be reproducible and a reversal
// is the one permutation guaranteed to disagree with the target on every separable pair. A sort that
// recovers Tailwind's order from its exact opposite has been asked the question properly.
func classOrderLiveReversed(classes []string) []string {
	reversed := make([]string, len(classes))
	for index, className := range classes {
		reversed[len(classes)-1-index] = className
	}
	return reversed
}

// TestClassOrderLiveStackedVariantsSortByMaskNotByDepth is the known-dirty control.
//
// This is the bug the swap exists to fix, stated as the smallest input that shows it. The shipped
// rule asserted "every single variant precedes every stacked one, whatever they start with", and
// Tailwind implements nothing of the kind: it ORs one bit per variant into a mask and compares masks
// numerically, so a class's position is decided by its highest-ranked variant.
//
// `group-hover:disabled:flex` carries two variants and mask 5. `dark:flex` carries one and mask 8.
// Tailwind sorts the stacked class first, and the depth-first rule reversed it. Both fixtures are
// Tailwind's own answers, quoted from variant_fixtures.json, not this port's output.
//
// The pairs are asserted rather than merely counted so that a corpus that lost its stacked-variant
// cases could not turn this suite green by having nothing left to disagree about.
func TestClassOrderLiveStackedVariantsSortByMaskNotByDepth(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name     string
		input    []string
		expected []string
		why      string
	}{
		{
			name:     "a stacked variant sorts before a higher-ranked single one",
			input:    []string{"dark:flex", "group-hover:disabled:flex"},
			expected: []string{"group-hover:disabled:flex", "dark:flex"},
			why: "masks 5 and 8: the stacked class carries only lower bits, so it sorts first " +
				"despite having more variants",
		},
		{
			name:     "and it lands between the singles rather than after them",
			input:    []string{"dark:flex", "group-hover:disabled:flex", "hover:flex", "group-hover:flex"},
			expected: []string{"group-hover:flex", "hover:flex", "group-hover:disabled:flex", "dark:flex"},
			why:      "depth-first would put the two-variant class last; Tailwind puts it third",
		},
		{
			name:     "an arbitrary variant sorts after a stacked registered one",
			input:    []string{"[&:hover]:flex", "dark:hover:flex", "hover:flex"},
			expected: []string{"hover:flex", "dark:hover:flex", "[&:hover]:flex"},
			why:      "arbitrary variants rank after every registered one, so mask 4 beats mask 3",
		},
		{
			name:     "stacking order does not change the mask",
			input:    []string{"hover:dark:flex", "dark:hover:flex"},
			expected: []string{"dark:hover:flex", "hover:dark:flex"},
			why:      "both set the same two bits, so they tie on the mask and separate on the name",
		},
	}

	forEachTailwindSystem(test, func(test *testing.T, designSystem DesignSystemResult) {
		for _, testCase := range testCases {
			test.Run(testCase.name, func(test *testing.T) {
				test.Parallel()
				unranked, placeable := partitionUnranked(testCase.input, designSystem)
				keys, unplaceable, resolved := classOrderKeys(placeable, designSystem.System, designSystem.Table)
				if !resolved {
					test.Fatalf("the rule could not place %q, so this case proves nothing", unplaceable)
				}
				ordered := append(unranked, sortClassesByKey(placeable, keys)...)
				if strings.Join(ordered, " ") != strings.Join(testCase.expected, " ") {
					test.Errorf("got %v, Tailwind says %v: %s", ordered, testCase.expected, testCase.why)
				}
			})
		}
	})
}

// TestClassOrderLiveDeclinesRatherThanPartiallySorting pins the fail-safe.
//
// A class the design system cannot place must stop the whole literal, not be sorted around. Sorting
// the classes that resolved would reorder correct code on the strength of a lookup that failed, and
// the author would be asked to make a change Tailwind does not agree with.
func TestClassOrderLiveDeclinesRatherThanPartiallySorting(test *testing.T) {
	test.Parallel()
	forEachTailwindSystem(test, func(test *testing.T, designSystem DesignSystemResult) {

		_, unplaceable, resolved := classOrderKeys(
			[]string{"items-center", "flex", "ahralia-splash"}, designSystem.System, designSystem.Table)
		if resolved {
			test.Fatal("a class outside the design system was placed, so the rule would sort a literal it " +
				"does not understand")
		}
		if unplaceable != "ahralia-splash" {
			test.Errorf("the decline should name the class that caused it, got %q", unplaceable)
		}
	})
}

// TestClassOrderLiveVariantIndicesAreRanksWithinTheList is the mechanism test.
//
// It is the reason this port sorts a list rather than comparing pairs, and it is stated as an
// assertion because a comment saying "the index is population-scoped" decays and a measurement does
// not. The same variant takes a different index depending on what else the list contains, so a key
// computed for one list is meaningless in another.
func TestClassOrderLiveVariantIndicesAreRanksWithinTheList(test *testing.T) {
	test.Parallel()
	forEachTailwindSystem(test, func(test *testing.T, designSystem DesignSystemResult) {

		maskOf := func(classes []string, target string) string {
			keys, unplaceable, resolved := classOrderKeys(classes, designSystem.System, designSystem.Table)
			if !resolved {
				test.Fatalf("could not place %q", unplaceable)
			}
			key, placed := keys[target]
			if !placed {
				test.Fatalf("%q got no key", target)
			}
			if key.mask == nil {
				return "0"
			}
			return key.mask.String()
		}

		narrow := maskOf([]string{"dark:flex", "hover:flex"}, "dark:flex")
		wide := maskOf([]string{"dark:flex", "hover:flex", "group-hover:disabled:flex"}, "dark:flex")

		if narrow == wide {
			test.Errorf(
				"`dark:flex` holds mask %s in both a two-class and a four-variant list; the index is "+
					"supposed to be a rank within the population, so a pairwise comparator would be a "+
					"valid shape after all and this port's central claim is wrong",
				narrow,
			)
		}
		test.Logf("`dark:flex` masks %s in the narrow list and %s in the wide one", narrow, wide)
	})
}

// TestClassOrderLiveUnrankedLeadInSourceOrder pins the one dimension Tailwind has no opinion on.
//
// `getClassOrder` returns null for `group` and `peer` and for every class that compiles to nothing,
// so the corpus agreement above says nothing about where they belong. The convention is Tailwind's
// own Prettier plugin's: every null leads, and two nulls keep the order they were written in.
//
// The non-marker nulls are the case #vf1hd6j found missing. `text-dark` and `dark:bg-dark-2` are
// classes the plugin ranked null on the committed trees, and `ahralia-splash` does not parse at all.
// `ahra` declares no `dark` colour, which this asserts rather than assumes: the test is about nulls
// and must not quietly turn into a test about two ranked classes.
func TestClassOrderLiveUnrankedLeadInSourceOrder(test *testing.T) {
	test.Parallel()
	forEachTailwindSystem(test, func(test *testing.T, designSystem DesignSystemResult) {

		for _, nullClass := range []string{"peer", "group", "text-dark", "dark:bg-dark-2", "ahralia-splash"} {
			if !isMarkerClass(nullClass) && classCompilesIn(nullClass, designSystem) {
				test.Fatalf(
					"%q compiles in this repository, so it is not a null and this test proves nothing",
					nullClass,
				)
			}
		}

		for _, testCase := range []struct {
			input, unranked []string
		}{
			{[]string{"flex", "peer", "group", "items-center"}, []string{"peer", "group"}},
			{[]string{"flex", "group", "peer", "items-center"}, []string{"group", "peer"}},
			{[]string{"flex", "text-dark", "peer", "items-center"}, []string{"text-dark", "peer"}},
			{
				[]string{"items-center", "ahralia-splash", "dark:bg-dark-2", "flex"},
				[]string{"ahralia-splash", "dark:bg-dark-2"},
			},
		} {
			unranked, placeable := partitionUnranked(testCase.input, designSystem)
			// Source order between them, which is what makes `peer group` and `group peer` both legal.
			if strings.Join(unranked, " ") != strings.Join(testCase.unranked, " ") {
				test.Errorf(
					"from %v the nulls are %v; they should be %v, in source order",
					testCase.input,
					unranked,
					testCase.unranked,
				)
			}
			if len(placeable) != 2 {
				test.Errorf("expected two placeable classes from %v, got %v", testCase.input, placeable)
			}
		}
	})
}
