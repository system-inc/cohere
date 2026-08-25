// The framework's single-declaration functional utilities.
//
// Wave 1 of #271785y: the roots whose `handle` body is `(value) => [decl(property, value)]`, meaning
// the resolved value reaches the declaration unchanged. 25 of them, and they are a table rather than
// 25 ported functions because there is nothing per-root to port: the property name, the theme
// namespaces, and which shared bare-value predicate applies are the entire difference between them.
//
// # Why the boundary is the handler's shape, not its line count
//
// The milestone body proposed waves of 15 / 71 / 28 by line count, from a measurement that counted
// registration call sites. Ten of the "15 smallest" were one-line delegations to a shared closure
// (`utilities.functional('rotate', handleRotate(...))`) and two were generic lines inside
// `functionalUtility` itself. Re-measured by shape: 72 `functionalUtility` blocks, 30 direct
// registrations, and 36 of the 72 pass their value through untouched.
//
// Of those 36, only 25 are mechanical. The other 11 transform the value in the bare handler, such as
// `outline-offset` returning `${value}px`, and they belong to wave 2 with the rest of the
// transforming bodies.
//
// # The bare handlers are a shared predicate, not per-root code
//
// Across the file there are 49 `handleBareValue` occurrences and 33 distinct bodies, and one body
// accounts for 15 of them: reject anything that is not a positive integer, otherwise pass the value
// through. That is `BareValuePositiveInteger` here, referenced by name from the table rather than
// duplicated, so a correction to the predicate cannot reach 11 roots and miss the twelfth.
//
// # What this file deliberately does not do
//
// It does not emit CSS. `Property` is the declaration a root produces, and the reading is PropertySort
// over one declaration carrying that property, because `getPropertySort` reads `node.property` and
// counts and never reads a value. `StaticValues` carry their own property for the same reason: a
// static value can declare a different property than the root's ordinary path, and the count is what
// the reading needs rather than the text.
package tailwind

// BareValueKind names one of the shared bare-value predicates.
//
// A named kind rather than a func field, because the table is data and the predicates are code: a
// closure per row would make the table 25 anonymous functions that all look alike and hide which
// ones are genuinely the same. The kind makes the sharing visible at the call site.
type BareValueKind string

const (
	// BareValueNone means the root has no bare-value handler, so a bare value that misses the theme
	// falls through to staticValues and then produces nothing.
	BareValueNone BareValueKind = ""
	// BareValuePositiveInteger is upstream's most common handler by a wide margin: reject anything
	// that is not a positive integer, otherwise pass the value through unchanged.
	BareValuePositiveInteger BareValueKind = "PositiveInteger"
)

// FrameworkStaticValue is one entry of a root's `staticValues` map.
//
// Property is carried rather than inherited from the root because a static value may declare a
// different one. `aspect-square` declares `aspect-ratio` like its root does, but the shape allows a
// root whose static entries diverge, and inheriting would be an assumption this table cannot check.
type FrameworkStaticValue struct {
	Name     string
	Property string
	Value    string
}

// FrameworkFunctionalUtility is one single-declaration root.
type FrameworkFunctionalUtility struct {
	// Property is the declaration the root's handle body emits.
	Property string
	// ThemeKeys are the namespaces the root consults, in order.
	ThemeKeys []string
	// SupportsNegative registers a second root with a leading dash.
	SupportsNegative bool
	// DefaultValue is what a valueless candidate resolves to. Absent and empty differ, so the
	// presence flag is separate rather than encoded as the empty string.
	DefaultValue        string
	DefaultValuePresent bool
	// BareValue names the shared predicate this root uses, if any.
	BareValue BareValueKind
	// StaticValues are the named values the root answers directly.
	StaticValues []FrameworkStaticValue
}

// Description converts a table row into the shape ResolveFunctionalUtilityValue takes.
//
// The conversion is here rather than in the table so the table stays data. It is also where the
// shared predicate becomes a closure, which is the one place a `BareValueKind` turns back into code.
func (utility FrameworkFunctionalUtility) Description() *FunctionalUtilityDescription {
	description := &FunctionalUtilityDescription{
		ThemeKeys:           utility.ThemeKeys,
		SupportsNegative:    utility.SupportsNegative,
		DefaultValue:        utility.DefaultValue,
		DefaultValuePresent: utility.DefaultValuePresent,
	}

	if utility.BareValue == BareValuePositiveInteger {
		description.HandleBareValue = func(value *ParsedValue) (string, bool) {
			if value == nil || !isPositiveInteger(value.Value) {
				return "", false
			}
			return value.Value, true
		}
	}

	if len(utility.StaticValues) > 0 {
		description.StaticValueNames = make(map[string]bool, len(utility.StaticValues))
		for _, static := range utility.StaticValues {
			description.StaticValueNames[static.Name] = true
		}
	}
	return description
}

// Reading returns what a candidate reads against this root, and whether it reads anything.
//
// One declaration either way. A static value declares its own property and the ordinary path
// declares the root's, and both are a single declaration, so the count is 1 wherever a value
// resolved at all. That is not a simplification of the engine: it is what a single-declaration
// handle body means, and it is the reason these 25 roots are a table.
func (utility FrameworkFunctionalUtility) Reading(candidate *ParsedCandidate, theme *Theme, negative bool) (Reading, bool) {
	resolved, produced := ResolveFunctionalUtilityValue(candidate, utility.Description(), theme, negative)
	if !produced {
		return Reading{}, false
	}

	// A static value carries its own property, and in this wave it is always the root's own.
	//
	// Measured across all 16 wave-1 roots that declare staticValues: zero entries name a property
	// different from their root's. So this branch is unreachable here, and mutating it away does not
	// fail the suite. That is recorded rather than hidden, because the alternative readings are both
	// wrong: it is not dead code, since wave 2 has roots whose static entries do diverge, and it is
	// not tested, since nothing in this wave can exercise it.
	//
	// Kept rather than deferred because inheriting the root's property would be an assumption the
	// table cannot check, and the shape has to be right before wave 2 lands on it.
	property := utility.Property
	if resolved.IsStaticValue {
		for _, static := range utility.StaticValues {
			if static.Name == resolved.StaticValueName {
				property = static.Property
				break
			}
		}
	}

	sorted := PropertySort([]*Node{{
		Kind:         KindDeclaration,
		Property:     property,
		Value:        resolved.Value,
		ValuePresent: true,
	}})
	return Reading{Order: sorted.Order, Count: sorted.Count}, true
}

// FrameworkFunctionalUtilities is the table.
//
// Extracted from `utilities.ts` at 4.3.3 by matching the `handle: (value) => [decl(p, value)]` shape,
// then spot-checked against the source. That check found a defect in the extractor rather than in the
// source: an indentation-sensitive pattern had classified `outline-offset` as a pass-through when its
// bare handler returns `${value}px`, which moved it and three others out of this table and into wave
// 2. The count here is 25 because of that correction, not 29.
var FrameworkFunctionalUtilities = map[string]FrameworkFunctionalUtility{
	"align":              {Property: "vertical-align"},
	"animate":            {Property: "animation", ThemeKeys: []string{"--animate"}, StaticValues: []FrameworkStaticValue{{Name: "none", Property: "animation", Value: "none"}}},
	"col":                {Property: "grid-column", ThemeKeys: []string{"--grid-column"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, StaticValues: []FrameworkStaticValue{{Name: "auto", Property: "grid-column", Value: "auto"}}},
	"col-end":            {Property: "grid-column-end", ThemeKeys: []string{"--grid-column-end"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, StaticValues: []FrameworkStaticValue{{Name: "auto", Property: "grid-column-end", Value: "auto"}}},
	"col-start":          {Property: "grid-column-start", ThemeKeys: []string{"--grid-column-start"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, StaticValues: []FrameworkStaticValue{{Name: "auto", Property: "grid-column-start", Value: "auto"}}},
	"columns":            {Property: "columns", ThemeKeys: []string{"--columns", "--container"}, BareValue: BareValuePositiveInteger, StaticValues: []FrameworkStaticValue{{Name: "auto", Property: "columns", Value: "auto"}}},
	"contain":            {Property: "contain"},
	"cursor":             {Property: "cursor", ThemeKeys: []string{"--cursor"}},
	"font-features":      {Property: "font-feature-settings"},
	"grow":               {Property: "flex-grow", DefaultValue: "1", DefaultValuePresent: true, BareValue: BareValuePositiveInteger},
	"list":               {Property: "list-style-type", ThemeKeys: []string{"--list-style-type"}, StaticValues: []FrameworkStaticValue{{Name: "none", Property: "list-style-type", Value: "none"}, {Name: "disc", Property: "list-style-type", Value: "disc"}, {Name: "decimal", Property: "list-style-type", Value: "decimal"}}},
	"list-image":         {Property: "list-style-image", ThemeKeys: []string{"--list-style-image"}, StaticValues: []FrameworkStaticValue{{Name: "none", Property: "list-style-image", Value: "none"}}},
	"mask-radial-at":     {Property: "--tw-mask-radial-position"},
	"object":             {Property: "object-position", ThemeKeys: []string{"--object-position"}, StaticValues: []FrameworkStaticValue{{Name: "top", Property: "object-position", Value: "top"}, {Name: "top-left", Property: "object-position", Value: "left top"}, {Name: "top-right", Property: "object-position", Value: "right top"}, {Name: "bottom", Property: "object-position", Value: "bottom"}, {Name: "bottom-left", Property: "object-position", Value: "left bottom"}, {Name: "bottom-right", Property: "object-position", Value: "right bottom"}, {Name: "left", Property: "object-position", Value: "left"}, {Name: "right", Property: "object-position", Value: "right"}, {Name: "center", Property: "object-position", Value: "center"}}},
	"order":              {Property: "order", ThemeKeys: []string{"--order"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, StaticValues: []FrameworkStaticValue{{Name: "first", Property: "order", Value: "-9999"}, {Name: "last", Property: "order", Value: "9999"}}},
	"origin":             {Property: "transform-origin", ThemeKeys: []string{"--transform-origin"}, StaticValues: []FrameworkStaticValue{{Name: "center", Property: "transform-origin", Value: "center"}, {Name: "top", Property: "transform-origin", Value: "top"}, {Name: "top-right", Property: "transform-origin", Value: "100% 0"}, {Name: "right", Property: "transform-origin", Value: "100%"}, {Name: "bottom-right", Property: "transform-origin", Value: "100% 100%"}, {Name: "bottom", Property: "transform-origin", Value: "bottom"}, {Name: "bottom-left", Property: "transform-origin", Value: "0 100%"}, {Name: "left", Property: "transform-origin", Value: "0"}, {Name: "top-left", Property: "transform-origin", Value: "0 0"}}},
	"perspective":        {Property: "perspective", ThemeKeys: []string{"--perspective"}, StaticValues: []FrameworkStaticValue{{Name: "none", Property: "perspective", Value: "none"}}},
	"perspective-origin": {Property: "perspective-origin", ThemeKeys: []string{"--perspective-origin"}, StaticValues: []FrameworkStaticValue{{Name: "center", Property: "perspective-origin", Value: "center"}, {Name: "top", Property: "perspective-origin", Value: "top"}, {Name: "top-right", Property: "perspective-origin", Value: "100% 0"}, {Name: "right", Property: "perspective-origin", Value: "100%"}, {Name: "bottom-right", Property: "perspective-origin", Value: "100% 100%"}, {Name: "bottom", Property: "perspective-origin", Value: "bottom"}, {Name: "bottom-left", Property: "perspective-origin", Value: "0 100%"}, {Name: "left", Property: "perspective-origin", Value: "0"}, {Name: "top-left", Property: "perspective-origin", Value: "0 0"}}},
	"row":                {Property: "grid-row", ThemeKeys: []string{"--grid-row"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, StaticValues: []FrameworkStaticValue{{Name: "auto", Property: "grid-row", Value: "auto"}}},
	"row-end":            {Property: "grid-row-end", ThemeKeys: []string{"--grid-row-end"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, StaticValues: []FrameworkStaticValue{{Name: "auto", Property: "grid-row-end", Value: "auto"}}},
	"row-start":          {Property: "grid-row-start", ThemeKeys: []string{"--grid-row-start"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, StaticValues: []FrameworkStaticValue{{Name: "auto", Property: "grid-row-start", Value: "auto"}}},
	"shrink":             {Property: "flex-shrink", DefaultValue: "1", DefaultValuePresent: true, BareValue: BareValuePositiveInteger},
	"tab":                {Property: "tab-size", BareValue: BareValuePositiveInteger},
	"will-change":        {Property: "will-change"},
	"z":                  {Property: "z-index", ThemeKeys: []string{"--z-index"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, StaticValues: []FrameworkStaticValue{{Name: "auto", Property: "z-index", Value: "auto"}}},
}
