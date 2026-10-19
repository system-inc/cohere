// What a class's signature can declare when this port cannot compute the signature itself (#15swfzq
// unit 1b).
//
// arbitraryUtilities bails when two listed classes share the written class's signature. A listed class
// whose signature is unknown could be one of them, so the pass cannot rule it out by comparing strings.
// It can rule it out by comparing properties: two equal signatures declare the same properties, so a
// class that cannot declare exactly the written class's properties cannot share its signature. That is
// the possible-match relation the bail counts with, and it errs toward possible: anything this file is
// unsure of widens the bound rather than narrowing it.
//
// SignatureBounds answers two sets. Every property in Certain is in the class's signature, and every
// property in its signature is in Certain or Maybe. A declaration lands in Maybe when canonicalizing
// might remove or rename it: a `--tw-*` declaration beside one that is not, which upstream drops when
// their values match, and a shorthand the signature expands, whose longhands depend on how many values
// it was written with. Unbounded means nothing is known, and such a class is a possible match for every
// written class.
//
// TestSignatureBoundsHoldUpstreamsProperties checks the bound against the properties of upstream's own
// signature for every class this port cannot compile, on three themes.
package tailwind

import "strings"

// SignatureBound is the properties one class's signature must and might declare.
type SignatureBound struct {
	// Certain are the properties the signature declares whatever this port is unsure of.
	Certain map[string]bool
	// Maybe are the properties it might also declare.
	Maybe map[string]bool
	// Unbounded is a class this port knows nothing about.
	Unbounded bool
}

// Admits reports whether a signature declaring exactly these properties is within the bound: the
// possible-match relation, which answers true whenever it cannot answer false.
func (bound SignatureBound) Admits(properties map[string]bool) bool {
	if bound.Unbounded {
		return true
	}
	for property := range bound.Certain {
		if !properties[property] {
			return false
		}
	}
	for property := range properties {
		if !bound.Certain[property] && !bound.Maybe[property] {
			return false
		}
	}
	return true
}

// SignatureProperties is the set of properties an exact signature declares, read off the canonical
// nodes rather than the printed string.
func SignatureProperties(signature string) map[string]bool {
	properties := map[string]bool{}
	for _, line := range strings.Split(signature, "\n") {
		line = strings.TrimLeft(line, " ")
		colon := strings.Index(line, ": ")
		if colon <= 0 || strings.HasSuffix(line, "{") {
			continue
		}
		properties[line[:colon]] = true
	}
	return properties
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
		var nodes []*Node
		if system.utilityRoots[candidate.Root][UtilityKindStatic] {
			nodes = append(nodes, system.staticUtilityNodes[candidate.Root]...)
		}
		if _, isRegistered := FrameworkStaticSuggested[candidate.Root]; isRegistered {
			declarations, isFlattened := FrameworkStaticDeclarations[candidate.Root]
			if !isFlattened {
				return nil, false, false
			}
			for _, declaration := range declarations {
				nodes = append(nodes, &Node{Kind: KindDeclaration, Property: declaration.Property, Value: declaration.Value, ValuePresent: true})
			}
		}
		return nodes, false, true

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

// RootProperties is one root's property universe, split by the arm a class takes.
type RootProperties struct {
	// Colour is what a named value a colour namespace resolves declares.
	Colour []string
	// Other is what any other named value, or the bare root, declares.
	Other []string
	// Arbitrary is what an arbitrary value was seen to declare.
	Arbitrary []string
}

// armFor is the part of the universe a class can reach: a named value a colour namespace resolves, or a
// colour keyword, reaches the colour arm, since a colour-bearing root tries its colour lookup first; any
// other named value and the bare root reach the rest; an arbitrary value, whose arm its inferred type
// picks, is given all three.
func (universe RootProperties) armFor(candidate *ParsedCandidate, theme *Theme) []string {
	if candidate.Value == nil {
		return universe.Other
	}
	if candidate.Value.Kind != ParsedValueKindNamed {
		return append(append(append([]string(nil), universe.Colour...), universe.Other...), universe.Arbitrary...)
	}
	if isColourKeyword(candidate.Value.Value) {
		return universe.Colour
	}
	if _, isColour := theme.Resolve(candidate.Value.Value, true, colourNamespacesOf(strings.TrimPrefix(candidate.Root, "-")), 0); isColour {
		return universe.Colour
	}
	return universe.Other
}

// colourNamespacesOf is every colour namespace a root's suggestions draw on: `--color` and any ending
// in `-color`, the same test the generator split the universe by.
func colourNamespacesOf(root string) []string {
	var colour []string
	for _, group := range FrameworkSuggestions[root] {
		for _, namespace := range group.Values.Namespaces {
			if namespace == "--color" || strings.HasSuffix(namespace, "-color") {
				colour = appendUnique(colour, namespace)
			}
		}
	}
	return colour
}

// addBoundNodes records one body's declarations into a bound.
func addBoundNodes(bound *SignatureBound, nodes []*Node, features SignatureFeatures, uncertain bool) {
	for _, node := range nodes {
		switch node.Kind {
		case KindRule, KindAtRule, KindContext, KindAtRoot:
			if node.Kind == KindAtRule && node.Name == "@property" {
				continue
			}
			addBoundNodes(bound, node.Nodes, features, uncertain)
		case KindDeclaration:
			if !node.ValuePresent || node.Property == "--tw-sort" {
				continue
			}
			property := node.Property
			maybe := uncertain
			// Dropped when a sibling that is not a `--tw-*` declaration carries the same value.
			if strings.HasPrefix(property, "--tw-") && hasPlainSibling(nodes) {
				maybe = true
			}
			if longhands := expandableLonghands(property, features); longhands != nil {
				bound.Maybe[property] = true
				for _, longhand := range longhands {
					bound.Maybe[longhand] = true
				}
				continue
			}
			if maybe {
				bound.Maybe[property] = true
			} else {
				bound.Certain[property] = true
			}
		}
	}
}

// hasPlainSibling reports whether a node list holds a declaration that is not a `--tw-*` one.
func hasPlainSibling(nodes []*Node) bool {
	for _, node := range nodes {
		if node.Kind == KindDeclaration && !strings.HasPrefix(node.Property, "--tw-") {
			return true
		}
	}
	return false
}

// expandableLonghands is every longhand a shorthand could expand into under these features, or nil
// when the property is not one the signature expands.
func expandableLonghands(property string, features SignatureFeatures) []string {
	if features&SignatureExpandProperties == 0 {
		return nil
	}
	spreads := signatureVariadicExpansions[property]
	if features&SignatureLogicalToPhysical != 0 {
		if logical, isLogical := signatureVariadicLogicalExpansions[property]; isLogical {
			spreads = logical
		} else if longhands, isLogical := signatureLogicalExpansions[property]; isLogical {
			return longhands
		}
	}
	if spreads == nil {
		return nil
	}
	seen := map[string]bool{}
	var longhands []string
	for _, spread := range spreads {
		for _, longhand := range spread {
			if !seen[longhand.property] {
				seen[longhand.property] = true
				longhands = append(longhands, longhand.property)
			}
		}
	}
	return longhands
}
