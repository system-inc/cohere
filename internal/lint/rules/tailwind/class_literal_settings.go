package tailwind

import (
	"encoding/json"
	"fmt"
)

// ClassLiteralSettings is the selectors a rule reads class strings through, upstream's flat
// `selectors` after its legacy options and settings have merged into them.
//
// Every surface matters, and that is not obvious. On the ahra tree the attribute surface holds 7,773
// literals and the others hold 2,192, so a rule that reads only JSX attributes silently covers 78% of
// the class surface while reporting a clean tree for the rest. Every rule in this package reads
// through this type for that reason.
type ClassLiteralSettings struct {
	Selectors []Selector
}

// key is the settings as one string, every selector in order with every field, so two settings share
// a key only when they would read every node the same way. Built once per configuration selection.
func (settings ClassLiteralSettings) key() string {
	encoded, encodeError := json.Marshal(settings.Selectors)
	if encodeError != nil {
		// Invariant: a Selector holds only strings, slices of matchers and targets whose MarshalJSON writes a
		// string or an int, none of which json.Marshal can fail on.
		panic(fmt.Sprintf("selectors that decoded do not encode: %v", encodeError))
	}
	return string(encoded)
}
