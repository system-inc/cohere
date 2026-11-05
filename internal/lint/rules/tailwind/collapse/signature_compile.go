// What one class compiles to, for its signature: the half of upstream's createUtilitySignatureCache that
// asks the design system to compile the class (#15swfzq unit 1b).
//
// A signature is only as exact as the nodes it is computed from, and this port does not hold every
// utility's value expression. So ClassSignature answers in three states rather than two: a signature,
// no signature because the class compiles to nothing, or unknown. Unknown is not a failure. It is the
// answer for a class whose compiled CSS this port would have to guess at, and the pass treats it as a
// class that might share any signature, which can only make it bail where upstream rewrites.
//
// # What compiles exactly
//
//   - a framework static whose body is declarations alone, from FrameworkStaticDeclarations;
//   - a root registered through `functionalUtility`, through its emitter at the real resolved value,
//     which is what 1a's deprecatedUtilities already relies on;
//   - a gap root's colour arm, which writes the resolved colour into each of its declarations, and
//     agreed with upstream on every gap root and every theme the differential reads;
//   - a repository `@utility` block, through UtilityEvaluator.Compile;
//   - an arbitrary property, which compiles to its own declaration.
//
// Everything else is unknown: a modifier, any other gap arm, a root both the framework and the
// repository register (upstream compiles both bodies and this compiles one), and a class two of whose
// readings compile. TestSignatureAgreesWithTailwind holds every exact answer to upstream's signature on
// three themes.
package tailwind

import (
	"regexp"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// NewSignatureCompiler prepares a compiler over one design system.
func NewSignatureCompiler(system *LoadedDesignSystem) *SignatureCompiler {
	compiler := &SignatureCompiler{system: system, compileTheme: themeWithoutInline(system.Theme())}
	if system.utility != nil {
		compiler.utilities = &UtilityEvaluator{Theme: compiler.compileTheme, Definitions: system.utility.Definitions}
	}
	return compiler
}

// themeWithoutInline is a copy of a theme with every entry's inline option cleared, upstream's
// temporarilyDisableThemeInline made permanent on a copy rather than patched in and out.
func themeWithoutInline(theme *Theme) *Theme {
	copied := &Theme{
		Prefix:   theme.Prefix,
		values:   make(map[string]themeValue, len(theme.values)),
		keyOrder: theme.keyOrder,
		deadKeys: theme.deadKeys,
	}
	for key, value := range theme.values {
		value.options &^= ThemeOptionInline
		copied.values[key] = value
	}
	return copied
}

// SignatureState is which of the three answers ClassSignature gave.
type SignatureState int

const (
	// SignatureUnknown is a class this port cannot compile exactly.
	SignatureUnknown SignatureState = iota
	// SignatureNone is a class that compiles to nothing, which upstream answers with a unique symbol
	// equal to no other signature.
	SignatureNone
	// SignatureExact is a class whose signature is upstream's, string for string.
	SignatureExact
)

// themeFunctionInvocation is upstream's THEME_FUNCTION_INVOCATION: a call to one of the four CSS
// functions Tailwind substitutes at compile time, the legacy `theme()` among them.
var themeFunctionInvocation = regexp.MustCompile(`--alpha\(|--spacing\(|--theme\(|theme\(`)

// substituteThemeFunctions is upstream's substituteFunctions over a compiled body, which runs before the
// signature is taken: every `--spacing(n)` becomes what the theme makes of it. It answers false for a
// body calling `--alpha()`, `--theme()` or `theme()`, which this port does not substitute, so the
// signature is unknown rather than computed from a call upstream would have replaced. It answers false
// too for a declaration with an empty value, which is how an emitter here spells a value expression it
// did not port, such as a root's named `staticValues`.
func substituteThemeFunctions(nodes []*Node, theme *Theme) bool {
	for _, node := range nodes {
		switch node.Kind {
		case KindDeclaration:
			if node.ValuePresent && node.Value == "" {
				return false
			}
			if node.ValuePresent && themeFunctionInvocation.MatchString(node.Value) {
				value, substituted := substituteSpacingFunction(node.Value, theme)
				if !substituted {
					return false
				}
				node.Value = value
			}
		case KindAtRule:
			switch node.Name {
			case "@media", "@custom-media", "@container", "@supports":
				if themeFunctionInvocation.MatchString(node.Parameters) {
					return false
				}
			}
		}
		if len(node.Nodes) > 0 && !substituteThemeFunctions(node.Nodes, theme) {
			return false
		}
	}
	return true
}

// substituteSpacingFunction is substituteFunctionsInValue for `--spacing()` alone, which is upstream's
// `spacing`: zero becomes `0px`, one becomes the multiplier itself, and anything else
// `calc(<multiplier> * <value>)`. The multiplier is `var(--spacing)` however the theme declared it,
// since the signature compiles with `@theme inline` switched off.
func substituteSpacingFunction(value string, theme *Theme) (string, bool) {
	substituted := true
	var walk func(nodes []ValueNode) []ValueNode
	walk = func(nodes []ValueNode) []ValueNode {
		spliced := nodes
		for index := 0; index < len(spliced); {
			node := &spliced[index]
			if node.Kind == ValueNodeKindFunction {
				switch node.Value {
				case "--alpha", "--theme", "theme":
					substituted = false
				case "--spacing":
					result, resolved := spacingFunction(node.Nodes, theme)
					if !resolved {
						substituted = false
						index++
						continue
					}
					spliced = append(spliced[:index:index], append(ParseValue(result), spliced[index+1:]...)...)
					continue
				}
			}
			if len(node.Nodes) > 0 {
				node.Nodes = walk(node.Nodes)
			}
			index++
		}
		return spliced
	}
	nodes := walk(ParseValue(value))
	return ValueToCss(nodes), substituted
}

// spacingFunction is upstream's `spacing` for one call's arguments, or false where upstream throws.
func spacingFunction(arguments []ValueNode, theme *Theme) (string, bool) {
	parts := segment(text.TrimWhitespace(ValueToCss(arguments)), ',')
	if len(parts) != 1 {
		return "", false
	}
	value := text.TrimWhitespace(parts[0])
	if value == "" {
		return "", false
	}
	multiplier, found := theme.variableReference("--spacing")
	if !found {
		return "", false
	}
	if dimension := parseDimension(value); dimension != nil {
		if dimension.value == 0 {
			return "0px", true
		}
		if dimension.value == 1 {
			return multiplier, true
		}
	}
	return "calc(" + multiplier + " * " + value + ")", true
}

// sortReadingsLikeCompile orders several readings of one class as compileCandidates orders their rules:
// by the first property position where their property sorts differ, then most declarations first. They
// share a class, so the alphabetical tie-break and the variant order never separate them, and a tie
// keeps the parse order, as V8's stable sort does.
func sortReadingsLikeCompile(readings [][]*Node) {
	sorts := make(map[*Node]Sort, len(readings))
	for _, nodes := range readings {
		if len(nodes) > 0 {
			sorts[nodes[0]] = PropertySort(nodes)
		}
	}
	sortOf := func(nodes []*Node) Sort {
		if len(nodes) == 0 {
			return Sort{}
		}
		return sorts[nodes[0]]
	}
	sort.SliceStable(readings, func(left, right int) bool {
		return compareReadingSorts(sortOf(readings[left]), sortOf(readings[right])) < 0
	})
}

// compareReadingSorts is compileCandidates' comparator without its variant and candidate terms. An
// absent position is Infinity upstream, and Infinity minus Infinity is NaN, which falls through to the
// count exactly as a zero does.
func compareReadingSorts(left Sort, right Sort) int {
	offset := 0
	for offset < len(left.Order) && offset < len(right.Order) && left.Order[offset] == right.Order[offset] {
		offset++
	}
	leftHas, rightHas := offset < len(left.Order), offset < len(right.Order)
	switch {
	case leftHas && rightHas:
		if left.Order[offset] != right.Order[offset] {
			return left.Order[offset] - right.Order[offset]
		}
	case leftHas:
		return -1
	case rightHas:
		return 1
	}
	return right.Count - left.Count
}

// prefixedClass puts the theme's prefix in front of a class that lacks it, as upstream's signature
// cache does before parsing.
func prefixedClass(className string, system *LoadedDesignSystem) string {
	prefix := system.Prefix()
	if prefix != "" && !strings.HasPrefix(className, prefix+":") {
		return prefix + ":" + className
	}
	return className
}

// signatureValueUnportedRoots are the roots whose emitter here writes the resolved value where upstream
// wraps it in an expression this port does not hold: the filter functions (`blur(8px)` where the emitter
// writes `8px`), `span n / span n`, the reverse-aware `calc()` chains of space and divide, the degree
// suffix and sign of skew and the mask angles, line-clamp's five declarations and the container's
// breakpoints. Each disagreed with upstream's signature on every theme TestSignatureAgreesWithTailwind
// reads, and every other root agreed, so these answer unknown.
var signatureValueUnportedRoots = map[string]bool{
	"backdrop-blur": true, "backdrop-brightness": true, "backdrop-contrast": true, "backdrop-grayscale": true,
	"backdrop-hue-rotate": true, "backdrop-invert": true, "backdrop-opacity": true, "backdrop-saturate": true,
	"backdrop-sepia": true, "blur": true, "brightness": true, "contrast": true, "grayscale": true,
	"hue-rotate": true, "invert": true, "saturate": true, "sepia": true,
	"col-span": true, "row-span": true,
	"space-x": true, "space-y": true, "divide-x": true, "divide-y": true,
	"skew": true, "skew-x": true, "skew-y": true, "mask-conic": true, "mask-linear": true,
	"line-clamp": true, "container": true, "transition": true,
}

// colourArmNodes compiles a gap root's class when it takes the root's colour arm, and answers unknown
// for every other arm.
//
// The arm is decided the way the emitter decides it, by asking it: a named value resolving through the
// colour arm is a colour, and an arbitrary value is one when it emits what a colour does. The value is
// upstream's resolveThemeColor for a named one and the value as written for an arbitrary one, both with
// no modifier, which readingNodes has already declined.
func colourArmNodes(candidate *ParsedCandidate, root string, theme *Theme) ([]*Node, SignatureState) {
	if candidate.Value == nil {
		return nil, SignatureUnknown
	}
	colourBranch := UtilityBranch{HasValue: true, ResolvedAsColor: true}
	switch candidate.Value.Kind {
	case ParsedValueKindNamed:
		description := descriptionForRoot(root)
		if description == nil || len(description.Arms) == 0 || !description.Arms[0].IsColor {
			return nil, SignatureUnknown
		}
		value, resolved := resolveArmColor(candidate, &description.Arms[0], theme)
		if !resolved {
			// Another arm may take the value, and this port does not compile those. Whether any arm
			// takes it at all is what existence asks, so a value no arm resolves compiles to nothing.
			if _, produced := ResolveFunctionalUtilityValue(
				candidate,
				description,
				theme,
				strings.HasPrefix(candidate.Root, "-"),
			); !produced {
				return nil, SignatureNone
			}
			return nil, SignatureUnknown
		}
		colourBranch.Value = value
	case ParsedValueKindArbitrary:
		// An annotation such as `bg-[size:123px]` names a type the root's own switch reads, under
		// spellings the branch does not map one to one.
		if candidate.Value.DataType != "" {
			return nil, SignatureUnknown
		}
		branch := UtilityBranchFor(candidate, gapTypeListFor(root), false, "")
		if !sameProperties(EmitGapRoot(root, branch), EmitGapRoot(root, colourBranch)) {
			return nil, SignatureUnknown
		}
		colourBranch.Value = candidate.Value.Value
	default:
		return nil, SignatureUnknown
	}
	return EmitGapRoot(root, colourBranch), SignatureExact
}

// sameProperties reports whether two emissions declare the same properties in the same order.
func sameProperties(left []*Node, right []*Node) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	for index := range left {
		if left[index].Kind != KindDeclaration || right[index].Kind != KindDeclaration ||
			left[index].Property != right[index].Property {
			return false
		}
	}
	return true
}

// isColourKeyword reports whether a named value is one resolveThemeColor answers before the theme.
func isColourKeyword(value string) bool {
	return value == "inherit" || value == "transparent" || value == "current"
}

// suggestsFractions reports whether a root's `suggest` offers fractions, which upstream sets exactly
// for the roots registered with `supportsFractions`.
func suggestsFractions(root string) bool {
	for _, group := range FrameworkSuggestions[root] {
		if group.Values.Fractions {
			return true
		}
	}
	return false
}

// colourKeywordValue is what resolveThemeColor makes of a keyword: `current` is `currentcolor`.
func colourKeywordValue(keyword string) string {
	if keyword == "current" {
		return "currentcolor"
	}
	return keyword
}

// suggestsColourKeywords reports whether a root's `suggest` offers the three keywords, which is how
// upstream's colorUtility registers itself.
func suggestsColourKeywords(root string) bool {
	for _, group := range FrameworkSuggestions[root] {
		for _, literal := range group.Values.Literals {
			if literal == "current" {
				return true
			}
		}
	}
	return false
}
