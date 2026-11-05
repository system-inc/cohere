package tailwind

import "sort"

// strictVariantLevel is one level of the variant tree: each variant written at it, in JavaScript's
// key order, with the classes that stop there and the level beneath.
type strictVariantLevel struct {
	names   []string
	entries map[string]*strictVariantEntry
}

func (level *strictVariantLevel) entry(variant string) *strictVariantEntry {
	if existing, isPresent := level.entries[variant]; isPresent {
		return existing
	}
	created := &strictVariantEntry{nested: newStrictVariantLevel()}
	level.entries[variant] = created
	level.names = append(level.names, variant)
	return created
}

// order flattens a level: each variant's classes, then its nested level, with arbitrary variants
// moved after the rest by a stable sort, as upstream's `getStrictOrder` does.
func (level *strictVariantLevel) order() []string {
	names := jsKeyOrder(level.names)
	sort.SliceStable(names, func(left int, right int) bool {
		return !isArbitraryVariant(names[left]) && isArbitraryVariant(names[right])
	})
	ordered := []string{}
	for _, name := range names {
		entry := level.entries[name]
		ordered = append(ordered, entry.classes...)
		if len(entry.nested.names) > 0 {
			ordered = append(ordered, entry.nested.order()...)
		}
	}
	return ordered
}
