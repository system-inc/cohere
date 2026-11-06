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
