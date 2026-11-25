package tailwind

import "strings"

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

// ClassSignature is the signature of one class without variants or importance, under the given features.
func (compiler *SignatureCompiler) ClassSignature(
	className string,
	features SignatureFeatures,
) (string, SignatureState) {
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

// signatureNodes is the body upstream's `@apply` would substitute for the class: every reading that
// compiles, concatenated in the order compileCandidates sorts their rules into. Exact only when every
// reading's answer is.
func (compiler *SignatureCompiler) signatureNodes(className string) ([]*Node, SignatureState) {
	system := compiler.system
	var readings [][]*Node
	for _, candidate := range ParseCandidate(prefixedClass(className, system), system) {
		if len(candidate.Variants) > 0 || candidate.Important {
			return nil, SignatureUnknown
		}
		nodes, state := compiler.readingNodes(&candidate)
		switch state {
		case SignatureUnknown:
			return nil, SignatureUnknown
		case SignatureExact:
			readings = append(readings, nodes)
		}
	}
	switch len(readings) {
	case 0:
		return nil, SignatureNone
	case 1:
		return readings[0], SignatureExact
	}
	sortReadingsLikeCompile(readings)
	var concatenated []*Node
	for _, nodes := range readings {
		concatenated = append(concatenated, nodes...)
	}
	return concatenated, SignatureExact
}

// readingNodes compiles one reading of a class.
func (compiler *SignatureCompiler) readingNodes(candidate *ParsedCandidate) ([]*Node, SignatureState) {
	system := compiler.system
	// A fraction is parsed with its denominator as the modifier, `w-1/2` as `1` over `2`. A root that
	// supports fractions reads the fraction before it reads a modifier, so for one of those it is not a
	// modifier at all; the parser fills the fraction for any named modifier, `black/10` included, and on
	// every other root it is the modifier it looks like.
	isFraction := candidate.Value != nil &&
		candidate.Value.Fraction != "" &&
		suggestsFractions(strings.TrimPrefix(candidate.Root, "-"))
	if candidate.Modifier != nil && !isFraction {
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
	// two slices' descriptions do not carry. A colour root, one whose `suggest` offers them, takes the
	// keyword's colour through its emitter; any other root resolves it as an ordinary name.
	if candidate.Value.Kind == ParsedValueKindNamed &&
		isColourKeyword(candidate.Value.Value) &&
		suggestsColourKeywords(root) {
		resolved := ResolvedUtilityValue{Value: colourKeywordValue(candidate.Value.Value)}
		if utility, known := FrameworkFunctionalUtilities[root]; known {
			return utility.Emit(root, resolved), SignatureExact
		}
		if utility, known := FrameworkMultiDeclarationUtilities[root]; known {
			return utility.Emit(root, "", resolved), SignatureExact
		}
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

// ClassSignatureBound is the bound for one class without variants or importance.
func (compiler *SignatureCompiler) ClassSignatureBound(className string, features SignatureFeatures) SignatureBound {
	system := compiler.system
	bound := SignatureBound{Certain: map[string]bool{}, Maybe: map[string]bool{}}
	candidates := ParseCandidate(prefixedClass(className, system), system)
	// Several readings may each compile, and upstream's body is their concatenation, so with more
	// than one nothing a reading declares is certain.
	several := len(candidates) > 1
	for index := range candidates {
		candidate := &candidates[index]
		nodes, certain, known := compiler.boundNodes(candidate)
		if !known {
			return SignatureBound{Unbounded: true}
		}
		addBoundNodes(&bound, nodes, features, several || !certain)
	}
	// A class this port reads as declaring nothing is one it does not understand: every listed class
	// declares something, so an empty bound would rule it out of every pool.
	if len(bound.Certain) == 0 && len(bound.Maybe) == 0 {
		return SignatureBound{Unbounded: true}
	}
	return bound
}

// boundNodes is a reading's compiled body as far as its properties go. Where readingNodes compiles it
// exactly, those nodes, certain. Otherwise every property the root was seen to declare, from
// FrameworkRootProperties, or a static's flattened body, or a repository block's compiled body, all
// read as possible rather than certain. The second bool is whether the nodes are certain, the third
// whether anything is known at all.
func (compiler *SignatureCompiler) boundNodes(candidate *ParsedCandidate) ([]*Node, bool, bool) {
	system := compiler.system
	if nodes, state := compiler.readingNodes(candidate); state != SignatureUnknown {
		return nodes, true, true
	}
	switch candidate.Kind {
	case ParsedCandidateKindStatic:
		// A static's declarations are its declarations however they nest, so one body alone is certain.
		// The framework's and the repository's together are a union upstream orders its own way, and
		// are only possible.
		var nodes []*Node
		bodies := 0
		if system.utilityRoots[candidate.Root][UtilityKindStatic] {
			nodes = append(nodes, system.staticUtilityNodes[candidate.Root]...)
			bodies++
		}
		if _, isRegistered := FrameworkStaticSuggested[candidate.Root]; isRegistered {
			declarations, isFlattened := FrameworkStaticDeclarations[candidate.Root]
			if !isFlattened {
				return nil, false, false
			}
			for _, declaration := range declarations {
				nodes = append(nodes, &Node{
					Kind:         KindDeclaration,
					Property:     declaration.Property,
					Value:        declaration.Value,
					ValuePresent: declaration.ValuePresent,
				})
			}
			bodies++
		}
		return nodes, bodies == 1, true

	case ParsedCandidateKindFunctional:
		var nodes []*Node
		if system.DeclaresFunctionalUtility(candidate.Root) {
			if repositoryNodes, compiles := compiler.utilities.Compile(candidate); compiles {
				nodes = append(nodes, repositoryNodes...)
			}
			if !frameworkFunctionalRoots[candidate.Root] {
				return nodes, false, true
			}
		}
		universe, probed := FrameworkRootProperties[candidate.Root]
		if !probed {
			return nil, false, false
		}
		for _, property := range universe.armFor(candidate, compiler.compileTheme) {
			nodes = append(nodes, Declaration(property, declaredPropertiesSentinel))
		}
		return nodes, false, true
	}
	return nil, false, false
}
