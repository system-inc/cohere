// Package unicodeproperties provides ECMAScript character properties from Unicode 17.0.0.
// Aliases follow ECMAScript's exact, case-sensitive spellings, without UCD loose matching.
package unicodeproperties

import "slices"

//go:generate go run ./internal/generate

// Range is an inclusive interval of Unicode code points, including surrogate code points.
type Range struct{ Lo, Hi rune }

// Property is a Unicode character property. Ranges are sorted and disjoint.
// The returned slices belong to the tables and must not be modified.
type Property struct {
	Family string
	Ranges []Range
}

// Lookup resolves a JavaScript Unicode property escape's contents, without braces.
// It accepts general categories, scripts, script extensions, and ECMAScript binary properties.
func Lookup(name string) (Property, bool) {
	id, ok := aliases[name]
	if !ok {
		return Property{}, false
	}
	return properties[id], true
}

// Names returns every accepted property spelling in sorted order.
func Names() []string {
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
