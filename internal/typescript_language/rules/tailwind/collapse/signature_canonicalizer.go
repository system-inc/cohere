package tailwind

import "strings"

// signatureCanonicalizer carries canonicalizeAst's inputs, and whether anything it did was something
// it could not be sure matched upstream.
type signatureCanonicalizer struct {
	theme    *Theme
	features SignatureFeatures
	exact    bool
}

// canonicalize is upstream's walk over one node list, with its enter and exit hooks.
//
// The walk's Replace semantics are kept: a node an enter hook replaces is spliced out, and the
// replacements are entered in its place, which is how an expanded shorthand's longhands get their
// variables resolved. The siblings an enter hook compares against are the list as it stands, earlier
// nodes already rewritten and later ones not yet, as upstream's are.
func (canonicalizer *signatureCanonicalizer) canonicalize(nodes []*Node) []*Node {
	spliced := nodes
	for index := 0; index < len(spliced); {
		node := spliced[index]
		if replacement, replaced := canonicalizer.enter(node, spliced); replaced {
			spliced = append(spliced[:index:index], append(replacement, spliced[index+1:]...)...)
			continue
		}
		if node.IsContainer() && len(node.Nodes) > 0 {
			node.Nodes = canonicalizer.canonicalize(node.Nodes)
		}
		canonicalizer.exit(node)
		index++
	}
	return spliced
}

// enter is canonicalizeAst's enter hook. It returns the nodes to put in this one's place, or false to
// keep it and descend.
//
// Mutates: node a kept declaration's value is rewritten to its canonical form in place, since the walk
// canonicalizes the tree it is handed
func (canonicalizer *signatureCanonicalizer) enter(node *Node, siblings []*Node) ([]*Node, bool) {
	switch node.Kind {
	case KindDeclaration:
		if !node.ValuePresent || node.Property == "--tw-sort" {
			return nil, true
		}

		// A `--tw-{property}` is noise when a sibling that is not a `--tw-*` declaration carries the same
		// value with the same importance.
		if strings.HasPrefix(node.Property, "--tw-") {
			for _, sibling := range siblings {
				if sibling.Kind == KindDeclaration && sibling.ValuePresent && sibling.Value == node.Value &&
					sibling.Important == node.Important && !strings.HasPrefix(sibling.Property, "--tw-") {
					return nil, true
				}
			}
		}

		if canonicalizer.features&SignatureExpandProperties != 0 {
			if expanded := expandSignatureDeclaration(node, canonicalizer.features); expanded != nil {
				return expanded, true
			}
		}

		if strings.Contains(node.Value, "var(") {
			node.Value = resolveVariablesInValue(node.Value, canonicalizer.theme)
		}

		// `rem` is upstream's option for rootFontSize, which this port does not read, so it is always
		// absent, as it is for the plugin when rootFontSize is unset.
		folded, foldedValue := constantFoldDeclarationAst(ParseValue(node.Value), nil, true)
		normalized, canonicalValue := canonicalizeCalcExpressionsAst(foldedValue)
		if folded || normalized {
			node.Value = ValueToCss(canonicalValue)
		}
		node.Value = printArbitraryValue(node.Value)
		return nil, false

	case KindContext, KindAtRoot:
		return node.Nodes, true

	case KindComment:
		return nil, true

	case KindAtRule:
		if node.Name == "@property" {
			return nil, true
		}
	}
	return nil, false
}

// exit is canonicalizeAst's exit hook: a rule's declarations deduplicated, last one winning, and sorted
// by property, and a declaration's hex colours lowercased unless it is a custom property.
//
// Mutates: node its children are deduplicated and sorted, or its value lowercased, in place, since the walk
// canonicalizes the tree it is handed
func (canonicalizer *signatureCanonicalizer) exit(node *Node) {
	switch node.Kind {
	case KindRule, KindAtRule:
		if len(node.Nodes) > 1 {
			seen := map[string]bool{}
			for index := len(node.Nodes) - 1; index >= 0; index-- {
				child := node.Nodes[index]
				if child.Kind != KindDeclaration || !child.ValuePresent {
					continue
				}
				if seen[child.Property] {
					node.Nodes = append(node.Nodes[:index:index], node.Nodes[index+1:]...)
				}
				seen[child.Property] = true
			}
		}
		for _, child := range node.Nodes {
			if child.Kind == KindDeclaration && !isByteCollatedProperty(child.Property) {
				canonicalizer.exact = false
			}
		}
		sortLikeJavaScript(node.Nodes, compareSignatureNodes)

	case KindDeclaration:
		if !node.ValuePresent || strings.HasPrefix(node.Property, "--") {
			return
		}
		node.Value = signatureHexColor.ReplaceAllStringFunc(node.Value, strings.ToLower)
	}
}
