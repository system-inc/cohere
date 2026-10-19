package tailwind

import "strings"

// modernizeArbitraryValuesVariant is upstream's pass of the same name (canonicalize-candidates.ts, 4.3.3):
// an arbitrary variant whose selector a named variant already spells is rewritten to that variant. `[&>*]`
// is `*`, `[&_*]` is `**`, `[&:has(p)]` is `has-[p]`, `[[data-x]_&]` is `in-[[data-x]]`, a lone pseudo
// class such as `[&:first-child]` is `first`, and a lone attribute such as `[&[aria-sort]]` is
// `aria-[sort]`. A child or descendant combinator in front of a pseudo class or an attribute splits the
// variant in two: `[&>[data-dot=true]]` is `*:data-[dot=true]`.
//
// Ported whole, branch for branch, over SelectorParser and AttributeSelectorParser, with two branches
// that need a variant's signature answered as if the signatures differ: hoisting `not` out of an
// `@media` or `@supports` variant, and hoisting a `:not(…)` of a pseudo class that has no named variant.
// Upstream compiles both spellings and compares them, which is unit 1b's machinery (#15swfzq). Taking the
// differ path there is what upstream itself does when they differ, so the port can only fall short of a
// rewrite, never make one upstream would not.
func (canonicalizer *Canonicalizer) modernizeArbitraryValuesVariant(variant ParsedVariant) []ParsedVariant {
	system := canonicalizer.system
	top := &variant

	// upstream's walkVariants yields a variant and then, if it is still a compound once the visit is
	// done, its nested variant, so a variant replaced in place is walked as what it became.
	var visit func(current *ParsedVariant, parent *ParsedVariant) []ParsedVariant
	visit = func(current *ParsedVariant, parent *ParsedVariant) []ParsedVariant {
		if result, returned := canonicalizer.modernizeOne(current, parent, system); returned {
			return result
		}
		if current.Kind == ParsedVariantKindCompound && current.Variant != nil {
			return visit(current.Variant, current)
		}
		return nil
	}
	if result := visit(top, nil); result != nil {
		return result
	}
	return []ParsedVariant{*top}
}

// modernizeOne is the loop body of upstream's pass for one variant of the walk. It rewrites the variant in
// place, as upstream's replaceObject does, and returns true only where upstream returns from the whole
// pass, which is the split into two variants.
func (canonicalizer *Canonicalizer) modernizeOne(
	variant *ParsedVariant,
	parent *ParsedVariant,
	system *LoadedDesignSystem,
) ([]ParsedVariant, bool) {
	// Forward a modifier from `has`, `not` or `in` to the variant it wraps, when that variant can carry
	// one. Upstream tests `'modifier' in variant.variant`, which holds for a functional or compound
	// variant and not for a static or arbitrary one.
	if variant.Kind == ParsedVariantKindCompound && (variant.Root == "has" || variant.Root == "not" || variant.Root == "in") {
		if variant.Modifier != nil && variant.Variant != nil &&
			(variant.Variant.Kind == ParsedVariantKindFunctional || variant.Variant.Kind == ParsedVariantKindCompound) {
			variant.Variant.Modifier = variant.Modifier
			variant.Modifier = nil
		}
	}

	if variant.Kind != ParsedVariantKindArbitrary || variant.Relative {
		return nil, false
	}

	ast := ParseSelector(variant.Selector)
	if !isSingleSelector(ast) {
		return nil, false
	}
	// A complex selector is one node holding the sequence; the branches below read the sequence.
	if len(ast) == 1 && ast[0].Kind == SelectorNodeKindComplex {
		ast = ast[0].Nodes
	}

	topLevel := parent == nil
	isNesting := func(node SelectorNode) bool {
		return node.Kind == SelectorNodeKindSelector && node.Value == "&"
	}
	isCombinator := func(node SelectorNode, value string) bool {
		return node.Kind == SelectorNodeKindCombinator && node.Value == value
	}
	replaceWith := func(text string) bool {
		parsed := ParseVariant(text, system)
		if parsed == nil {
			return false
		}
		*variant = *parsed
		return true
	}

	// `[&>*]` is `*`, and `[&_*]` is `**`. Only at the top, so `has-[&>*]` is left to another pass.
	if topLevel && len(ast) == 3 && isNesting(ast[0]) && ast[2].Kind == SelectorNodeKindSelector && ast[2].Value == "*" {
		if isCombinator(ast[1], ">") {
			replaceWith("*")
			return nil, false
		}
		if isCombinator(ast[1], " ") {
			replaceWith("**")
			return nil, false
		}
	}

	// `[&:has(…)]` is `has-[…]`.
	if topLevel && len(ast) == 1 && ast[0].Kind == SelectorNodeKindCompound && len(ast[0].Nodes) == 2 &&
		isNesting(ast[0].Nodes[0]) &&
		ast[0].Nodes[1].Kind == SelectorNodeKindFunction && ast[0].Nodes[1].Value == ":has" &&
		len(ast[0].Nodes[1].Nodes) == 1 && ast[0].Nodes[1].Nodes[0].Kind == SelectorNodeKindSelector {
		replaceWith("has-[" + SelectorToCss(ast[0].Nodes[1].Nodes, true) + "]")
		return nil, false
	}

	// A selector ending in ` &` is an `in-*` variant: `[[data-visible]_&]` is `in-[[data-visible]]`,
	// which the walk then reaches as a compound and modernizes further.
	if topLevel && len(ast) == 3 && ast[0].Kind == SelectorNodeKindSelector && isCombinator(ast[1], " ") && isNesting(ast[2]) {
		replaceWith("in-[" + SelectorToCss(ast[:1], true) + "]")
		return nil, false
	}

	// Hoisting `not` out of an `@media` or `@supports` variant is decided by comparing the two
	// variants' signatures, which this port does not compute. Upstream falls through to the code below
	// whenever the signatures differ, so falling through here is its own answer for that case.

	var prefixedVariant *ParsedVariant

	// A child combinator in front of a pseudo class or an attribute: `[&>[data-visible]]` is
	// `*:data-visible`. A descendant one: `[&_[data-visible]]` is `**:data-visible`.
	if topLevel && len(ast) == 3 && isNesting(ast[0]) && ast[2].Kind == SelectorNodeKindSelector &&
		(startsWithColon(ast[2].Value) || isBracketedAttribute(ast[2])) {
		if isCombinator(ast[1], ">") {
			ast = []SelectorNode{ast[2]}
			prefixedVariant = ParseVariant("*", system)
		} else if isCombinator(ast[1], " ") {
			ast = []SelectorNode{ast[2]}
			prefixedVariant = ParseVariant("**", system)
		}
	}

	selectorNodes := withoutNestingSelectors(ast)
	if len(selectorNodes) != 1 {
		return nil, false
	}

	target := selectorNodes[0]
	if target.Kind == SelectorNodeKindFunction && target.Value == ":is" {
		// One selector inside, and `[foo][bar]` counts as one selector of several nodes, so the node count
		// is checked as well.
		if !isSingleSelector(target.Nodes) || len(target.Nodes) != 1 {
			return nil, false
		}
		if target.Nodes[0].Kind == SelectorNodeKindSelector && !isBracketedAttribute(target.Nodes[0]) {
			return nil, false
		}
		target = target.Nodes[0]
	}

	switch {
	case (target.Kind == SelectorNodeKindFunction || target.Kind == SelectorNodeKindSelector) && startsWithColon(target.Value):
		targetNode := target
		compoundNot := false
		if targetNode.Kind == SelectorNodeKindFunction && targetNode.Value == ":not" {
			compoundNot = true
			if len(targetNode.Nodes) != 1 {
				return nil, false
			}
			inner := targetNode.Nodes[0]
			if inner.Kind != SelectorNodeKindSelector && inner.Kind != SelectorNodeKindFunction {
				return nil, false
			}
			if !startsWithColon(inner.Value) {
				return nil, false
			}
			targetNode = inner
		}

		newVariant, found := pseudoClassVariant(targetNode, &compoundNot)
		if !found {
			if prefixedVariant != nil {
				*variant = ParsedVariant{Kind: ParsedVariantKindArbitrary, Selector: target.Value}
				return []ParsedVariant{*prefixedVariant, *variant}, true
			}
			return nil, false
		}
		if compoundNot {
			newVariant = "not-" + newVariant
		}
		if !replaceWith(newVariant) {
			return nil, false
		}

	case target.Kind == SelectorNodeKindSelector && isBracketedAttribute(target):
		attribute, parsed := ParseAttributeSelector(target.Value)
		if !parsed {
			return nil, false
		}
		*variant = attributeVariant(attribute, target.Value)
	}

	if prefixedVariant != nil {
		return []ParsedVariant{*prefixedVariant, *variant}, true
	}
	return nil, false
}

// pseudoClassVariant is the named variant upstream's inline function finds for a pseudo class, and false
// where it finds none. `odd` and `even` swap under `:not`, which is why compoundNot is passed by
// reference: upstream clears it there so no `not-` is added.
//
// The `:not` hoist for a pseudo class with no named variant compares two signatures, and is answered as
// if they differ, which is upstream's own answer then: no variant.
func pseudoClassVariant(targetNode SelectorNode, compoundNot *bool) (string, bool) {
	isFunctionOfOne := targetNode.Kind == SelectorNodeKindFunction && len(targetNode.Nodes) == 1
	argument := ""
	argumentIsValue := false
	if isFunctionOfOne {
		argument = targetNode.Nodes[0].Value
		argumentIsValue = targetNode.Nodes[0].Kind == SelectorNodeKindValue
	}

	if targetNode.Value == ":nth-child" && isFunctionOfOne && argumentIsValue && (argument == "odd" || argument == "even") {
		if *compoundNot {
			*compoundNot = false
			if argument == "odd" {
				return "even", true
			}
			return "odd", true
		}
		return argument, true
	}

	for _, pair := range [...]struct{ selector, variant string }{
		{":nth-child", "nth"},
		{":nth-last-child", "nth-last"},
		{":nth-of-type", "nth-of-type"},
		{":nth-last-of-type", "nth-of-last-type"},
	} {
		if targetNode.Value == pair.selector && isFunctionOfOne {
			if argumentIsValue && isPositiveInteger(argument) {
				return pair.variant + "-" + argument, true
			}
			return pair.variant + "-[" + SelectorToCss(targetNode.Nodes, true) + "]", true
		}
	}

	return "", false
}

// attributeVariant is the variant upstream builds for a lone attribute selector: `data-*`, `aria-*`, or
// the attribute kept as an arbitrary variant.
func attributeVariant(attribute AttributeSelector, written string) ParsedVariant {
	// `${operator}${quote}${value}${quote}${ sensitivity}`, the tail every arbitrary form below repeats.
	tail := func() string {
		value := ""
		if attribute.Value != nil {
			value = *attribute.Value
		}
		sensitivity := ""
		if attribute.Sensitivity != "" {
			sensitivity = " " + attribute.Sensitivity
		}
		return attribute.Operator + attribute.Quote + value + attribute.Quote + sensitivity
	}

	if name, isData := strings.CutPrefix(attribute.Attribute, "data-"); isData {
		if attribute.Value == nil {
			return ParsedVariant{Kind: ParsedVariantKindFunctional, Root: "data", Value: &ParsedVariantValue{Kind: ParsedValueKindNamed, Value: name}}
		}
		return ParsedVariant{Kind: ParsedVariantKindFunctional, Root: "data", Value: &ParsedVariantValue{Kind: ParsedValueKindArbitrary, Value: name + tail()}}
	}

	if name, isAria := strings.CutPrefix(attribute.Attribute, "aria-"); isAria {
		switch {
		case attribute.Value == nil:
			return ParsedVariant{Kind: ParsedVariantKindFunctional, Root: "aria", Value: &ParsedVariantValue{Kind: ParsedValueKindArbitrary, Value: name}}
		case attribute.Operator == "=" && *attribute.Value == "true" && attribute.Sensitivity == "":
			return ParsedVariant{Kind: ParsedVariantKindFunctional, Root: "aria", Value: &ParsedVariantValue{Kind: ParsedValueKindNamed, Value: name}}
		default:
			return ParsedVariant{Kind: ParsedVariantKindFunctional, Root: "aria", Value: &ParsedVariantValue{Kind: ParsedValueKindArbitrary, Value: attribute.Attribute + tail()}}
		}
	}

	return ParsedVariant{Kind: ParsedVariantKindArbitrary, Selector: written}
}

// isSingleSelector is upstream's helper: anything but a selector list.
func isSingleSelector(ast []SelectorNode) bool {
	return !(len(ast) == 1 && ast[0].Kind == SelectorNodeKindList)
}

// isBracketedAttribute is upstream's local isAttributeSelector, which reads only the brackets at either end
// of the node's value, whatever its kind.
func isBracketedAttribute(node SelectorNode) bool {
	return len(node.Value) > 0 && node.Value[0] == '[' && node.Value[len(node.Value)-1] == ']'
}

func startsWithColon(value string) bool {
	return len(value) > 0 && value[0] == ':'
}

// withoutNestingSelectors is upstream's walk over the selector: every `&` dropped, a function's arguments
// left unvisited, and a compound left holding one node replaced by that node on the way out.
func withoutNestingSelectors(nodes []SelectorNode) []SelectorNode {
	kept := make([]SelectorNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == SelectorNodeKindSelector && node.Value == "&" {
			continue
		}
		if node.Kind == SelectorNodeKindFunction {
			kept = append(kept, node)
			continue
		}
		if len(node.Nodes) > 0 {
			node.Nodes = withoutNestingSelectors(node.Nodes)
		}
		if node.Kind == SelectorNodeKindCompound && len(node.Nodes) == 1 {
			kept = append(kept, node.Nodes[0])
			continue
		}
		kept = append(kept, node)
	}
	return kept
}
