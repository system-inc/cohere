// The ported `handle` bodies of the framework's multi-declaration functional utilities.
//
// The companion to frameworkhandlers.go, covering the 152 roots whose reading was measured rather
// than derived. Same contract: each root's emitter is the Go form of the `handle` body upstream
// registers for it at v4.3.3, and the reading is `PropertySort` over what it emits.
//
// # Why these were a measured reading and the other 33 were a property name
//
// A single-declaration root is fully described by its property. These are not. Three things break
// that, and all three are why frameworkmultideclaration.go carries `{order, count}` instead:
//
// A declaration whose property is outside `PropertyOrder` counts and contributes no position, so
// `ease` reads one position at count two. A `--tw-sort` whose value names a known property latches,
// so every declaration after it counts and contributes nothing, which is how `divide-y` reads one
// position at count six. And a handler branches, so `transition` emits three declarations for a
// value and one for `transition-none`.
//
// Emitting declarations reproduces all three without any of them being special-cased here.
// `PropertySort` already implements the latch, already skips unknown properties when collecting
// positions, and already counts what it skips.
//
// # The wrapper helpers are not ported, and the count is 21 rather than ten
//
// Every one of these bodies opens with a properties wrapper: `filterProperties()`,
// `transformProperties()`, `maskPropertiesRadial()`. Measured at the tag by walking each
// `let <name>Properties = () =>` definition: there are 21 of them, every one returns `atRoot(...)`,
// and not one contains a `decl(` call. `PropertySort` does not descend into at-root, so all 21 are
// structurally invisible and none needs a representation here.
//
// The milestone body said ten. That was an undercount rather than a different definition, and it
// does not change the conclusion.
//
// # Where the roots come from
//
// 147 are registered in `utilities.ts`, some directly and some by a loop over a table of name and
// property pairs. Five are not there at all: `flex-grow`, `flex-shrink`, `start`, `end` and
// `max-w-screen` are registered by `compat/legacy-utilities.ts`, which the earlier scoping did not
// mention. Each emits a single declaration, and each was read at the tag like the rest rather than
// inferred from its measured reading.
package tailwind

// declareProperties is the emitter for a root whose handle body emits a fixed declaration list.
//
// The properties are given in the order the body emits them, which is not the order the reading
// reports: `PropertySort` sorts positions numerically, so `scale-x` emits `--tw-scale-x` then
// `scale` and reads `[61 62]`. Keeping source order here means a reader can check this against
// `utilities.ts` line by line, and the sorting stays the one place that does the sorting.
//
// Every declaration carries the resolved value, including the ones whose upstream value is a
// computed expression such as `skewX(${value})` or a shared constant such as `cssFilterValue`. The
// value is not read by either consumer, so reproducing the expressions would add 374 sites of
// arithmetic that nothing checks and that could drift from upstream silently.
func declareProperties(properties ...string) FrameworkEmitter {
	return func(resolved ResolvedUtilityValue) []*Node {
		nodes := make([]*Node, 0, len(properties))
		for _, property := range properties {
			nodes = append(nodes, Declaration(property, resolved.Value))
		}
		return nodes
	}
}

// declareSorted is declareProperties for a root that opens with a `--tw-sort` declaration.
//
// sortValue is the property name the root sorts at, and it is carried as the declaration's value
// because that is what `PropertySort` reads: a `--tw-sort` whose value is a known property latches
// the order at that property's position, and everything after it counts without contributing.
//
// The distinction between a latching and a non-latching sort is not encoded here, deliberately.
// `size` writes `--tw-sort: size`, which is not a CSS property, so it does not latch and `width` and
// `height` contribute their own positions. That falls out of `PropertyOrder` not knowing `size`
// rather than out of a flag, which is the same way the engine decides it.
func declareSorted(sortValue string, properties ...string) FrameworkEmitter {
	return func(resolved ResolvedUtilityValue) []*Node {
		nodes := make([]*Node, 0, len(properties)+1)
		nodes = append(nodes, Declaration("--tw-sort", sortValue))
		for _, property := range properties {
			nodes = append(nodes, Declaration(property, resolved.Value))
		}
		return nodes
	}
}

// frameworkMultiEmitters is the ported body of each multi-declaration root.
var frameworkMultiEmitters = map[string]FrameworkEmitter{
	"accent":           declareProperty("accent-color"),
	"aspect":           declareProperty("aspect-ratio"),
	"auto-cols":        declareProperty("grid-auto-columns"),
	"auto-rows":        declareProperty("grid-auto-rows"),
	"basis":            declareProperty("flex-basis"),
	"bottom":           declareProperty("bottom"),
	"caret":            declareProperty("caret-color"),
	"col-span":         declareProperty("grid-column"),
	"end":              declareProperty("inset-inline-end"),
	"flex-grow":        declareProperty("flex-grow"),
	"flex-shrink":      declareProperty("flex-shrink"),
	"font-stretch":     declareProperty("font-stretch"),
	"gap":              declareProperty("gap"),
	"gap-x":            declareProperty("column-gap"),
	"gap-y":            declareProperty("row-gap"),
	"grid-cols":        declareProperty("grid-template-columns"),
	"grid-rows":        declareProperty("grid-template-rows"),
	"h":                declareProperty("height"),
	"indent":           declareProperty("text-indent"),
	"inset":            declareProperty("inset"),
	"inset-be":         declareProperty("inset-block-end"),
	"inset-bs":         declareProperty("inset-block-start"),
	"inset-e":          declareProperty("inset-inline-end"),
	"inset-s":          declareProperty("inset-inline-start"),
	"inset-x":          declareProperty("inset-inline"),
	"inset-y":          declareProperty("inset-block"),
	"left":             declareProperty("left"),
	"m":                declareProperty("margin"),
	"max-block":        declareProperty("max-block-size"),
	"max-h":            declareProperty("max-height"),
	"max-inline":       declareProperty("max-inline-size"),
	"max-w":            declareProperty("max-width"),
	"max-w-screen":     declareProperty("max-width"),
	"mb":               declareProperty("margin-bottom"),
	"mbe":              declareProperty("margin-block-end"),
	"mbs":              declareProperty("margin-block-start"),
	"me":               declareProperty("margin-inline-end"),
	"min-block":        declareProperty("min-block-size"),
	"min-h":            declareProperty("min-height"),
	"min-inline":       declareProperty("min-inline-size"),
	"min-w":            declareProperty("min-width"),
	"ml":               declareProperty("margin-left"),
	"mr":               declareProperty("margin-right"),
	"ms":               declareProperty("margin-inline-start"),
	"mt":               declareProperty("margin-top"),
	"mx":               declareProperty("margin-inline"),
	"my":               declareProperty("margin-block"),
	"p":                declareProperty("padding"),
	"pb":               declareProperty("padding-bottom"),
	"pbe":              declareProperty("padding-block-end"),
	"pbs":              declareProperty("padding-block-start"),
	"pe":               declareProperty("padding-inline-end"),
	"pl":               declareProperty("padding-left"),
	"pr":               declareProperty("padding-right"),
	"ps":               declareProperty("padding-inline-start"),
	"pt":               declareProperty("padding-top"),
	"px":               declareProperty("padding-inline"),
	"py":               declareProperty("padding-block"),
	"right":            declareProperty("right"),
	"row-span":         declareProperty("grid-row"),
	"scroll-m":         declareProperty("scroll-margin"),
	"scroll-mb":        declareProperty("scroll-margin-bottom"),
	"scroll-mbe":       declareProperty("scroll-margin-block-end"),
	"scroll-mbs":       declareProperty("scroll-margin-block-start"),
	"scroll-me":        declareProperty("scroll-margin-inline-end"),
	"scroll-ml":        declareProperty("scroll-margin-left"),
	"scroll-mr":        declareProperty("scroll-margin-right"),
	"scroll-ms":        declareProperty("scroll-margin-inline-start"),
	"scroll-mt":        declareProperty("scroll-margin-top"),
	"scroll-mx":        declareProperty("scroll-margin-inline"),
	"scroll-my":        declareProperty("scroll-margin-block"),
	"scroll-p":         declareProperty("scroll-padding"),
	"scroll-pb":        declareProperty("scroll-padding-bottom"),
	"scroll-pbe":       declareProperty("scroll-padding-block-end"),
	"scroll-pbs":       declareProperty("scroll-padding-block-start"),
	"scroll-pe":        declareProperty("scroll-padding-inline-end"),
	"scroll-pl":        declareProperty("scroll-padding-left"),
	"scroll-pr":        declareProperty("scroll-padding-right"),
	"scroll-ps":        declareProperty("scroll-padding-inline-start"),
	"scroll-pt":        declareProperty("scroll-padding-top"),
	"scroll-px":        declareProperty("scroll-padding-inline"),
	"scroll-py":        declareProperty("scroll-padding-block"),
	"start":            declareProperty("inset-inline-start"),
	"top":              declareProperty("top"),
	"underline-offset": declareProperty("text-underline-offset"),
	"w":                declareProperty("width"),

	// The filter family. Each wraps `filterProperties()`, which is atRoot and contributes nothing,
	// then declares its own `--tw-*` and the shared `filter`. Two visible declarations, and the
	// `--tw-*` one is in PropertyOrder so both carry a position.
	"blur":       declareProperties("--tw-blur", "filter"),
	"brightness": declareProperties("--tw-brightness", "filter"),
	"contrast":   declareProperties("--tw-contrast", "filter"),
	"grayscale":  declareProperties("--tw-grayscale", "filter"),
	"hue-rotate": declareProperties("--tw-hue-rotate", "filter"),
	"invert":     declareProperties("--tw-invert", "filter"),
	"saturate":   declareProperties("--tw-saturate", "filter"),
	"sepia":      declareProperties("--tw-sepia", "filter"),

	// The backdrop-filter family emits three, not two. `-webkit-backdrop-filter` is a real
	// declaration that PropertyOrder does not know, so it counts and contributes no position, which
	// is why these read two positions at count three where the plain filters read two at two.
	"backdrop-blur":       declareProperties("--tw-backdrop-blur", "-webkit-backdrop-filter", "backdrop-filter"),
	"backdrop-brightness": declareProperties("--tw-backdrop-brightness", "-webkit-backdrop-filter", "backdrop-filter"),
	"backdrop-contrast":   declareProperties("--tw-backdrop-contrast", "-webkit-backdrop-filter", "backdrop-filter"),
	"backdrop-grayscale":  declareProperties("--tw-backdrop-grayscale", "-webkit-backdrop-filter", "backdrop-filter"),
	"backdrop-hue-rotate": declareProperties("--tw-backdrop-hue-rotate", "-webkit-backdrop-filter", "backdrop-filter"),
	"backdrop-invert":     declareProperties("--tw-backdrop-invert", "-webkit-backdrop-filter", "backdrop-filter"),
	"backdrop-opacity":    declareProperties("--tw-backdrop-opacity", "-webkit-backdrop-filter", "backdrop-filter"),
	"backdrop-saturate":   declareProperties("--tw-backdrop-saturate", "-webkit-backdrop-filter", "backdrop-filter"),
	"backdrop-sepia":      declareProperties("--tw-backdrop-sepia", "-webkit-backdrop-filter", "backdrop-filter"),

	// The transform family. `transformProperties()` is atRoot, so only the `--tw-*` axes and the
	// shared `transform` are visible.
	"skew":     declareProperties("--tw-skew-x", "--tw-skew-y", "transform"),
	"skew-x":   declareProperties("--tw-skew-x", "transform"),
	"skew-y":   declareProperties("--tw-skew-y", "transform"),
	"rotate-x": declareProperties("--tw-rotate-x", "transform"),
	"rotate-y": declareProperties("--tw-rotate-y", "transform"),
	"rotate-z": declareProperties("--tw-rotate-z", "transform"),

	// Scale and translate declare the shared shorthand last in source order, and it sorts first
	// because PropertySort sorts positions rather than preserving visit order. `scale` is 61 and
	// `--tw-scale-x` is 62, so `scale-x` reads [61 62] from a list that emitted them the other way.
	"scale-x":     declareProperties("--tw-scale-x", "scale"),
	"scale-y":     declareProperties("--tw-scale-y", "scale"),
	"scale-z":     declareProperties("--tw-scale-z", "scale"),
	"translate":   declareProperties("--tw-translate-x", "--tw-translate-y", "translate"),
	"translate-x": declareProperties("--tw-translate-x", "translate"),
	"translate-y": declareProperties("--tw-translate-y", "translate"),
	"translate-z": declareProperties("--tw-translate-z", "translate"),

	// The border-radius family maps a property list, so a corner root emits one and an edge root
	// emits two. Source order is the list's order; the reading sorts it.
	"rounded":    declareProperties("border-radius"),
	"rounded-s":  declareProperties("border-start-start-radius", "border-end-start-radius"),
	"rounded-e":  declareProperties("border-start-end-radius", "border-end-end-radius"),
	"rounded-t":  declareProperties("border-top-left-radius", "border-top-right-radius"),
	"rounded-r":  declareProperties("border-top-right-radius", "border-bottom-right-radius"),
	"rounded-b":  declareProperties("border-bottom-right-radius", "border-bottom-left-radius"),
	"rounded-l":  declareProperties("border-top-left-radius", "border-bottom-left-radius"),
	"rounded-ss": declareProperties("border-start-start-radius"),
	"rounded-se": declareProperties("border-start-end-radius"),
	"rounded-ee": declareProperties("border-end-end-radius"),
	"rounded-es": declareProperties("border-end-start-radius"),
	"rounded-tl": declareProperties("border-top-left-radius"),
	"rounded-tr": declareProperties("border-top-right-radius"),
	"rounded-br": declareProperties("border-bottom-right-radius"),
	"rounded-bl": declareProperties("border-bottom-left-radius"),

	// A `--tw-*` variable plus its consumer, where the variable is outside PropertyOrder. Two
	// declarations at one position, which is the shape `ease` proved a property-name table cannot
	// express.
	"content":         declareProperties("--tw-content", "content"),
	"ease":            declareProperties("--tw-ease", "transition-timing-function"),
	"leading":         declareProperties("--tw-leading", "line-height"),
	"tracking":        declareProperties("--tw-tracking", "letter-spacing"),
	"scrollbar-thumb": declareProperties("--tw-scrollbar-thumb", "scrollbar-color"),
	"scrollbar-track": declareProperties("--tw-scrollbar-track", "scrollbar-color"),

	// The border-spacing family writes both axes and then the shorthand, or one axis and the
	// shorthand. Both `--tw-border-spacing-*` are outside PropertyOrder, so all three read [55].
	"border-spacing":   declareProperties("--tw-border-spacing-x", "--tw-border-spacing-y", "border-spacing"),
	"border-spacing-x": declareProperties("--tw-border-spacing-x", "border-spacing"),
	"border-spacing-y": declareProperties("--tw-border-spacing-y", "border-spacing"),

	// The mask gradients. Each wraps two atRoot helpers and then declares four visible ones.
	"mask-linear": declareProperties("mask-image", "mask-composite", "--tw-mask-linear", "--tw-mask-linear-position"),
	"mask-radial": declareProperties("mask-image", "mask-composite", "--tw-mask-radial", "--tw-mask-radial-size"),
	"mask-conic":  declareProperties("mask-image", "mask-composite", "--tw-mask-conic", "--tw-mask-conic-position"),

	// `size` writes an unlatching `--tw-sort`. `size` is not a CSS property, so PropertyOrder does
	// not know it, the latch does not engage, and the declaration still counts. Three declarations,
	// two positions.
	"size": declareSorted("size", "width", "height"),

	// `line-clamp` emits four, two of which are `-webkit-*` properties outside PropertyOrder.
	"line-clamp": declareProperties("overflow", "display", "-webkit-box-orient", "-webkit-line-clamp"),

	// The six roots whose `--tw-sort` latches, and the reason the value has to be carried.
	//
	// Each emits its declarations inside a `styleRule`, which PropertySort descends into, led by a
	// `--tw-sort` naming a property that is not any of them. PropertyOrder knows every one of these
	// names, so the latch engages on the first declaration and no later one contributes a position.
	// Count keeps rising regardless, which is how `divide-y` reads one position at count six.
	//
	// The declarations after the latch are still emitted rather than elided. They cannot change
	// Order, but each one is a declaration the engine counts, and Count is half the sort key.
	"divide":   declareSorted("divide-color", "border-color"),
	"divide-x": declareSorted("divide-x-width", "--tw-divide-x-reverse", "border-inline-style", "border-inline-start-width", "border-inline-end-width"),
	"divide-y": declareSorted("divide-y-width", "--tw-divide-y-reverse", "border-bottom-style", "border-top-style", "border-top-width", "border-bottom-width"),
	"space-x":  declareSorted("row-gap", "--tw-space-x-reverse", "margin-inline-start", "margin-inline-end"),
	"space-y":  declareSorted("column-gap", "--tw-space-y-reverse", "margin-block-start", "margin-block-end"),

	// `placeholder` sorts at `placeholder-color` and declares plain `color` inside `&::placeholder`.
	// Without the redirect it would read at 297 with the rest of the colour utilities.
	"placeholder": declareSorted("placeholder-color", "color"),

	// `transition` is the branching root. Its ordinary path and five of its six static values emit
	// three declarations; `transition-none` emits one. There is no single list for it, so the branch
	// follows from the resolved value the way upstream's does.
	"transition": declareProperties("transition-property", "transition-timing-function", "transition-duration"),
}

// frameworkMultiLiteralEmitters are the root-defined keywords whose declaration list differs from
// their root's ordinary path.
//
// Upstream these are `staticValues` entries, answered before the handle body runs. Most agree with
// the ordinary path and need no entry; four do not, and each is a real branch in the source rather
// than a quirk of the measurement:
//
//	divide-none      a separate staticUtility that sorts at divide-style and writes three
//	ease-initial     omits transition-timing-function, leaving only the --tw-ease variable
//	transition-none  emits transition-property alone
//	translate-none   emits the translate shorthand alone
//
// Keyed by root and then by literal, so a literal that shares its root's list stays absent rather
// than being restated. TestMultiDeclarationLiteralsAreOnlyListedWhenTheyDiffer holds that line.
var frameworkMultiLiteralEmitters = map[string]map[string]FrameworkEmitter{
	"divide": {
		"none": declareSorted("divide-style", "--tw-border-style", "border-style"),
	},
	"ease": {
		"initial": declareProperties("--tw-ease"),
	},
	"transition": {
		"none": declareProperties("transition-property"),
	},
	"translate": {
		"none": declareProperties("translate"),
	},
}

// Emit returns the declarations this root's handle body produces for a resolved value.
//
// The literal branch runs first and on the candidate's own value rather than on the resolved one,
// matching ReadingFor and matching upstream: a root-defined keyword is answered by the handler
// directly and never reaches the theme. Checking after resolution would let a repository declaring
// `--ease-initial` shadow the literal, which the engine does not allow.
//
// Returns nil for a root with no ported emitter, for the same reason the single-declaration Emit
// does: a fallback would leave the acceptance test comparing a table against itself.
func (utility FrameworkMultiDeclarationUtility) Emit(root string, literal string, resolved ResolvedUtilityValue) []*Node {
	if literal != "" {
		if byLiteral, found := frameworkMultiLiteralEmitters[root]; found {
			if emitter, found := byLiteral[literal]; found {
				return emitter(resolved)
			}
		}
	}
	emitter, ported := frameworkMultiEmitters[root]
	if !ported {
		return nil
	}
	return emitter(resolved)
}
