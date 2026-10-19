// A utility's signature: upstream's normal form of what one class compiles to, ported so two classes
// can be compared the way Tailwind's canonicalizer compares them (#15swfzq unit 1b).
//
// Upstream's createUtilitySignatureCache compiles `.x { @apply <class> }`, runs canonicalizeAst over the
// result and prints it. Two classes with the same signature are interchangeable to every pass that
// rewrites a utility, and arbitraryUtilities in particular rewrites a written arbitrary value to the one
// listed class sharing its signature. So the string has to be upstream's string, not one that agrees
// with it most of the time: a signature that splits two classes upstream joins turns its bail into a
// rewrite here.
//
// This file is the half that does not depend on how a class is compiled: given the compiled nodes, it
// produces the string. canonicalizeSignatureAst is canonicalizeAst, expandSignatureDeclaration is
// expand-declaration.ts, resolveVariablesInValue is the function of the same name, and
// printSignatureCss is ast.ts's toCss for the node kinds a signature can hold.
// TestSignatureAgreesWithTailwind holds the whole of it to upstream's own signatures, by hash.
//
// # Where it cannot be sure, it says so
//
// UtilitySignature returns whether its answer is exact. It is not when the property sort would have to
// order names upstream's `localeCompare` might order differently from bytes, which is any name outside
// lowercase ASCII letters, digits and hyphens. A caller treats an inexact signature as unknown, never as
// a value to compare.
package tailwind

import (
	"regexp"
	"strings"
)

// SignatureFeatures are upstream's SignatureFeatures, set from the rule's options as createSignatureOptions
// sets them: `collapse` turns on ExpandProperties and `logicalToPhysical` turns on LogicalToPhysical.
type SignatureFeatures int

const (
	// SignatureExpandProperties expands shorthands into their longhands before comparing.
	SignatureExpandProperties SignatureFeatures = 1 << 0
	// SignatureLogicalToPhysical expands logical block and inline properties into physical ones, and only
	// does anything when ExpandProperties is on too, since expansion is where it is applied.
	SignatureLogicalToPhysical SignatureFeatures = 1 << 1
)

// SignatureFeaturesFor is the features the plugin's options turn on.
func SignatureFeaturesFor(collapse bool, logicalToPhysical bool) SignatureFeatures {
	var features SignatureFeatures
	if collapse {
		features |= SignatureExpandProperties
	}
	if logicalToPhysical {
		features |= SignatureLogicalToPhysical
	}
	return features
}

// UtilitySignature is the signature of a class whose compiled body is nodes, and whether it is exact.
//
// The nodes are the class's rule body as upstream's `@apply` substitutes it into `.x`: declarations and
// any nested rule, without the class's own selector. They are cloned first, since canonicalizing rewrites
// values in place.
func UtilitySignature(nodes []*Node, theme *Theme, features SignatureFeatures) (string, bool) {
	root := StyleRule(".x", cloneNodes(nodes)...)
	canonicalizer := signatureCanonicalizer{theme: theme, features: features, exact: true}
	ast := canonicalizer.canonicalize([]*Node{root})
	var printed strings.Builder
	for _, node := range ast {
		printSignatureCss(&printed, node, 0)
	}
	return printed.String(), canonicalizer.exact
}

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
	for index := 0; index < len(nodes); {
		node := nodes[index]
		if replacement, replaced := canonicalizer.enter(node, nodes); replaced {
			nodes = append(nodes[:index:index], append(replacement, nodes[index+1:]...)...)
			continue
		}
		if node.IsContainer() && len(node.Nodes) > 0 {
			node.Nodes = canonicalizer.canonicalize(node.Nodes)
		}
		canonicalizer.exit(node)
		index++
	}
	return nodes
}

// enter is canonicalizeAst's enter hook. It returns the nodes to put in this one's place, or false to
// keep it and descend.
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

// signatureHexColor is upstream's HEX_REGEX, the four hex colour lengths, longest first.
var signatureHexColor = regexp.MustCompile(`(?i)#(?:[a-f0-9]{8}|[a-f0-9]{6}|[a-f0-9]{4}|[a-f0-9]{3})`)

// exit is canonicalizeAst's exit hook: a rule's declarations deduplicated, last one winning, and sorted
// by property, and a declaration's hex colours lowercased unless it is a custom property.
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

// compareSignatureNodes is upstream's sort comparator: declarations by property, and zero whenever
// either side is anything else. The zero makes the order depend on the sort algorithm as well as the
// comparator, which is why sortLikeJavaScript is V8's.
//
// Properties compare by bytes, which is what `localeCompare` gives for the alphabet isByteCollatedProperty
// accepts. Measured on every property name in ahra's 37,070 signatures: the two orders agree.
func compareSignatureNodes(left *Node, right *Node) int {
	if left.Kind != KindDeclaration || right.Kind != KindDeclaration {
		return 0
	}
	return strings.Compare(left.Property, right.Property)
}

// isByteCollatedProperty reports whether a property name is made only of lowercase ASCII letters, digits
// and hyphens, the names whose `localeCompare` order is their byte order. `a_b` sorts before `a-b` and
// `ab` before `aB` under `localeCompare`, so a name with an underscore or a capital is not.
func isByteCollatedProperty(property string) bool {
	for index := 0; index < len(property); index++ {
		character := property[index]
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '-' {
			return false
		}
	}
	return true
}

// sortLikeJavaScript sorts in place exactly as V8's Array.prototype.sort does for the short lists a
// signature holds: TimSort, which below 64 elements is one run counted from the front, reversed if it
// descends, and binary insertion for the rest. Reproduced rather than replaced with a stable sort because
// upstream's comparator answers zero for any pair involving a nested rule, and with such a comparator
// the result depends on the algorithm.
//
// A list of 64 or more would be merged in runs upstream. No signature holds one, so it falls back to a
// stable insertion sort, which agrees whenever the comparator is consistent.
func sortLikeJavaScript(nodes []*Node, compare func(left *Node, right *Node) int) {
	length := len(nodes)
	if length < 2 {
		return
	}
	// CountAndMakeRun.
	runLength := 2
	isDescending := compare(nodes[1], nodes[0]) < 0
	previous := nodes[1]
	for index := 2; index < length; index++ {
		order := compare(nodes[index], previous)
		if isDescending && order >= 0 || !isDescending && order < 0 {
			break
		}
		previous = nodes[index]
		runLength++
	}
	if isDescending {
		for left, right := 0, runLength-1; left < right; left, right = left+1, right-1 {
			nodes[left], nodes[right] = nodes[right], nodes[left]
		}
	}
	// BinaryInsertionSort from the end of the run.
	for start := runLength; start < length; start++ {
		pivot := nodes[start]
		left, right := 0, start
		for left < right {
			middle := left + (right-left)>>1
			if compare(pivot, nodes[middle]) < 0 {
				right = middle
			} else {
				left = middle + 1
			}
		}
		copy(nodes[left+1:start+1], nodes[left:start])
		nodes[left] = pivot
	}
}

// printSignatureCss is ast.ts's toCss for the kinds a canonical signature holds, which are rules,
// at-rules, declarations and comments, in its exact format: two spaces of indent per level, `property:
// value;` with ` !important` before the semicolon, and an at-rule with no body ending in a semicolon.
func printSignatureCss(printed *strings.Builder, node *Node, depth int) {
	indent := strings.Repeat("  ", depth)
	switch node.Kind {
	case KindDeclaration:
		printed.WriteString(indent + node.Property + ": " + node.Value)
		if node.Important {
			printed.WriteString(" !important")
		}
		printed.WriteString(";\n")
	case KindRule:
		printed.WriteString(indent + node.Selector + " {\n")
		for _, child := range node.Nodes {
			printSignatureCss(printed, child, depth+1)
		}
		printed.WriteString(indent + "}\n")
	case KindAtRule:
		if len(node.Nodes) == 0 {
			printed.WriteString(indent + node.Name + " " + node.Params + ";\n")
			return
		}
		printed.WriteString(indent + node.Name)
		if node.Params != "" {
			printed.WriteString(" " + node.Params + " ")
		} else {
			printed.WriteString(" ")
		}
		printed.WriteString("{\n")
		for _, child := range node.Nodes {
			printSignatureCss(printed, child, depth+1)
		}
		printed.WriteString(indent + "}\n")
	case KindComment:
		printed.WriteString(indent + "/*" + node.Value + "*/\n")
	}
}

// resolveVariablesInValue is upstream's function of the same name: each `var(--key)` naming a theme key
// gains the key's value as its fallback, and a `var(--key, value)` whose fallback is exactly that value is
// replaced by the value. The replacement is walked in turn, so a theme value naming another key resolves
// that one too, and a key already resolved once in this value is left alone the second time, which is
// upstream's guard against recursion and applies to a repeat as well.
func resolveVariablesInValue(value string, theme *Theme) string {
	changed := false
	seen := map[string]bool{}
	var walk func(nodes []ValueNode) []ValueNode
	walk = func(nodes []ValueNode) []ValueNode {
		for index := 0; index < len(nodes); {
			node := &nodes[index]
			if node.Kind == ValueNodeKindFunction && node.Value == "var" && (len(node.Nodes) == 1 || len(node.Nodes) >= 3) {
				if replacement, replaced := resolveVariableNode(node, theme, seen, &changed); replaced {
					nodes = append(nodes[:index:index], append(replacement, nodes[index+1:]...)...)
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
	if !changed {
		return value
	}
	return ValueToCss(nodes)
}

// resolveVariableNode is the walk's body for one `var()`, returning its replacement when it has one.
func resolveVariableNode(node *ValueNode, theme *Theme, seen map[string]bool, changed *bool) ([]ValueNode, bool) {
	variable := node.Nodes[0].Value
	// Upstream drops the prefix as `variable.slice(`--${prefix}-`.length)`, which drops the leading
	// dashes with it, and the key then resolves nothing. Kept as written.
	if theme.Prefix != "" && strings.HasPrefix(variable, "--"+theme.Prefix+"-") {
		variable = variable[len("--"+theme.Prefix+"-"):]
	}
	variableValue, resolved := resolveThemeValue(variable, theme)
	if seen[variable] {
		return nil, false
	}
	seen[variable] = true
	if !resolved {
		return nil, false
	}

	if len(node.Nodes) == 1 {
		*changed = true
		node.Nodes = append(node.Nodes, ParseValue(","+variableValue)...)
	}
	if len(node.Nodes) >= 3 {
		if ValueToCss(node.Nodes) == node.Nodes[0].Value+","+variableValue {
			*changed = true
			return ParseValue(variableValue), true
		}
	}
	return nil, false
}

// resolveThemeValue is the design system's resolveThemeValue with forceInline on, as resolveVariablesInValue
// calls it: the key's literal value, or false when the theme does not hold it. A trailing `/ modifier`
// would apply an alpha, and a variable name cannot hold one, so it is not ported.
func resolveThemeValue(path string, theme *Theme) (string, bool) {
	return theme.Resolve("", false, []string{path}, ThemeOptionInline)
}

// The expansion maps of expand-declaration.ts. A variadic shorthand maps each of its value's
// space-separated arguments to longhands according to how many there are; a logical one copies its whole
// value to each physical longhand.
var (
	signatureVariadicExpansions = map[string]map[int][]signatureLonghand{
		"inset":               signatureQuad("top", "right", "bottom", "left"),
		"margin":              signatureQuad("margin-top", "margin-right", "margin-bottom", "margin-left"),
		"padding":             signatureQuad("padding-top", "padding-right", "padding-bottom", "padding-left"),
		"scroll-margin":       signatureQuad("scroll-margin-top", "scroll-margin-right", "scroll-margin-bottom", "scroll-margin-left"),
		"scroll-padding":      signatureQuad("scroll-padding-top", "scroll-padding-right", "scroll-padding-bottom", "scroll-padding-left"),
		"border-width":        signatureQuad("border-top-width", "border-right-width", "border-bottom-width", "border-left-width"),
		"border-style":        signatureQuad("border-top-style", "border-right-style", "border-bottom-style", "border-left-style"),
		"border-color":        signatureQuad("border-top-color", "border-right-color", "border-bottom-color", "border-left-color"),
		"gap":                 signaturePair("row-gap", "column-gap"),
		"overflow":            signaturePair("overflow-x", "overflow-y"),
		"overscroll-behavior": signaturePair("overscroll-behavior-x", "overscroll-behavior-y"),
	}
	signatureVariadicLogicalExpansions = map[string]map[int][]signatureLonghand{
		"inset-block":           signaturePair("top", "bottom"),
		"inset-inline":          signaturePair("left", "right"),
		"margin-block":          signaturePair("margin-top", "margin-bottom"),
		"margin-inline":         signaturePair("margin-left", "margin-right"),
		"padding-block":         signaturePair("padding-top", "padding-bottom"),
		"padding-inline":        signaturePair("padding-left", "padding-right"),
		"scroll-margin-block":   signaturePair("scroll-margin-top", "scroll-margin-bottom"),
		"scroll-margin-inline":  signaturePair("scroll-margin-left", "scroll-margin-right"),
		"scroll-padding-block":  signaturePair("scroll-padding-top", "scroll-padding-bottom"),
		"scroll-padding-inline": signaturePair("scroll-padding-left", "scroll-padding-right"),
	}
	signatureLogicalExpansions = map[string][]string{
		"border-block":        {"border-bottom", "border-top"},
		"border-block-color":  {"border-bottom-color", "border-top-color"},
		"border-block-style":  {"border-bottom-style", "border-top-style"},
		"border-block-width":  {"border-bottom-width", "border-top-width"},
		"border-inline":       {"border-left", "border-right"},
		"border-inline-color": {"border-left-color", "border-right-color"},
		"border-inline-style": {"border-left-style", "border-right-style"},
		"border-inline-width": {"border-left-width", "border-right-width"},
	}
)

// signatureLonghand is one longhand a variadic shorthand writes, with the argument it takes.
type signatureLonghand struct {
	property string
	argument int
}

// signatureQuad is createBareQuad: one to four arguments spread over top, right, bottom and left the way
// CSS spreads them.
func signatureQuad(top, right, bottom, left string) map[int][]signatureLonghand {
	return map[int][]signatureLonghand{
		1: {{top, 0}, {right, 0}, {bottom, 0}, {left, 0}},
		2: {{top, 0}, {right, 1}, {bottom, 0}, {left, 1}},
		3: {{top, 0}, {right, 1}, {bottom, 2}, {left, 1}},
		4: {{top, 0}, {right, 1}, {bottom, 2}, {left, 3}},
	}
}

// signaturePair is createPair: one argument for both longhands, or one each.
func signaturePair(first, second string) map[int][]signatureLonghand {
	return map[int][]signatureLonghand{
		1: {{first, 0}, {second, 0}},
		2: {{first, 0}, {second, 1}},
	}
}

// expandSignatureDeclaration is expandDeclaration: the longhands a shorthand declaration stands for, or
// nil when it is not one, or has more arguments than its map spreads.
func expandSignatureDeclaration(node *Node, features SignatureFeatures) []*Node {
	if features&SignatureLogicalToPhysical != 0 {
		if spread, isVariadic := signatureVariadicLogicalExpansions[node.Property]; isVariadic {
			return spreadLonghands(node, spread)
		}
		if longhands, isLogical := signatureLogicalExpansions[node.Property]; isLogical {
			expanded := make([]*Node, 0, len(longhands))
			for _, property := range longhands {
				expanded = append(expanded, &Node{Kind: KindDeclaration, Property: property, Value: node.Value, ValuePresent: true, Important: node.Important})
			}
			return expanded
		}
	}
	if spread, isVariadic := signatureVariadicExpansions[node.Property]; isVariadic {
		return spreadLonghands(node, spread)
	}
	return nil
}

// spreadLonghands maps a shorthand's arguments onto its longhands for their count, or nil for a count the
// map does not hold, which upstream's optional chain turns into no expansion.
func spreadLonghands(node *Node, spread map[int][]signatureLonghand) []*Node {
	arguments := segment(node.Value, ' ')
	longhands, spreads := spread[len(arguments)]
	if !spreads {
		return nil
	}
	expanded := make([]*Node, 0, len(longhands))
	for _, longhand := range longhands {
		expanded = append(expanded, &Node{
			Kind:         KindDeclaration,
			Property:     longhand.property,
			Value:        arguments[longhand.argument],
			ValuePresent: true,
			Important:    node.Important,
		})
	}
	return expanded
}
