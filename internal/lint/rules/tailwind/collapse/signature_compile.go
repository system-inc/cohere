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
	"strings"

	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// SignatureCompiler compiles classes for their signatures over one design system.
//
// Upstream compiles a signature's body with `@theme inline` switched off, so a theme key resolves to
// `var(--key)` whether or not it was declared inline, and only canonicalizing inlines it again. That is
// what keeps `max-w-(--breakpoint-md)` from matching `max-w-3xl` because the two keys happen to hold one
// value. compileTheme is the run's theme with that flag cleared, and the repository's `@utility` blocks
// evaluate against it too. Built once per design system, since the clone is the size of the theme.
type SignatureCompiler struct {
	system       *LoadedDesignSystem
	compileTheme *Theme
	utilities    *UtilityEvaluator
}

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

// ClassSignature is the signature of one class without variants or importance, under the given features.
func (compiler *SignatureCompiler) ClassSignature(className string, features SignatureFeatures) (string, SignatureState) {
	system := compiler.system
	nodes, state := compiler.signatureNodes(className)
	if state != SignatureExact {
		return "", state
	}
	if !substituteThemeFunctions(nodes, compiler.compileTheme) {
		return "", SignatureUnknown
	}
	signature, exact := UtilitySignature(nodes, system.Theme(), features)
	if !exact {
		return "", SignatureUnknown
	}
	return signature, SignatureExact
}

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
				if themeFunctionInvocation.MatchString(node.Params) {
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
		for index := 0; index < len(nodes); {
			node := &nodes[index]
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
					nodes = append(nodes[:index:index], append(ParseValue(result), nodes[index+1:]...)...)
					continue
				}
			}
			if len(node.Nodes) > 0 {
				node.Nodes = walk(node.Nodes)
			}
			index++
		}
		return nodes
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

// signatureNodes is the body upstream's `@apply` would substitute for the class: every reading that
// compiles, concatenated. Exact only when at most one reading compiles and this port compiles it.
func (compiler *SignatureCompiler) signatureNodes(className string) ([]*Node, SignatureState) {
	system := compiler.system
	var compiled []*Node
	compiledReadings := 0
	for _, candidate := range ParseCandidate(prefixedClass(className, system), system) {
		if len(candidate.Variants) > 0 || candidate.Important {
			return nil, SignatureUnknown
		}
		nodes, state := compiler.readingNodes(&candidate)
		switch state {
		case SignatureUnknown:
			return nil, SignatureUnknown
		case SignatureExact:
			compiled = nodes
			compiledReadings++
		}
	}
	switch compiledReadings {
	case 0:
		return nil, SignatureNone
	case 1:
		return compiled, SignatureExact
	default:
		// Upstream concatenates every reading's body; two readings both compiling are rare enough
		// that this declines rather than reproducing the order they would be emitted in.
		return nil, SignatureUnknown
	}
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

// readingNodes compiles one reading of a class.
func (compiler *SignatureCompiler) readingNodes(candidate *ParsedCandidate) ([]*Node, SignatureState) {
	system := compiler.system
	if candidate.Modifier != nil {
		return nil, SignatureUnknown
	}
	switch candidate.Kind {
	case ParsedCandidateKindArbitrary:
		return []*Node{Declaration(candidate.Property, candidate.PropertyValue)}, SignatureExact

	case ParsedCandidateKindStatic:
		declaredByRepository := system.utilityRoots[candidate.Root][UtilityKindStatic]
		frameworkDeclarations, isFramework := FrameworkStaticDeclarations[candidate.Root]
		_, isRegisteredFramework := FrameworkStaticSuggested[candidate.Root]
		switch {
		case declaredByRepository && isRegisteredFramework:
			return nil, SignatureUnknown
		case declaredByRepository:
			return cloneNodes(system.staticUtilityNodes[candidate.Root]), SignatureExact
		case isFramework && FrameworkStaticNested[candidate.Root]:
			return nil, SignatureUnknown
		case isFramework:
			nodes := make([]*Node, 0, len(frameworkDeclarations))
			for _, declaration := range frameworkDeclarations {
				nodes = append(nodes, &Node{
					Kind:         KindDeclaration,
					Property:     declaration.Property,
					Value:        declaration.Value,
					ValuePresent: declaration.ValuePresent,
					Important:    declaration.Important,
				})
			}
			return nodes, SignatureExact
		case isRegisteredFramework:
			// A static whose body reads the theme, left out of FrameworkStaticDeclarations.
			return nil, SignatureUnknown
		default:
			return nil, SignatureNone
		}

	case ParsedCandidateKindFunctional:
		return compiler.functionalReadingNodes(candidate)
	}
	return nil, SignatureUnknown
}

// functionalReadingNodes compiles one functional reading.
func (compiler *SignatureCompiler) functionalReadingNodes(candidate *ParsedCandidate) ([]*Node, SignatureState) {
	system := compiler.system
	root := strings.TrimPrefix(candidate.Root, "-")
	negative := root != candidate.Root
	declaredByRepository := system.DeclaresFunctionalUtility(candidate.Root)
	registeredByFramework := frameworkFunctionalRoots[candidate.Root]

	if declaredByRepository {
		if registeredByFramework {
			return nil, SignatureUnknown
		}
		nodes, compiles := compiler.utilities.Compile(candidate)
		if !compiles {
			return nil, SignatureNone
		}
		return nodes, SignatureExact
	}
	if !registeredByFramework {
		return nil, SignatureNone
	}

	theme := compiler.compileTheme
	if _, isGapRoot := gapEmitters[root]; isGapRoot {
		return colourArmNodes(candidate, root, theme)
	}
	// A bare root takes the registration's default, which the description carries as a literal where
	// upstream reads a theme key, so it is not a value expression this port holds. A named
	// `staticValues` entry reaches the emitter with no value at all and is caught by
	// substituteThemeFunctions's empty-value check.
	if candidate.Value == nil || signatureValueUnportedRoots[root] {
		return nil, SignatureUnknown
	}
	// The three colour keywords are answered by resolveThemeColor before any theme lookup, which the
	// two slices' descriptions do not carry, so here they resolve to nothing where upstream compiles.
	if candidate.Value.Kind == ParsedValueKindNamed && isColourKeyword(candidate.Value.Value) {
		return nil, SignatureUnknown
	}
	if utility, known := FrameworkFunctionalUtilities[root]; known {
		resolved, produced := ResolveFunctionalUtilityValue(candidate, utility.Description(), theme, negative)
		if !produced {
			return nil, SignatureNone
		}
		return utility.Emit(root, resolved), SignatureExact
	}
	if utility, known := FrameworkMultiDeclarationUtilities[root]; known {
		if candidate.Value != nil && candidate.Value.Kind == ParsedValueKindNamed {
			if _, isLiteral := utility.LiteralReadings[candidate.Value.Value]; isLiteral {
				return utility.Emit(root, candidate.Value.Value, ResolvedUtilityValue{}), SignatureExact
			}
		}
		if candidate.Value == nil && utility.RefusesEmptyValue {
			return nil, SignatureNone
		}
		if utility.RefusesArbitraryValue && candidate.Value != nil && candidate.Value.Kind == ParsedValueKindArbitrary {
			return nil, SignatureNone
		}
		resolved, produced := ResolveFunctionalUtilityValue(candidate, utility.Description(), theme, negative)
		if !produced {
			return nil, SignatureNone
		}
		return utility.Emit(root, "", resolved), SignatureExact
	}
	return nil, SignatureUnknown
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
	"space-x": true, "space-y": true, "divide": true, "divide-x": true, "divide-y": true,
	"skew": true, "skew-x": true, "skew-y": true, "mask-conic": true, "mask-linear": true,
	"line-clamp": true, "container": true, "transition": true,
}
