// The value-resolution descriptions of the framework roots registered as bare closures.
//
// frameworkgaphandlers.go holds what these 57 roots emit. This holds how they decide a value
// resolves at all, which is a different question and the one `no-unknown-classes` asks: `text-lg` is
// a class and `text-ss` is a typo, and nothing in a declaration list separates them.
//
// # Why these are not in the other two tables
//
// `FrameworkFunctionalUtilities` and `FrameworkMultiDeclarationUtilities` describe roots upstream
// registers through `functionalUtility`, which takes a declarative description: one namespace list,
// one bare-value handler, one default. 185 roots fit that and are fully answered by it.
//
// These 57 are registered as `utilities.functional(root, candidate => {...})`, a bare closure, and a
// closure can do things a description could not express until `Arms` existed: consult several
// namespace lists in sequence, infer a type instead of reading the theme, and accept a modifier on
// one path while refusing it on another.
//
// # Read at the tag, verified against the engine
//
// Every entry was read from `utilities.ts` at the v4.3.3 tag rather than inferred from the descriptor
// rows, which cannot supply it: a row records only the namespaces whose reading differs, so
// `outline`'s row carries `--color` and not `--outline-width`, and a description built from rows
// reports `outline-2` as unknown.
//
// Each family was then scored against `candidatesToCss` on a live design system, which is the engine
// answering whether a class compiles. The measurements are in gaproots_test.go, per family, and the
// pairs that carry them are the ones a careful reader gets wrong: `border-2/50` against
// `border-red-500/50`, `stroke-2/50` against both, `mask-x-from-1.5` against `mask-x-from-3.7`.
package tailwind

// colorArm is the `resolveThemeColor(candidate, theme, keys)` block these closures share.
func colorArm(keys ...string) FunctionalUtilityArm {
	return FunctionalUtilityArm{ThemeKeys: keys, IsColor: true}
}

// themeArm is a block that reads one namespace and stops there.
func themeArm(key string) FunctionalUtilityArm {
	return FunctionalUtilityArm{ThemeKeys: []string{key}}
}

// widthArm is a block that reads one namespace and falls back to a bare positive integer.
//
// The suffix and the modifier guard are per-root rather than per-shape, which is the detail this
// family is easiest to get wrong on. `border`, `outline` and `ring` append `px` and refuse a
// modifier; `stroke` does neither, so `stroke-2` resolves to `2` and `stroke-2/50` compiles where
// `border-2/50` does not.
func widthArm(key string, suffix string, refusesModifier bool) FunctionalUtilityArm {
	return FunctionalUtilityArm{
		ThemeKeys:       []string{key},
		BareValue:       BareValuePositiveInteger,
		BareValueSuffix: suffix,
		RefusesModifier: refusesModifier,
	}
}

// borderSideDescription is `borderSideUtility`, utilities.ts:2432, shared by eleven roots.
//
// One description rather than eleven, because upstream registers all eleven through one function and
// the sides differ only in which declarations they emit, which is frameworkgaphandlers.go's half.
func borderSideDescription() *FunctionalUtilityDescription {
	return &FunctionalUtilityDescription{
		DefaultValue:        "1px",
		DefaultValuePresent: true,
		Arms: []FunctionalUtilityArm{
			colorArm("--border-color", "--color"),
			widthArm("--border-width", "px", true),
		},
	}
}

// maskStopDescription is `maskStopUtility`, utilities.ts:3242, shared by eighteen roots.
//
// The second arm infers rather than reading the theme, and its `--spacing` entry is a precondition
// rather than a lookup: upstream refuses the class when the theme declares no `--spacing` at all.
func maskStopDescription() *FunctionalUtilityDescription {
	return &FunctionalUtilityDescription{
		Arms: []FunctionalUtilityArm{
			colorArm("--background-color", "--color"),
			{
				ThemeKeys:               []string{"--spacing"},
				InferTypes:              []DataType{DataTypeNumber, DataTypePercentage},
				PercentagePassesThrough: true,
				BareValue:               BareValueSpacingMultiplier,
				RefusesModifier:         true,
			},
		},
	}
}

// GapRootDescriptions is the resolution description of each root registered as a bare closure.
//
// Keyed on the undashed root, matching the emitters. A root absent here is one no description was
// read for, and a caller must decline rather than guess: answering "this value does not resolve" for
// a root nobody described would report working classes.
var GapRootDescriptions = map[string]*FunctionalUtilityDescription{
	// The eleven `borderSideUtility` registrations, utilities.ts:2505 through 2585.
	"border":    borderSideDescription(),
	"border-x":  borderSideDescription(),
	"border-y":  borderSideDescription(),
	"border-s":  borderSideDescription(),
	"border-e":  borderSideDescription(),
	"border-bs": borderSideDescription(),
	"border-be": borderSideDescription(),
	"border-t":  borderSideDescription(),
	"border-r":  borderSideDescription(),
	"border-b":  borderSideDescription(),
	"border-l":  borderSideDescription(),

	// The twelve `maskEdgeUtility` registrations and the six `maskStopUtility` ones, which share one
	// resolution body through `maskStopUtility`.
	"mask-x-from":      maskStopDescription(),
	"mask-x-to":        maskStopDescription(),
	"mask-y-from":      maskStopDescription(),
	"mask-y-to":        maskStopDescription(),
	"mask-t-from":      maskStopDescription(),
	"mask-t-to":        maskStopDescription(),
	"mask-r-from":      maskStopDescription(),
	"mask-r-to":        maskStopDescription(),
	"mask-b-from":      maskStopDescription(),
	"mask-b-to":        maskStopDescription(),
	"mask-l-from":      maskStopDescription(),
	"mask-l-to":        maskStopDescription(),
	"mask-linear-from": maskStopDescription(),
	"mask-linear-to":   maskStopDescription(),
	"mask-radial-from": maskStopDescription(),
	"mask-radial-to":   maskStopDescription(),
	"mask-conic-from":  maskStopDescription(),
	"mask-conic-to":    maskStopDescription(),

	// The colour-then-width closures. Each was read separately rather than assumed to match
	// `borderSideUtility`, and two of them do not: `decoration` puts its width arm first, and
	// `stroke` neither appends `px` nor refuses a modifier.
	"outline": {
		DefaultValue: "1px", DefaultValuePresent: true,
		Arms: []FunctionalUtilityArm{
			colorArm("--outline-color", "--color"),
			widthArm("--outline-width", "px", true),
		},
	},
	"ring": {
		DefaultValue: "1px", DefaultValuePresent: true,
		Arms: []FunctionalUtilityArm{
			colorArm("--ring-color", "--color"),
			widthArm("--ring-width", "px", true),
		},
	},
	"inset-ring": {
		DefaultValue: "1px", DefaultValuePresent: true,
		Arms: []FunctionalUtilityArm{
			colorArm("--inset-ring-color", "--color"),
			widthArm("--inset-ring-width", "px", true),
		},
	},
	"stroke": {
		Arms: []FunctionalUtilityArm{
			colorArm("--stroke", "--color"),
			widthArm("--stroke-width", "", false),
		},
	},
	// `decoration` puts its width arm first upstream and the order is reproduced rather than
	// normalised, though nothing here would catch it if it were wrong: measured by swapping the two
	// arms, every case still agrees, because no value is both a `--text-decoration-thickness` key
	// and a colour. The order is kept because it is what the source says and a future value that is
	// both would resolve on the wrong arm silently.
	"decoration": {
		Arms: []FunctionalUtilityArm{
			widthArm("--text-decoration-thickness", "px", true),
			colorArm("--text-decoration-color", "--color"),
		},
	},
	"fill": {
		Arms: []FunctionalUtilityArm{colorArm("--fill", "--color")},
	},

	// The colour-then-scale closures. A named value is a colour first and a size second, which is
	// what separates `text-red-500` from `text-lg` and `shadow-red-500` from `shadow-lg`.
	"text": {
		Arms: []FunctionalUtilityArm{colorArm("--text-color", "--color"), themeArm("--text")},
	},
	"bg": {
		Arms: []FunctionalUtilityArm{colorArm("--background-color", "--color"), themeArm("--background-image")},
	},
	"font": {
		Arms: []FunctionalUtilityArm{themeArm("--font"), themeArm("--font-weight")},
	},

	// The shadow family, whose size arm reads its own namespace before the colour arm runs.
	// `none` and `inherit` are answered by the closure directly, which is what `StaticValueNames`
	// carries here: upstream spells them as `case` labels rather than as theme keys.
	"shadow": {
		DefaultValue: "var(--shadow)", DefaultValuePresent: true,
		Arms:             []FunctionalUtilityArm{themeArm("--shadow"), colorArm("--box-shadow-color", "--color")},
		StaticValueNames: map[string]bool{"none": true, "inherit": true},
	},
	"inset-shadow": {
		Arms:             []FunctionalUtilityArm{themeArm("--inset-shadow"), colorArm("--inset-shadow-color", "--color")},
		StaticValueNames: map[string]bool{"none": true, "inherit": true},
	},
	"text-shadow": {
		Arms:             []FunctionalUtilityArm{themeArm("--text-shadow"), colorArm("--text-shadow-color", "--color")},
		StaticValueNames: map[string]bool{"none": true, "inherit": true},
	},
	"drop-shadow": {
		DefaultValue: "var(--drop-shadow)", DefaultValuePresent: true,
		Arms:             []FunctionalUtilityArm{themeArm("--drop-shadow"), colorArm("--drop-shadow-color", "--color")},
		StaticValueNames: map[string]bool{"none": true},
	},

	// `duration` resolves through one namespace and appends `ms` to a bare integer, and answers
	// `initial` directly.
	"duration": {
		Arms:             []FunctionalUtilityArm{{ThemeKeys: []string{"--transition-duration"}, BareValue: BareValuePositiveInteger, BareValueSuffix: "ms"}},
		StaticValueNames: map[string]bool{"initial": true},
	},

	// The gradient roots, which carry a default and resolve an angle as a bare integer.
	"bg-conic": {
		DefaultValue: "in oklab", DefaultValuePresent: true,
		Arms: []FunctionalUtilityArm{{ThemeKeys: []string{"--conic"}, BareValue: BareValuePositiveInteger}},
	},
	"bg-radial": {DefaultValue: "in oklab", DefaultValuePresent: true},

	// The arbitrary-only closures. Each accepts a bracketed value or one named keyword and consults
	// no namespace at all, so a named value that is not the keyword resolves nowhere:
	// `filter-nonsense` and `mask-nonsense` compile to nothing while `filter-none` compiles.
	//
	// `mask` is the one that also refuses the bare root, because its body opens with
	// `if (!candidate.value) return` where `filter` and `transform` fall through to a default.
	"filter": {
		DefaultValue: "none", DefaultValuePresent: true,
		StaticValueNames: map[string]bool{"none": true},
	},
	"transform": {
		DefaultValue: "none", DefaultValuePresent: true,
		StaticValueNames: map[string]bool{"none": true},
	},
	"backdrop-filter": {
		DefaultValue: "none", DefaultValuePresent: true,
		StaticValueNames: map[string]bool{"none": true},
	},
	"mask": {
		StaticValueNames: map[string]bool{"none": true},
	},

	// `block` and `inline` are `staticUtility` registrations that the descriptor extractor probed as
	// functional roots, which `TestGapRootExemptionsAreExactlyTheStaticCollision` already records.
	// They take no value, so their description is empty and the emitter answers the static form.
	"block":  {},
	"inline": {},
	"flex":   {},

	// The negative spellings. Upstream registers each as a second root whose resolved value is
	// negated, and the description is the positive root's with the flag set, which is what
	// `SupportsNegative` means everywhere else in this port.
	"-scale":    {SupportsNegative: true, Arms: []FunctionalUtilityArm{{ThemeKeys: []string{"--scale"}, BareValue: BareValuePositiveInteger}}},
	"scale":     {Arms: []FunctionalUtilityArm{{ThemeKeys: []string{"--scale"}, BareValue: BareValuePositiveInteger}}},
	"-rotate":   {SupportsNegative: true, Arms: []FunctionalUtilityArm{{ThemeKeys: []string{"--rotate"}, BareValue: BareValuePositiveInteger, BareValueSuffix: "deg"}}},
	"rotate":    {Arms: []FunctionalUtilityArm{{ThemeKeys: []string{"--rotate"}, BareValue: BareValuePositiveInteger, BareValueSuffix: "deg"}}},
	"-bg-conic": {SupportsNegative: true, DefaultValue: "in oklab", DefaultValuePresent: true, Arms: []FunctionalUtilityArm{{ThemeKeys: []string{"--conic"}, BareValue: BareValuePositiveInteger}}},
}
