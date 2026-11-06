package tailwind

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	tailwindengine "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
)

// strictClassOrder is upstream's `strict` order: the `official` order, regrouped so classes sharing a
// variant stack sit together, nested by variant, with arbitrary variants after named ones at every
// level. It is `sortClassNames`'s tail and `getStrictOrder` (enforce-consistent-class-order.js at
// 4.7.0), applied to the list the official order produced.
//
// Each class's variants are the first parse's, printed as Tailwind prints them and in the order
// written. A class that does not parse takes everything before its last colon, split on colons,
// upstream's fallback for component classes. Upstream builds both levels as JavaScript objects keyed
// by name, so they enumerate in JavaScript's key order, which jsKeyOrder reproduces.
func strictClassOrder(officially []string, system *tailwindengine.LoadedDesignSystem) []string {
	root := newStrictVariantLevel()
	for _, className := range jsKeyOrder(officially) {
		variants := append([]string{""}, strictVariantsOf(className, system)...)
		level := root
		for index, variant := range variants {
			entry := level.entry(variant)
			if index == len(variants)-1 {
				entry.classes = append(entry.classes, className)
				break
			}
			level = entry.nested
		}
	}
	return root.order()
}

type strictVariantEntry struct {
	classes []string
	nested  *strictVariantLevel
}

func newStrictVariantLevel() *strictVariantLevel {
	return &strictVariantLevel{entries: map[string]*strictVariantEntry{}}
}

// isArbitraryVariant is upstream's test: a variant holding both brackets.
func isArbitraryVariant(variant string) bool {
	return variant != "" && strings.Contains(variant, "[") && strings.Contains(variant, "]")
}

// beforeLastColon is upstream's fallback for a class that does not parse: everything before its last
// colon, the greedy `^(.*):`, which like JavaScript's `.` stops at a line break.
var beforeLastColon = regexp.MustCompile(`^(.*):`)

// strictVariantsOf is one class's variants, in the order written.
func strictVariantsOf(className string, system *tailwindengine.LoadedDesignSystem) []string {
	if system != nil {
		if parsed := tailwindengine.ParseCandidate(className, system); len(parsed) > 0 {
			// The parser keeps variants in the order the compiler applies them, the reverse of how
			// they were written; upstream reverses its own list the same way.
			variants := parsed[0].Variants
			printed := make([]string, 0, len(variants))
			for index := len(variants) - 1; index >= 0; index-- {
				printed = append(printed, tailwindengine.PrintVariant(variants[index]))
			}
			return printed
		}
	}
	match := beforeLastColon.FindStringSubmatch(className)
	if match == nil {
		return nil
	}
	return strings.Split(match[1], ":")
}

// jsKeyOrder is the order a JavaScript object enumerates these keys in, inserted in this order:
// every key that is an array index (a canonical decimal below 2^32 - 1) first, ascending, then the
// rest in insertion order. A repeated key keeps its first position.
func jsKeyOrder(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	var indexes []string
	var rest []string
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		if isArrayIndexKey(key) {
			indexes = append(indexes, key)
		} else {
			rest = append(rest, key)
		}
	}
	sort.SliceStable(indexes, func(left int, right int) bool {
		// Ignored: indexes holds only keys isArrayIndexKey already parsed as a uint below 2^32 - 1.
		leftValue, _ := strconv.ParseUint(indexes[left], 10, 64)
		// Ignored: the same keys, so this parse cannot fail either.
		rightValue, _ := strconv.ParseUint(indexes[right], 10, 64)
		return leftValue < rightValue
	})
	return append(indexes, rest...)
}

// isArrayIndexKey reports whether a JavaScript object would treat a key as an array index.
func isArrayIndexKey(key string) bool {
	value, parseError := strconv.ParseUint(key, 10, 64)
	return parseError == nil && value < 1<<32-1 && strconv.FormatUint(value, 10) == key
}
