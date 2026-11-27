// The live class-order sort: one repository's own design system deciding its own class order.
//
// This is the seam #4q5dsn3 exists to cross. Until this file, `enforce-consistent-class-order`
// answered out of `OrderingPropertiesBy{Root,Static,Class}` and `VariantOrder` in
// property_order_table.go, four tables generated from one repository and then shipped as though
// they described Tailwind. After it, the rule asks the repository in front of it.
//
// # Why a list and not a comparator
//
// The shipped rule sorted with `sort.SliceStable(ordered, func(left, right int) bool { … })` over a
// pairwise `classSortsBefore`, and that shape cannot express what Tailwind does. It is not a
// stylistic difference and it is the whole reason this file replaces a function rather than editing
// one.
//
// `getClassOrder(classes)` takes the entire list. Upstream ranks a candidate by
//
//	let variantOrder = 0n
//	for (let variant of candidate.variants) variantOrder |= 1n << BigInt(variantOrderMap.get(variant)!)
//
// where `variantOrderMap` is `getVariantOrder()`, a *dense index per distinct variant assigned by
// sorting exactly the variants this design system has parsed*. Two consequences, and each one is
// invisible to a pairwise comparator:
//
//   - **The index is a rank within a population, not a fact about a variant.** `dark` is index 3 in
//     one list and index 1 in another, depending on what else is present. A comparator handed two
//     classes has no population to rank within, so it has to invent one, and any per-pair population
//     is a different population from the one Tailwind used.
//   - **Ties collapse to a shared index.** `BuildVariantOrder` advances the index only when Compare
//     says the next variant differs, so two variants that compare equal carry the *same bit* and
//     their classes interleave by property rather than separating. A pairwise comparator that
//     resolved the tie either way would separate them.
//
// So the population is built once over the whole literal, every class is keyed against it, and the
// sort reads the keys. `internal/lint/rules/tailwind/tools/generate_variant/enumerate.mjs` says the same thing from
// Tailwind's side: "The design system is fresh per call, because the order map is a function of
// exactly which variants have been parsed into the design system's cache."
//
// # The bug this removes, measured before it was fixed
//
// `#nr3wtj1`. The shipped comparator asserted, at what was enforce_consistent_class_order.go:447,
// that "every single variant precedes every stacked one, whatever they start with." Tailwind
// implements nothing of the kind. It ORs one bit per variant into a mask and compares masks
// numerically, so a class's position is decided by its *highest-ranked* variant and only ties on
// that are broken by the next bit down. A stacked variant therefore sorts first whenever its
// outermost variant is outranked by the other class's.
//
// Measured on the corpus before this file existed: 15 disagreeing pairs of 4,265 separable pairs
// across 7 class lists. `ahra/list-99` is a real literal in this repository where Tailwind keeps
// `dark:hover:bg-light-50` (mask 5, two variants) ahead of `[&_svg]:me-3` (mask 8, one variant) and
// the shipped rule reversed them, so the rule reported a finding on correctly ordered code and
// asked the author to break it.
//
// # Declining is a state, and it is not the same state as agreeing
//
// Every function here returns a `found` boolean rather than a zero value, for the reason
// `internal/tailwind`'s differential harness carries a five-state outcome lattice instead of two:
// a side that stops answering scores as agreement in any comparison that folds silence into
// equality, and that is the failure this whole port is guarded against. A class this file cannot
// place is reported as unplaceable, the caller declines to report on the literal, and the rule says
// so rather than emitting a clean tree.
package tailwind

import (
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	tailwindcollapse "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
)

// classOrderKey is everything the sort needs about one class, resolved against a live design system.
//
// The variant half is a mask rather than a position, which is the correction this file carries. See
// the file comment: a position implies a total order on variants that Tailwind does not have.
type classOrderKey struct {
	// mask is the variant bitmask, nil for a class carrying no variants.
	//
	// nil rather than a zero big.Int so that "no variants" and "variants that all ranked zero" stay
	// distinguishable while debugging; both compare as zero, which is what Tailwind does.
	mask *big.Int
	// order is the class's property indices, ascending, from the design system's own reading.
	order []int
	// count is how many declarations the utility emitted.
	count int
}

// classOrderKeys resolves every class in a literal against one design system, in one population.
//
// The whole list at once, because the variant indices are ranks within it. Returns false when any
// class cannot be placed, and the caller must then decline rather than sort what it managed to
// resolve: a partial sort silently reorders the classes it understood around the ones it did not,
// which is a rewrite of correct code justified by a lookup that failed.
//
// `unplaceable` names the first class that could not be resolved, so a caller reporting the decline
// can say which class caused it rather than that something did.
func classOrderKeys(
	classes []string,
	system *tailwindcollapse.LoadedDesignSystem,
	table *tailwindcollapse.Table,
) (keys map[string]classOrderKey, unplaceable string, ok bool) {
	if system == nil || table == nil {
		return nil, "", false
	}

	// Parse once and keep the candidates. Parsing is the expensive half and the population below
	// needs every class's variants, so a second parse per class would double the cost of the rule's
	// hot path to recover something already in hand.
	candidates := make(map[string]tailwindcollapse.ParsedCandidate, len(classes))
	readings := make(map[string]tailwindcollapse.Reading, len(classes))
	population := make([]tailwindcollapse.ParsedVariant, 0, len(classes))
	for _, className := range classes {
		parsed := tailwindcollapse.ParseCandidate(className, system)
		if len(parsed) == 0 {
			// Not a utility at all. `group` and `peer` reach here and are handled by the caller,
			// which knows they are markers; anything else is a class outside the design system and
			// `no-unknown-classes` is the rule with something to say about it.
			return nil, className, false
		}
		// The first candidate that compiles. Tailwind compiles every reading of a class and ranks
		// it by what came out, so a reading that emits nothing has no say. This took parsed[0] until
		// #vf1hd6j's acceptance: `shadow--0` parses first as the repository's `@utility shadow--*`
		// with value `0`, which needs a `--shadow-0` nobody declared, and second as the framework's
		// `shadow` with value `-0`, which finds `--shadow--0`. The first emits nothing and the second
		// is what renders, so every literal holding one was declined.
		chosen := -1
		for index := range parsed {
			if reading, hasReading := readingFor(&parsed[index], system, table); hasReading {
				chosen = index
				readings[className] = reading
				break
			}
		}
		if chosen < 0 {
			return nil, className, false
		}
		candidates[className] = parsed[chosen]
		population = append(population, parsed[chosen].Variants...)
	}

	// One order over the whole literal. `BuildVariantOrder` deduplicates, adds every nested
	// sub-variant, sorts by the registry's Compare and assigns dense indices, collapsing ties onto a
	// shared index.
	variantOrder := system.Variants().BuildVariantOrder(population)

	keys = make(map[string]classOrderKey, len(classes))
	for className, candidate := range candidates {
		mask, hasMask := variantOrder.VariantBitmask(candidate.Variants)
		if !hasMask {
			// A variant with no index means the order was built from a different population than
			// the candidate came from, which cannot happen here since both come from `candidates`.
			// Reported rather than assumed away: a partial mask is a mask missing a bit, and a
			// missing high bit reorders the class against every other one.
			return nil, className, false
		}

		reading := readings[className]
		keys[className] = classOrderKey{mask: mask, order: reading.Order, count: reading.Count}
	}
	return keys, "", true
}

// readingFor is the `{order, count}` half, asked of the table and then of the evaluator.
//
// Two sources rather than one, and the order between them is the contract rather than a preference.
// `Table.Lookup` answers the framework's roots out of the measured base table. It deliberately
// declines a root whose arity is per-declaration and every functional `@utility` the repository
// declared, because no row of a descriptor table can answer those: `50` is both an `integer` and a
// `--percentage-*` key so two declarations survive, while `4` is only an integer so one does, and
// arity is therefore not a function of any single data type.
//
// The evaluator can answer them, because it compiles the block. So a decline from the table is an
// escalation and not a failure, which is exactly what descriptor_live.go's
// `addRepositoryFunctionalRoots` exists to make possible: it gives every repository functional root
// a present-but-declining descriptor, so `Lookup` returns false for "this table is not what answers
// it" rather than false for "this root does not exist", and the two are otherwise identical.
func readingFor(
	candidate *tailwindcollapse.ParsedCandidate,
	system *tailwindcollapse.LoadedDesignSystem,
	table *tailwindcollapse.Table,
) (tailwindcollapse.Reading, bool) {
	if reading, found := table.Lookup(candidate); found {
		return reading, true
	}
	return system.Utilities().Reading(candidate)
}

// sortClassesByKey orders a class list the way Tailwind's sort does.
//
// Upstream's comparator in `compile.ts`, in its own order: the variant mask, then the first
// differing index into the property order, then more declarations first, then the class name, read
// as Tailwind's `compare` reads it (compareTailwindCandidates). Each step is Tailwind's and none of
// them is this port's invention.
//
// `SliceStable` rather than `Slice` because the final tiebreak is the class name and two distinct
// classes cannot tie on it, so stability is not load-bearing for the result. It is here so that a
// list containing a duplicate (which the caller rejects before reaching this, but which a future
// caller might not) degrades to source order rather than to whatever the sort happened to do.
func sortClassesByKey(classes []string, keys map[string]classOrderKey) []string {
	ordered := make([]string, len(classes))
	copy(ordered, classes)

	sort.SliceStable(ordered, func(left int, right int) bool {
		leftKey, rightKey := keys[ordered[left]], keys[ordered[right]]

		// The mask, compared numerically. This is the correction: a *set* of bits compared as a
		// number is decided by the highest bit on which the two differ, so the class's position is
		// governed by its single highest-ranked variant. A stacked variant does not sort after every
		// single one.
		if comparison := compareVariantMasks(leftKey.mask, rightKey.mask); comparison != 0 {
			return comparison < 0
		}

		// The first position where the two property lists differ decides. Both are ascending, so
		// this walks them in lockstep exactly as Tailwind does.
		offset := 0
		for offset < len(leftKey.order) &&
			offset < len(rightKey.order) &&
			leftKey.order[offset] == rightKey.order[offset] {
			offset++
		}
		leftIndex := propertyIndexBeyond(leftKey.order, offset)
		rightIndex := propertyIndexBeyond(rightKey.order, offset)
		if leftIndex != rightIndex {
			return leftIndex < rightIndex
		}

		// More declarations first, which is upstream's `z.length - a.length`. `space-x-1` and
		// `me-0.5` reach here with the same first index and are separated by nothing else.
		if leftKey.count != rightKey.count {
			return leftKey.count > rightKey.count
		}
		return compareTailwindCandidates(ordered[left], ordered[right]) < 0
	})

	return ordered
}

// compareTailwindCandidates ports `utils/compare.ts`, the `compare` behind upstream's "Sort
// alphabetically" step: alphabetical, except that a digit run in one name against a digit run in the
// other compares as a number.
//
// That step decides every pair the earlier ones tie on, which is any two classes of one variant mask
// declaring the same properties the same number of times: `swell-5` and `swell-15`, `z-5` and `z-10`.
// Byte order puts `swell-15` first because `1` is below `5`, and Tailwind puts it second (#9zs88f3).
// Over the public theme's 23,740 registry classes the two orders disagree on 37,422 tied pairs.
//
// Walked by byte rather than by UTF-16 unit, which upstream's `charCodeAt` reads, and it comes to the
// same answer. Digits are ASCII and no byte of a multi-byte UTF-8 sequence is one, so the digit runs
// are the same runs; the walk only ever compares equal prefixes, so the two names share every rune
// boundary up to the first difference and a prefix in bytes is a prefix in units. The one place the
// encodings disagree is which of two different non-ASCII characters is lower, so a difference inside
// one is handed to compareJavaScriptStrings from the start of its rune.
func compareTailwindCandidates(left string, right string) int {
	shorter := min(len(left), len(right))
	for index := 0; index < shorter; index++ {
		leftByte, rightByte := left[index], right[index]

		if isAsciiDigit(leftByte) && isAsciiDigit(rightByte) {
			leftEnd, rightEnd := index+1, index+1
			for leftEnd < len(left) && isAsciiDigit(left[leftEnd]) {
				leftEnd++
			}
			for rightEnd < len(right) && isAsciiDigit(right[rightEnd]) {
				rightEnd++
			}
			leftNumber, rightNumber := left[index:leftEnd], right[index:rightEnd]

			// Upstream subtracts two `Number`s and goes on only when the difference is falsy. A float64
			// is a `Number`, and ParseFloat rounds a long run the way `Number` does and returns an
			// infinity past the largest double. Two infinities subtract to NaN, which is falsy too, so
			// NaN falls through with zero, exactly as `if (diff)` lets it.
			// Ignored: a run of ASCII digits always parses, and the one error left, out of range, comes with
			// the infinity the paragraph above relies on.
			leftValue, _ := strconv.ParseFloat(leftNumber, 64)
			// Ignored: the same run of digits on the other side, for the same reason.
			rightValue, _ := strconv.ParseFloat(rightNumber, 64)
			if difference := leftValue - rightValue; difference != 0 && !math.IsNaN(difference) {
				if difference < 0 {
					return -1
				}
				return 1
			}

			// The same number spelled differently, `0123` against `123`, falls back to the spelling.
			if leftNumber != rightNumber {
				return strings.Compare(leftNumber, rightNumber)
			}

			// Upstream steps one character on rather than past the run, so the rest of the run is
			// walked again, finds equal digits and moves on. Stepping one keeps it identical.
			continue
		}

		if leftByte == rightByte {
			continue
		}
		if leftByte < utf8.RuneSelf && rightByte < utf8.RuneSelf {
			return int(leftByte) - int(rightByte)
		}
		runeStart := index
		for runeStart > 0 && !(utf8.RuneStart(left[runeStart]) && utf8.RuneStart(right[runeStart])) {
			runeStart--
		}
		return compareJavaScriptStrings(left[runeStart:], right[runeStart:])
	}

	// Equal as far as the shorter one goes, so the shorter one sorts first.
	return len(left) - len(right)
}

// isAsciiDigit is upstream's `code >= ZERO && code <= NINE`.
func isAsciiDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

// compareVariantMasks orders two masks, treating a nil mask as zero.
//
// nil is how a class with no variants is spelled here, and zero is what Tailwind's `0n` gives it,
// so the two must compare identically. Written out rather than allocating a zero big.Int per
// comparison, on a path that runs once per pair of classes in every class attribute in the tree.
func compareVariantMasks(left *big.Int, right *big.Int) int {
	switch {
	case left == nil && right == nil:
		return 0
	case left == nil:
		return -right.Sign()
	case right == nil:
		return left.Sign()
	default:
		return tailwindcollapse.CompareVariantBitmasks(left, right)
	}
}

// propertyIndexBeyond returns the index at a position, or a value past every real one.
//
// Tailwind uses Infinity here, so a class that ran out of properties sorts after one that still
// has more. A class declaring nothing at all therefore sorts last among its variant group rather
// than first, which a control in Phase 0 got backwards and reported 365,174 spurious mismatches for
// while the model underneath was fine.
func propertyIndexBeyond(order []int, offset int) int {
	if offset < len(order) {
		return order[offset]
	}
	return 1 << 30
}
