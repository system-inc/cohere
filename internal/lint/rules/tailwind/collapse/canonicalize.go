// What Tailwind rewrites one class to on its own: upstream's `canonicalizeCandidates` for a single class
// with collapse off, ported pass by pass (#15swfzq).
//
// better-tailwindcss's enforce-canonical-classes reports a class whenever Tailwind's canonicalizer gives
// it back spelled differently, and that is a separate question from the pairs that collapse into one,
// which collapse_table.go answers. Upstream runs one class through two lists of passes. The utility, with
// its variants and importance taken off, goes through `UTILITY_CANONICALIZATIONS`; each variant goes
// through `VARIANT_CANONICALIZATIONS`; then the pieces are printed back together. A class is reported when
// the printed text differs from what was written, so printing alone is a canonicalization: `bg-[var(--a)]`
// prints as `bg-(--a)` with no pass touching it.
//
// # Which passes are here, and what the rest cost
//
// Ported: bgGradientToLinear, calcToSpacingFunction, optimizeArbitraryValueExpressions, arbitraryUtilities
// and deprecatedUtilities among the utility passes, modernizeArbitraryValuesVariant among the variant
// ones, and the printer. The other passes are not run at all, so a class only they would rewrite comes
// back unchanged and is not reported. A pass that cannot tell what upstream would do stops the passes
// after it, so the class is printed as the earlier ones left it. That is the direction this rule's comment insists
// on: a missing pass is a finding that does not appear, never one that appears wrongly.
// canonical_singles.json is Tailwind's own answer for every class on the public theme and ahra, and
// TestCanonicalizeAgreesWithTailwind fails on any class this answers differently, except the ones listed
// in canonicalUnported, each under the pass that would rewrite it.
//
// # Signatures, and the sufficient condition that stands in for them
//
// Upstream decides that two spellings mean the same thing by comparing signatures: both compiled to CSS
// with theme variables inlined, calc folded and declarations sorted. signature.go ports that, and the
// passes added in unit 1b compare through it. deprecatedUtilities, from unit 1a, needs only a yes or no
// between two specific classes and answers it by the framework emitters at real resolved values: two
// classes whose declarations come out identical have equal signatures, because a signature is a function
// of the CSS. The converse does not hold, so it can say no where upstream says yes, which under-reports,
// and can never say yes where upstream says no.
package tailwind

import (
	"strings"
	"sync"
)

// Canonicalizer answers what one design system's Tailwind rewrites a class to.
//
// Separate from LoadedDesignSystem because it remembers its answers, and the design system is immutable
// after build by contract (design_system.go). The memo is a sync.Map since every worker of a run asks
// through one Canonicalizer, the way upstream keeps one cache per design system.
type Canonicalizer struct {
	system *LoadedDesignSystem
	// features are the signature features this canonicalizer compares under, from the rule's collapse
	// and logical options. WithFeatures hands out one canonicalizer per set.
	features SignatureFeatures
	// shared is what every feature set's canonicalizer over this design system has in common.
	shared *canonicalizerShared
	// classes memoizes CanonicalizeClass by the class as written, and variants memoizes one variant's
	// canonical spelling by its printed form, as upstream's two DefaultMaps do.
	classes  sync.Map
	variants sync.Map
	// signatures memoizes a class's signature under features, and pools the signature pool for each set
	// of properties arbitraryUtilities has searched.
	signatures sync.Map
	pools      sync.Map
}

// NewCanonicalizer returns a canonicalizer over one design system, comparing under the plugin's default
// options: collapse and logical both on.
func NewCanonicalizer(system *LoadedDesignSystem) *Canonicalizer {
	features := SignatureFeaturesFor(true, true)
	shared := &canonicalizerShared{system: system}
	canonicalizer := &Canonicalizer{system: system, features: features, shared: shared}
	shared.byFeatures.Store(features, canonicalizer)
	return canonicalizer
}

// WithFeatures is the canonicalizer over the same design system comparing under other features, built
// the first time a rule's options ask for them.
func (canonicalizer *Canonicalizer) WithFeatures(features SignatureFeatures) *Canonicalizer {
	if existing, found := canonicalizer.shared.byFeatures.Load(features); found {
		return existing.(*Canonicalizer)
	}
	created, _ := canonicalizer.shared.byFeatures.LoadOrStore(features, &Canonicalizer{
		system:   canonicalizer.system,
		features: features,
		shared:   canonicalizer.shared,
	})
	return created.(*Canonicalizer)
}

// CanonicalizeClass is what Tailwind rewrites className to on its own, or className itself when nothing
// this port runs would change it.
//
// upstream's createCanonicalizeCandidateCache. A class with variants or importance is answered from the
// bare utility's answer, its variants' answers and the importance put back, and that answer is returned
// for the first reading whatever it is. A bare class tries each reading until one prints differently.
func (canonicalizer *Canonicalizer) CanonicalizeClass(className string) string {
	if cached, isCached := canonicalizer.classes.Load(className); isCached {
		return cached.(string)
	}
	canonical := canonicalizer.canonicalizeClass(className)
	canonicalizer.classes.Store(className, canonical)
	return canonical
}

func (canonicalizer *Canonicalizer) canonicalizeClass(className string) string {
	system := canonicalizer.system
	prefix := system.Prefix()

	for _, candidate := range ParseCandidate(className, system) {
		if candidate.Important || len(candidate.Variants) > 0 {
			// The variants in source order, each possibly becoming several: upstream reverses the
			// compiler's innermost-first list and flat-maps each through the variant passes.
			var variants []ParsedVariant
			for index := len(candidate.Variants) - 1; index >= 0; index-- {
				variants = append(variants, canonicalizer.canonicalizeVariant(candidate.Variants[index])...)
			}

			bare := candidate
			bare.Variants = nil
			bare.Important = false
			result := canonicalizer.CanonicalizeClass(PrintCandidate(bare, prefix))

			// The prefix sits in front of the variants, so it comes off the utility before they go on and
			// goes back on in front of them.
			if prefix != "" && len(variants) > 0 {
				result = strings.TrimPrefix(result, prefix+":")
			}
			if len(variants) > 0 {
				printed := make([]string, 0, len(variants)+1)
				for _, variant := range variants {
					printed = append(printed, PrintVariant(variant))
				}
				result = strings.Join(printed, ":") + ":" + result
			}
			if candidate.Important {
				result += "!"
			}
			if prefix != "" && len(variants) > 0 {
				result = prefix + ":" + result
			}
			return result
		}

		if result := canonicalizer.canonicalizeUtility(className); result != className {
			return result
		}
	}
	return className
}

// canonicalizeUtility is upstream's createCanonicalizeUtilityCache for a class with no variants and no
// importance: each reading through every utility pass in order, and the first that prints differently
// from what was written is the answer.
func (canonicalizer *Canonicalizer) canonicalizeUtility(className string) string {
	prefix := canonicalizer.system.Prefix()
	for _, candidate := range ParseCandidate(className, canonicalizer.system) {
		replacement := candidate
		for _, pass := range utilityCanonicalizations {
			var answered bool
			if replacement, answered = pass(canonicalizer, replacement); !answered {
				// A pass that cannot tell what upstream does stops the passes, and what the earlier ones
				// established is still printed. Every rewrite a pass here makes is signature-equivalent,
				// and upstream prints whatever its passes leave, so the class it reports is reported here
				// too, at worst naming a replacement short of upstream's, which canonicalUnported counts.
				break
			}
		}
		if printed := PrintCandidate(replacement, prefix); printed != className {
			return printed
		}
	}
	return className
}

// utilityCanonicalizations is upstream's UTILITY_CANONICALIZATIONS, in its order, holding the passes this
// port runs. Upstream's list is bgGradientToLinear, themeToVarUtility, calcToSpacingFunction,
// optimizeArbitraryValueExpressions, arbitraryUtilities, bareValueUtilities, deprecatedUtilities,
// dropUnnecessaryDataTypes, arbitraryValueToBareValueUtility and optimizeModifier. A pass missing here
// leaves its classes as written, which canonicalUnported counts. themeToVarUtility is not ported, and
// stands here as a guard that aborts a class it would change, since the passes after it would otherwise
// see a candidate upstream's never do.
//
// A pass answers false when it cannot tell what upstream would do, and no pass after it runs.
var utilityCanonicalizations = []func(*Canonicalizer, ParsedCandidate) (ParsedCandidate, bool){
	(*Canonicalizer).bgGradientToLinear,
	(*Canonicalizer).themeToVarUtilityGuard,
	(*Canonicalizer).calcToSpacingFunction,
	(*Canonicalizer).optimizeArbitraryValueExpressions,
	(*Canonicalizer).arbitraryUtilities,
	(*Canonicalizer).deprecatedUtilities,
}

// canonicalizeVariant is upstream's createCanonicalizeVariantCache: one variant through every variant
// pass, where a pass may turn one variant into several.
func (canonicalizer *Canonicalizer) canonicalizeVariant(variant ParsedVariant) []ParsedVariant {
	key := PrintVariant(variant)
	if cached, isCached := canonicalizer.variants.Load(key); isCached {
		return cached.([]ParsedVariant)
	}

	replacement := []ParsedVariant{cloneVariant(variant)}
	for _, pass := range variantCanonicalizations {
		var next []ParsedVariant
		for _, current := range replacement {
			next = append(next, pass(canonicalizer, cloneVariant(current))...)
		}
		replacement = next
	}

	canonicalizer.variants.Store(key, replacement)
	return replacement
}

// variantCanonicalizations is upstream's VARIANT_CANONICALIZATIONS, in its order, holding the passes this
// port runs. Upstream's list is themeToVarVariant, arbitraryValueToBareValueVariant,
// modernizeArbitraryValuesVariant and arbitraryVariants.
var variantCanonicalizations = []func(*Canonicalizer, ParsedVariant) []ParsedVariant{
	(*Canonicalizer).modernizeArbitraryValuesVariant,
}

// cloneVariant copies a variant deeply, so a pass that rewrites one in place, as upstream's
// replaceObject does, never reaches a variant the parser or a memo still holds.
func cloneVariant(variant ParsedVariant) ParsedVariant {
	cloned := variant
	if variant.Value != nil {
		value := *variant.Value
		cloned.Value = &value
	}
	if variant.Modifier != nil {
		modifier := *variant.Modifier
		cloned.Modifier = &modifier
	}
	if variant.Variant != nil {
		inner := cloneVariant(*variant.Variant)
		cloned.Variant = &inner
	}
	return cloned
}

// gradientDirections is upstream's DIRECTIONS, the eight a legacy `bg-gradient-to-*` can name.
var gradientDirections = map[string]bool{"t": true, "tr": true, "r": true, "br": true, "b": true, "bl": true, "l": true, "tl": true}

// bgGradientToLinear is upstream's pass of the same name: Tailwind 4's `bg-linear-to-*` for the static
// `bg-gradient-to-*` it replaced. A rename with no sameness check, as upstream has none.
func (canonicalizer *Canonicalizer) bgGradientToLinear(candidate ParsedCandidate) (ParsedCandidate, bool) {
	if candidate.Kind != ParsedCandidateKindStatic {
		return candidate, true
	}
	direction, isGradient := strings.CutPrefix(candidate.Root, "bg-gradient-to-")
	if !isGradient || !gradientDirections[direction] {
		return candidate, true
	}
	candidate.Root = "bg-linear-to-" + direction
	return candidate, true
}

// deprecationRenames is upstream's DEPRECATION_MAP: whole classes renamed outright.
var deprecationRenames = map[string]string{
	"order-none":        "order-0",
	"break-words":       "wrap-break-word",
	"overflow-ellipsis": "text-ellipsis",
}

// deprecatedUtilities is upstream's pass of the same name: a class renamed by a Tailwind release is
// rewritten to its current spelling when the two compile to the same thing.
//
// The renames are DEPRECATION_MAP and DEPRECATION_TRANSFORMATION_MAP, which turns `start-*` into
// `inset-s-*` and `end-*` into `inset-e-*`, negative or not. The sameness check is what keeps a
// repository's own `@utility start-*` as written: it compiles to its own body, not to the inset.
func (canonicalizer *Canonicalizer) deprecatedUtilities(candidate ParsedCandidate) (ParsedCandidate, bool) {
	written := PrintCandidate(candidate, "")
	for _, replacementText := range deprecatedReplacements(written) {
		if !canonicalizer.compilesTheSame(written, replacementText) {
			continue
		}
		replacements := ParseCandidate(canonicalizer.prefixed(replacementText), canonicalizer.system)
		if len(replacements) == 0 {
			continue
		}
		return replacements[0], true
	}
	return candidate, true
}

// deprecatedReplacements is upstream's tryDeprecatedUtilities: the outright rename first, then each
// pattern that changes the class. `^(-)?start-(.*?)$` with a lazy tail that is anchored at the end still
// takes the whole rest, so the pattern is a prefix test.
func deprecatedReplacements(className string) []string {
	var replacements []string
	if renamed, isRenamed := deprecationRenames[className]; isRenamed {
		replacements = append(replacements, renamed)
	}
	negative, rest := "", className
	if strings.HasPrefix(rest, "-") {
		negative, rest = "-", rest[1:]
	}
	if tail, isStart := strings.CutPrefix(rest, "start-"); isStart {
		replacements = append(replacements, negative+"inset-s-"+tail)
	}
	if tail, isEnd := strings.CutPrefix(rest, "end-"); isEnd {
		replacements = append(replacements, negative+"inset-e-"+tail)
	}
	return replacements
}

// prefixed puts the theme's prefix in front of a class that lacks it, upstream's parseCandidate wrapper.
func (canonicalizer *Canonicalizer) prefixed(className string) string {
	prefix := canonicalizer.system.Prefix()
	if prefix != "" && !strings.HasPrefix(className, prefix+":") {
		return prefix + ":" + className
	}
	return className
}

// compilesTheSame answers upstream's hasSameSignature with a condition sufficient for it: both classes
// compile through the framework's own utilities, and to identical declarations. See the file comment for
// why that can only under-report.
func (canonicalizer *Canonicalizer) compilesTheSame(left string, right string) bool {
	leftDeclarations, leftKnown := canonicalizer.exactDeclarations(left)
	if !leftKnown {
		return false
	}
	rightDeclarations, rightKnown := canonicalizer.exactDeclarations(right)
	if !rightKnown {
		return false
	}
	return leftDeclarations == rightDeclarations
}

// exactDeclarations is the CSS one class compiles to, printed, when only the framework compiles it and
// exactly one reading compiles at all.
//
// It declines whenever its answer could be short of upstream's. A root the repository also declares with
// `@utility` compiles both bodies upstream, and this sees only the framework's. A class with two readings
// that both compile would be a union upstream and one of them here.
func (canonicalizer *Canonicalizer) exactDeclarations(className string) (string, bool) {
	system := canonicalizer.system
	compiled := ""
	compiledReadings := 0
	for _, candidate := range ParseCandidate(canonicalizer.prefixed(className), system) {
		if len(candidate.Variants) > 0 || candidate.Important || candidate.Modifier != nil {
			return "", false
		}
		// Any kind the repository declared under either spelling of the root, since upstream compiles
		// the repository's body beside the framework's and this sees only the framework's.
		root := strings.TrimPrefix(candidate.Root, "-")
		if len(system.utilityRoots[root]) > 0 || len(system.utilityRoots[candidate.Root]) > 0 {
			return "", false
		}
		printed, produced := frameworkDeclarations(&candidate, system.Theme())
		if !produced {
			continue
		}
		compiled = printed
		compiledReadings++
	}
	if compiledReadings != 1 {
		return "", false
	}
	return compiled, true
}

// frameworkDeclarations compiles one framework reading to its declarations at real resolved values,
// printed one per line, or false when no framework table answers it.
//
// The same dispatch ReadingFor and Reading use, kept to the two functional tables and the statics, whose
// emitters take the resolved value as given. The gap roots are left out: their emitter answers a branch,
// not a value, so two of them could print alike while upstream's CSS differs.
func frameworkDeclarations(candidate *ParsedCandidate, theme *Theme) (string, bool) {
	if candidate.Kind == ParsedCandidateKindStatic {
		declarations, isStatic := FrameworkStaticDeclarations[candidate.Root]
		if !isStatic {
			return "", false
		}
		var printed strings.Builder
		for _, declaration := range declarations {
			printed.WriteString(declaration.Property + ":" + declaration.Value)
			if declaration.Important {
				printed.WriteString(" !important")
			}
			printed.WriteString("\n")
		}
		return printed.String(), printed.Len() > 0
	}
	if candidate.Kind != ParsedCandidateKindFunctional {
		return "", false
	}

	root := strings.TrimPrefix(candidate.Root, "-")
	negative := root != candidate.Root
	var nodes []*Node
	if utility, known := FrameworkFunctionalUtilities[root]; known {
		resolved, produced := ResolveFunctionalUtilityValue(candidate, utility.Description(), theme, negative)
		if !produced {
			return "", false
		}
		nodes = utility.Emit(root, resolved)
	} else if utility, known := FrameworkMultiDeclarationUtilities[root]; known {
		literal := ""
		if candidate.Value != nil && candidate.Value.Kind == ParsedValueKindNamed {
			if _, isLiteral := utility.LiteralReadings[candidate.Value.Value]; isLiteral {
				literal = candidate.Value.Value
			}
		}
		if literal == "" {
			if candidate.Value == nil && utility.RefusesEmptyValue {
				return "", false
			}
			if utility.RefusesArbitraryValue && candidate.Value != nil && candidate.Value.Kind == ParsedValueKindArbitrary {
				return "", false
			}
			resolved, produced := ResolveFunctionalUtilityValue(candidate, utility.Description(), theme, negative)
			if !produced {
				return "", false
			}
			nodes = utility.Emit(root, "", resolved)
		} else {
			nodes = utility.Emit(root, literal, ResolvedUtilityValue{})
		}
	} else {
		return "", false
	}

	var printed strings.Builder
	printNodesForComparison(&printed, nodes)
	return printed.String(), printed.Len() > 0
}

// printNodesForComparison writes a node tree as text two trees can be compared by: every rule with its
// selector, every declaration with its value and importance, in emitted order.
func printNodesForComparison(printed *strings.Builder, nodes []*Node) {
	for _, node := range nodes {
		switch node.Kind {
		case KindRule:
			printed.WriteString(node.Selector + "{\n")
			printNodesForComparison(printed, node.Nodes)
			printed.WriteString("}\n")
		case KindAtRule:
			printed.WriteString(node.Name + " " + node.Params + "{\n")
			printNodesForComparison(printed, node.Nodes)
			printed.WriteString("}\n")
		case KindDeclaration:
			if !node.ValuePresent {
				continue
			}
			printed.WriteString(node.Property + ":" + node.Value)
			if node.Important {
				printed.WriteString(" !important")
			}
			printed.WriteString("\n")
		}
	}
}
