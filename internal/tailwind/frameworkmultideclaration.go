// The framework's multi-declaration functional utilities.
//
// Wave 2b of #271785y: the 21 roots that emit more than one declaration, all of them building a
// `--tw-*` custom property and then declaring the real property that reads it. The filter family,
// the backdrop-filter family, the skews, and `ease`.
//
// # Why these carry a reading and waves 1 and 2a carry a property
//
// The earlier waves emit one declaration, so naming its property is enough and PropertySort computes
// the rest. These do not, and the source cannot be read to find out how many. Twenty-one of them
// match one pattern in `utilities.ts` (a properties wrapper, a `--tw-*` declaration, a final
// declaration), and extracting that pattern predicts they all read alike. Measured against the
// engine, they do not:
//
//	grayscale     [334,339]#2      one --tw- variable plus filter
//	skew-3        [69,70,71]#3     writes both --tw-skew-x and --tw-skew-y
//	skew-x-3      [69,71]#2        writes one
//	ease-linear   [354]#2          two declarations sharing one property position
//
// `ease` is the case that settles the shape. Two declarations at a single order index means a table
// keyed on property names would have said `#1`, and nothing in the handler's text says otherwise. So
// the reading is measured per root rather than derived, and this table carries `{order, count}`.
//
// # The wrapper contributes nothing, and that is measured rather than assumed
//
// Every one of these calls a properties wrapper first: `filterProperties()`, `transformProperties()`,
// `backdropFilterProperties()`. Each returns `atRoot([...])` holding a dozen `@property` blocks, and
// PropertySort descends into rules and at-rules but not into at-root, so the wrapper adds zero to the
// reading. `grayscale` reading `[334,339]#2` rather than `#15` is the confirmation: thirteen
// `@property` declarations are structurally invisible.
//
// # Repository-invariant, and checked rather than claimed
//
// Generated against ahra and against www-connected-app, which resolve different themes. Zero roots
// differ in their reading set, 256 answered on each. A root whose reading depended on a repository's
// tokens would show up as a differing set, and none does, which is what makes this checked in rather
// than derived live like the namespace half of the descriptor table.
package tailwind

// FrameworkMultiDeclarationUtility is one root whose reading was measured rather than derived.
//
// The value axes (theme keys, bare-value predicate, default) are here for the same reason they are in
// FrameworkFunctionalUtility: they decide whether a class resolves at all. What they do not decide is
// the reading, which is why Reading is a field rather than a computation.
type FrameworkMultiDeclarationUtility struct {
	// Reading is what this root reads for any value that resolves.
	Reading Reading
	// LiteralReadings are the root-defined keywords that read differently from everything else.
	//
	// One root uses this: `ease-initial` reads `[]#1` where every other `ease-*` reads `[354]#2`,
	// because `initial` is a literal the handler answers directly rather than a value it wraps. It is
	// a map rather than a flag so a second such literal costs a row and not a field.
	LiteralReadings map[string]Reading

	ThemeKeys           []string
	SupportsNegative    bool
	DefaultValue        string
	DefaultValuePresent bool
	BareValue           BareValueKind
	BareValueSuffix     string
}

// Description converts a row into the shape ResolveFunctionalUtilityValue takes.
func (utility FrameworkMultiDeclarationUtility) Description() *FunctionalUtilityDescription {
	description := &FunctionalUtilityDescription{
		ThemeKeys:           utility.ThemeKeys,
		SupportsNegative:    utility.SupportsNegative,
		DefaultValue:        utility.DefaultValue,
		DefaultValuePresent: utility.DefaultValuePresent,
	}
	if predicate := bareValuePredicate(utility.BareValue); predicate != nil {
		suffix := utility.BareValueSuffix
		description.HandleBareValue = func(value *ParsedValue) (string, bool) {
			if value == nil || !predicate(value.Value) {
				return "", false
			}
			return value.Value + suffix, true
		}
	}
	return description
}

// Reading returns what a candidate reads against this root, and whether it reads anything.
//
// The literal check runs before resolution, matching upstream: a root-defined keyword is answered by
// the handler directly and never reaches the theme. Checking after would let a repository declaring
// `--ease-initial` shadow the literal, which the engine does not allow.
func (utility FrameworkMultiDeclarationUtility) ReadingFor(candidate *ParsedCandidate, theme *Theme, negative bool) (Reading, bool) {
	if candidate == nil {
		return Reading{}, false
	}
	if candidate.Value != nil && candidate.Value.Kind == ParsedValueKindNamed {
		if reading, found := utility.LiteralReadings[candidate.Value.Value]; found {
			return reading, true
		}
	}
	if _, produced := ResolveFunctionalUtilityValue(candidate, utility.Description(), theme, negative); !produced {
		return Reading{}, false
	}
	return utility.Reading, true
}

// FrameworkMultiDeclarationUtilities is the table.
var FrameworkMultiDeclarationUtilities = map[string]FrameworkMultiDeclarationUtility{
	"backdrop-blur":       {Reading: Reading{Order: []int{340, 349}, Count: 3}, ThemeKeys: []string{"--backdrop-blur", "--blur"}, LiteralReadings: map[string]Reading{"none": Reading{Order: []int{340, 349}, Count: 3}}},
	"backdrop-brightness": {Reading: Reading{Order: []int{341, 349}, Count: 3}, ThemeKeys: []string{"--backdrop-brightness", "--brightness"}, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"backdrop-contrast":   {Reading: Reading{Order: []int{342, 349}, Count: 3}, ThemeKeys: []string{"--backdrop-contrast", "--contrast"}, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"backdrop-grayscale":  {Reading: Reading{Order: []int{343, 349}, Count: 3}, ThemeKeys: []string{"--backdrop-grayscale", "--grayscale"}, DefaultValue: "100%", DefaultValuePresent: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"backdrop-hue-rotate": {Reading: Reading{Order: []int{344, 349}, Count: 3}, ThemeKeys: []string{"--backdrop-hue-rotate", "--hue-rotate"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "deg"},
	"backdrop-invert":     {Reading: Reading{Order: []int{345, 349}, Count: 3}, ThemeKeys: []string{"--backdrop-invert", "--invert"}, DefaultValue: "100%", DefaultValuePresent: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"backdrop-opacity":    {Reading: Reading{Order: []int{346, 349}, Count: 3}, ThemeKeys: []string{"--backdrop-opacity", "--opacity"}, BareValue: BareValueOpacity, BareValueSuffix: "%"},
	"backdrop-saturate":   {Reading: Reading{Order: []int{347, 349}, Count: 3}, ThemeKeys: []string{"--backdrop-saturate", "--saturate"}, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"backdrop-sepia":      {Reading: Reading{Order: []int{348, 349}, Count: 3}, ThemeKeys: []string{"--backdrop-sepia", "--sepia"}, DefaultValue: "100%", DefaultValuePresent: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"blur":                {Reading: Reading{Order: []int{330, 339}, Count: 2}, ThemeKeys: []string{"--blur"}, LiteralReadings: map[string]Reading{"none": Reading{Order: []int{330, 339}, Count: 2}}},
	"brightness":          {Reading: Reading{Order: []int{331, 339}, Count: 2}, ThemeKeys: []string{"--brightness"}, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"contrast":            {Reading: Reading{Order: []int{332, 339}, Count: 2}, ThemeKeys: []string{"--contrast"}, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"ease":                {Reading: Reading{Order: []int{354}, Count: 2}, ThemeKeys: []string{"--ease"}, LiteralReadings: map[string]Reading{"linear": Reading{Order: []int{354}, Count: 2}, "initial": Reading{Order: nil, Count: 1}}},
	"grayscale":           {Reading: Reading{Order: []int{334, 339}, Count: 2}, ThemeKeys: []string{"--grayscale"}, DefaultValue: "100%", DefaultValuePresent: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"hue-rotate":          {Reading: Reading{Order: []int{335, 339}, Count: 2}, ThemeKeys: []string{"--hue-rotate"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "deg"},
	"invert":              {Reading: Reading{Order: []int{336, 339}, Count: 2}, ThemeKeys: []string{"--invert"}, DefaultValue: "100%", DefaultValuePresent: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"saturate":            {Reading: Reading{Order: []int{337, 339}, Count: 2}, ThemeKeys: []string{"--saturate"}, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"sepia":               {Reading: Reading{Order: []int{338, 339}, Count: 2}, ThemeKeys: []string{"--sepia"}, DefaultValue: "100%", DefaultValuePresent: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "%"},
	"skew":                {Reading: Reading{Order: []int{69, 70, 71}, Count: 3}, ThemeKeys: []string{"--skew"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "deg"},
	"skew-x":              {Reading: Reading{Order: []int{69, 71}, Count: 2}, ThemeKeys: []string{"--skew"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "deg"},
	"skew-y":              {Reading: Reading{Order: []int{70, 71}, Count: 2}, ThemeKeys: []string{"--skew"}, SupportsNegative: true, BareValue: BareValuePositiveInteger, BareValueSuffix: "deg"},
}
