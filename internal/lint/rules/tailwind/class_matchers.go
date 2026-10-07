package tailwind

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"
)

// This file is better-tailwindcss 4.7.0's matcher engine (utils/matchers.js getLiteralNodesByMatchers
// and parsers/es.js getESMatcherFunctions), walking typescript-go's tree where upstream walks ESTree.
//
// The two trees disagree in ways that matter here, and each is answered once, in esParent and
// skipOuter: typescript-go keeps parentheses, a computed key's brackets and a template's spans as
// nodes, and ESTree has none of them, so `{a: ({b: 'x'})}` is the path `a.b` to upstream and would be
// cut short at the parentheses by a reader that took typescript-go's parents as they are. A method is
// one node in typescript-go and a key beside a function expression in ESTree, so its key is read and
// its body is not. A template literal is both the literal and, for each run of text, a quasi, and the
// two answer differently: a computed key `[`x`]` is an object key as a literal and never as a quasi.
//
// Every exclusion upstream makes is a question about every ancestor up to the file, not up to the
// selector's root: a string in `cn('p-2') === x` is in a comparison, and is never read.

// compiledMatcher is one matcher, its path compiled.
type compiledMatcher struct {
	matcherType MatcherType
	// path is nil when the matcher filters nothing, and also when its pattern does not compile, which
	// then matches no path rather than refusing the run, as an uncompilable name does (newNamePatterns).
	path    *esregexp.RegExp
	hasPath bool
	// inner is an anonymousFunctionReturn matcher's own matchers, run on what the function returns.
	inner []compiledMatcher
}

func compileMatchers(matchers []SelectorMatcher) []compiledMatcher {
	if matchers == nil {
		return nil
	}
	compiled := make([]compiledMatcher, 0, len(matchers))
	for _, matcher := range matchers {
		entry := compiledMatcher{matcherType: matcher.Type, hasPath: matcher.Path != "", inner: compileMatchers(matcher.Match)}
		if entry.hasPath {
			entry.path, _ = esregexp.Compile(matcher.Path, "")
		}
		compiled = append(compiled, entry)
	}
	return compiled
}

// matchesPath is upstream's check after a key or value is found: no path, or no path filter, is a
// match, and otherwise the filter must be found somewhere in the path.
func (matcher compiledMatcher) matchesPath(path string) bool {
	if path == "" || !matcher.hasPath {
		return true
	}
	if matcher.path == nil {
		return false
	}
	return matcher.path.Test(path)
}

// matchedNodes collects what a walk matched: a string literal, or a template literal whose text was
// matched, either as itself or as its runs.
type matchedNodes []*ast.Node

func (matched *matchedNodes) add(node *ast.Node) {
	for _, existing := range *matched {
		if existing == node {
			return
		}
	}
	*matched = append(*matched, node)
}

// matchNodes is upstream's getLiteralNodesByMatchers: the root asked first, then everything under it
// the matchers that survive can reach.
//
// Two walks answer it. Matchers that never nest walk with a bitmask and no allocation per node, which is
// every selector but twc's and twx's. A list holding an anonymousFunctionReturn matcher walks with
// matchers that make matchers, generalWalker.
func matchNodes(root *ast.Node, matchers []compiledMatcher, matched *matchedNodes) {
	if root == nil || root.Parent == nil || len(matchers) == 0 {
		return
	}
	for _, matcher := range matchers {
		if matcher.matcherType == MatcherTypeAnonymousFunctionReturn {
			matchNodesNesting(root, matchers, matched)
			return
		}
	}
	walker := &matcherWalker{matchers: matchers, matched: matched}
	walker.visitChild = walker.visit
	walker.live = uint64(1)<<len(matchers) - 1
	walker.visit(root)
}

// matcherWalker is one walk's state. Its one visitor is made once per walk, since a closure per node
// would be an allocation for every node under every class surface in the file.
type matcherWalker struct {
	matchers   []compiledMatcher
	matched    *matchedNodes
	live       uint64
	visitChild func(*ast.Node) bool
}

// visit is upstream's findMatchingNestedNodes for the three matchers that never nest: a node is asked
// by every matcher still live, kept when one matches, and entered by those not stopped at it.
func (walker *matcherWalker) visit(node *ast.Node) bool {
	if node == nil {
		return false
	}
	isMatch, survivors := evaluateMatchers(node, walker.live, walker.matchers)
	if isMatch {
		walker.matched.add(node)
	}
	if survivors == 0 {
		return false
	}
	live := walker.live
	walker.live = survivors
	walker.descend(node)
	walker.live = live
	return false
}

// descend enters a node's children as ESTree has them.
func (walker *matcherWalker) descend(node *ast.Node) {
	switch node.Kind {
	// ESTree's method is a key beside a function expression, and every matcher stops at a function
	// expression, so only the key is entered.
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		walker.visit(node.Name())
		return
	case ast.KindConstructor:
		return

	// A template's runs are asked as quasis, all at once since they share every ancestor, and its holes
	// are entered as any expression is.
	case ast.KindTemplateExpression:
		if isMatch, _ := evaluateQuasi(node, walker.live, walker.matchers); isMatch {
			walker.matched.add(node)
		}
		template := node.AsTemplateExpression()
		if template.TemplateSpans != nil {
			for _, span := range template.TemplateSpans.Nodes {
				walker.visit(span.AsTemplateSpan().Expression)
			}
		}
		return

	// A template with no holes is one quasi in ESTree, a child of the literal.
	case ast.KindNoSubstitutionTemplateLiteral:
		if isMatch, _ := evaluateQuasi(node, walker.live, walker.matchers); isMatch {
			walker.matched.add(node)
		}
		return
	}

	node.ForEachChild(walker.visitChild)
}

// evaluateMatchers asks every live matcher about one node, and returns whether any matched and which
// are still live below it.
func evaluateMatchers(node *ast.Node, live uint64, matchers []compiledMatcher) (bool, uint64) {
	// The three matchers stop at the same four kinds of node, upstream's UNCROSSABLE_BOUNDARY.
	if isUncrossable(node) {
		return false, 0
	}
	// Only a string, or a template as a literal, can be matched in a way that yields class text. Any
	// other node is NO_MATCH to all three, or a match that reads nothing, such as an identifier key.
	isTemplate := node.Kind == ast.KindTemplateExpression || node.Kind == ast.KindNoSubstitutionTemplateLiteral
	if node.Kind != ast.KindStringLiteral && !isTemplate {
		return false, live
	}

	isMatch := false
	for index, matcher := range matchers {
		if live&(1<<index) == 0 {
			continue
		}
		switch matcher.matcherType {
		case MatcherTypeStrings:
			// A template as a literal is never string-like; its runs are, and are asked as quasis.
			if !isTemplate && stringsMatcherReads(node, node) {
				isMatch = true
			}
		case MatcherTypeObjectKeys:
			if isESObjectKey(node) &&
				!isInsideDisallowedBinaryExpression(node) &&
				!isInsideConditionalExpressionTest(node) &&
				!isInsideLogicalExpressionLeft(node) &&
				!isInsideMemberExpression(node) &&
				!isIndexedAccessLiteral(node) &&
				matcher.matchesPath(objectPath(node, false)) {
				isMatch = true
			}
		case MatcherTypeObjectValues:
			if !isTemplate && objectValuesMatcherReads(node, node) && matcher.matchesPath(objectPath(node, false)) {
				isMatch = true
			}
		}
	}
	return isMatch, live
}

// evaluateQuasi asks the live matchers about a template's runs. A quasi is string-like, never an
// object key and never an indexed access, and its every ancestor is the template's, so the
// questions about ancestors are asked from the template.
func evaluateQuasi(template *ast.Node, live uint64, matchers []compiledMatcher) (bool, uint64) {
	isMatch := false
	for index, matcher := range matchers {
		if live&(1<<index) == 0 {
			continue
		}
		switch matcher.matcherType {
		case MatcherTypeStrings:
			if stringsMatcherReads(template, nil) {
				isMatch = true
			}
		case MatcherTypeObjectValues:
			if objectValuesMatcherReads(template, nil) && matcher.matchesPath(objectPath(template, true)) {
				isMatch = true
			}
		}
	}
	return isMatch, live
}

// liveMatcher is one matcher alive in a walk that nests: one of the selector's own, or one an
// anonymousFunctionReturn matcher made, upstream's closures in getESMatcherFunctions.
type liveMatcher struct {
	kind liveMatcherKind
	// matcher is a selector's own matcher, for liveMatcherSelector.
	matcher *compiledMatcher
	// inner is what a made matcher hands on once it finds the returned value.
	inner []compiledMatcher
}

type liveMatcherKind int

const (
	// liveMatcherSelector is a matcher the selector wrote.
	liveMatcherSelector liveMatcherKind = iota
	// liveMatcherArrowBody finds the body of an arrow function with no braces, the value it returns.
	liveMatcherArrowBody
	// liveMatcherReturnStatement finds a return statement, short of another function, call or
	// declarator.
	liveMatcherReturnStatement
)

// matcherResult is a matcher's answer about one node: upstream's MATCH, NO_MATCH, UNCROSSABLE_BOUNDARY,
// or a list of matchers to ask in its place.
type matcherResult int

const (
	matcherNoMatch matcherResult = iota
	matcherMatch
	matcherUncrossable
	matcherNested
)

// esNode is a node as ESTree has it, where typescript-go has one node for two of ESTree's: a method,
// which ESTree splits into a key and a function expression, and a template literal, which ESTree
// splits into the literal and its quasis.
type esNode struct {
	node *ast.Node
	// asFunction is a method read as its function expression.
	asFunction bool
	// asQuasi is a template literal read as its runs.
	asQuasi bool
}

// matchNodesNesting is getLiteralNodesByMatchers where matchers can make matchers. The root is asked
// once, every answer kept: what survives and what was made are both handed to the walk below.
func matchNodesNesting(root *ast.Node, matchers []compiledMatcher, matched *matchedNodes) {
	walker := &nestingWalker{matched: matched}
	live := make([]liveMatcher, 0, len(matchers))
	for index := range matchers {
		live = append(live, liveMatcher{kind: liveMatcherSelector, matcher: &matchers[index]})
	}
	var below []liveMatcher
	isMatch := false
	for _, matcher := range live {
		result, nested := matcher.evaluate(esNode{node: root})
		switch result {
		case matcherMatch:
			isMatch = true
			below = append(below, matcher)
		case matcherNoMatch:
			below = append(below, matcher)
		case matcherNested:
			below = append(below, nested...)
		}
	}
	if isMatch {
		matched.add(root)
	}
	walker.descend(esNode{node: root}, below)
}

// nestingWalker is upstream's findMatchingNestedNodes where matchers can make matchers.
type nestingWalker struct {
	matched *matchedNodes
}

// visit asks one node. Upstream asks it until no matcher makes another: the matchers made are asked of
// the same node in place of every matcher that was there, and only the last round's survivors go below.
func (walker *nestingWalker) visit(node esNode, live []liveMatcher) {
	if node.node == nil {
		return
	}
	isMatch := false
	current := live
	for len(current) > 0 {
		var next, nested []liveMatcher
		for _, matcher := range current {
			result, made := matcher.evaluate(node)
			switch result {
			case matcherMatch:
				isMatch = true
				next = append(next, matcher)
			case matcherNoMatch:
				next = append(next, matcher)
			case matcherNested:
				nested = append(nested, made...)
			}
		}
		if len(nested) > 0 {
			current = nested
			continue
		}
		current = next
		break
	}
	if isMatch {
		walker.matched.add(node.node)
	}
	if len(current) > 0 {
		walker.descend(node, current)
	}
}

// descend enters a node's children as ESTree has them.
func (walker *nestingWalker) descend(node esNode, live []liveMatcher) {
	if node.asQuasi {
		return
	}
	if node.asFunction {
		for _, parameter := range node.node.Parameters() {
			walker.visit(esNode{node: parameter}, live)
		}
		walker.visit(esNode{node: node.node.Type()}, live)
		walker.visit(esNode{node: node.node.Body()}, live)
		return
	}
	switch node.node.Kind {
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		walker.visit(esNode{node: node.node.Name()}, live)
		walker.visit(esNode{node: node.node, asFunction: true}, live)
		return
	case ast.KindConstructor:
		walker.visit(esNode{node: node.node, asFunction: true}, live)
		return
	case ast.KindTemplateExpression:
		walker.visit(esNode{node: node.node, asQuasi: true}, live)
		if spans := node.node.AsTemplateExpression().TemplateSpans; spans != nil {
			for _, span := range spans.Nodes {
				walker.visit(esNode{node: span.AsTemplateSpan().Expression}, live)
			}
		}
		return
	case ast.KindNoSubstitutionTemplateLiteral:
		walker.visit(esNode{node: node.node, asQuasi: true}, live)
		return
	}
	node.node.ForEachChild(func(child *ast.Node) bool {
		walker.visit(esNode{node: child}, live)
		return false
	})
}

// evaluate is one matcher's answer about one node, and the matchers it makes when it nests.
func (matcher liveMatcher) evaluate(node esNode) (matcherResult, []liveMatcher) {
	switch matcher.kind {
	case liveMatcherArrowBody:
		// The returned value is the body of an arrow with no braces.
		if node.asFunction || node.asQuasi {
			return matcherNoMatch, nil
		}
		parent := esParent(node.node)
		if parent != nil && parent.Kind == ast.KindArrowFunction && parent.Body().Kind != ast.KindBlock &&
			skipOuter(parent.Body()) == node.node {
			return matcherNested, selectorMatchers(matcher.inner)
		}
		return matcherNoMatch, nil

	case liveMatcherReturnStatement:
		if node.asFunction {
			return matcherUncrossable, nil
		}
		if node.asQuasi {
			return matcherNoMatch, nil
		}
		switch node.node.Kind {
		case ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindFunctionDeclaration, ast.KindVariableDeclaration:
			return matcherUncrossable, nil
		case ast.KindCallExpression:
			if isUncrossable(node.node) {
				return matcherUncrossable, nil
			}
		case ast.KindReturnStatement:
			return matcherNested, selectorMatchers(matcher.inner)
		}
		return matcherNoMatch, nil
	}

	selector := matcher.matcher
	if selector.matcherType == MatcherTypeAnonymousFunctionReturn {
		if !node.asFunction && !node.asQuasi &&
			(node.node.Kind == ast.KindVariableDeclaration || node.node.Kind == ast.KindCallExpression && isUncrossable(node.node)) {
			return matcherUncrossable, nil
		}
		switch {
		case node.asFunction:
			return matcherNested, []liveMatcher{{kind: liveMatcherReturnStatement, inner: selector.inner}}
		case node.asQuasi:
			return matcherNoMatch, nil
		case node.node.Kind == ast.KindArrowFunction:
			if node.node.Body().Kind != ast.KindBlock {
				return matcherNested, []liveMatcher{{kind: liveMatcherArrowBody, inner: selector.inner}}
			}
			return matcherNested, []liveMatcher{{kind: liveMatcherReturnStatement, inner: selector.inner}}
		case node.node.Kind == ast.KindFunctionExpression && node.node.Name() == nil:
			return matcherNested, []liveMatcher{{kind: liveMatcherReturnStatement, inner: selector.inner}}
		}
		return matcherNoMatch, nil
	}

	// The three matchers that never nest stop at the same four kinds, a method's function among them.
	if node.asFunction || (!node.asQuasi && isUncrossable(node.node)) {
		return matcherUncrossable, nil
	}
	if node.asQuasi {
		switch selector.matcherType {
		case MatcherTypeStrings:
			if stringsMatcherReads(node.node, nil) {
				return matcherMatch, nil
			}
		case MatcherTypeObjectValues:
			if objectValuesMatcherReads(node.node, nil) && selector.matchesPath(objectPath(node.node, true)) {
				return matcherMatch, nil
			}
		}
		return matcherNoMatch, nil
	}
	if isMatch, _ := evaluateMatchers(node.node, 1, []compiledMatcher{*selector}); isMatch {
		return matcherMatch, nil
	}
	return matcherNoMatch, nil
}

// selectorMatchers is matchers a selector wrote, alive in a nesting walk.
func selectorMatchers(matchers []compiledMatcher) []liveMatcher {
	live := make([]liveMatcher, 0, len(matchers))
	for index := range matchers {
		live = append(live, liveMatcher{kind: liveMatcherSelector, matcher: &matchers[index]})
	}
	return live
}

// stringsMatcherReads is the strings matcher's exclusions for a string-like node. from is where the
// ancestors are asked from; own is the node itself when it is a string literal, and nil for a quasi,
// which is never an object key nor an indexed access.
func stringsMatcherReads(from *ast.Node, own *ast.Node) bool {
	if own != nil && (isIndexedAccessLiteral(own) || isESObjectKey(own)) {
		return false
	}
	return !isInsideDisallowedBinaryExpression(from) &&
		!isInsideConditionalExpressionTest(from) &&
		!isInsideLogicalExpressionLeft(from) &&
		!isInsideObjectValue(from)
}

// objectValuesMatcherReads is the object values matcher's conditions for a string-like node, with
// from and own as stringsMatcherReads takes them.
func objectValuesMatcherReads(from *ast.Node, own *ast.Node) bool {
	if own != nil && (isESObjectKey(own) || isIndexedAccessLiteral(own)) {
		return false
	}
	return isInsideObjectValue(from) &&
		!isInsideDisallowedBinaryExpression(from) &&
		!isInsideConditionalExpressionTest(from) &&
		!isInsideLogicalExpressionLeft(from)
}

// isUncrossable is the four node kinds no matcher walks into: a call, an arrow function, a variable
// declarator and a function expression. `import('x')` is a call to typescript-go and an
// ImportExpression to ESTree, which no matcher stops at.
func isUncrossable(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindCallExpression:
		return node.AsCallExpression().Expression.Kind != ast.KindImportKeyword
	case ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindVariableDeclaration:
		return true
	}
	return false
}

// isTransparent is a node ESTree does not have: parentheses, a computed key's brackets, and a
// template span. Its child is its parent's child in ESTree.
func isTransparent(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindParenthesizedExpression, ast.KindComputedPropertyName, ast.KindTemplateSpan:
		return true
	}
	return false
}

// esParent is a node's parent in ESTree.
func esParent(node *ast.Node) *ast.Node {
	parent := node.Parent
	for parent != nil && isTransparent(parent) {
		parent = parent.Parent
	}
	return parent
}

// skipOuter is the node ESTree has where typescript-go has a child: inside any parentheses, and inside
// a computed key's brackets. Comparing a child against skipOuter of a parent's field is ESTree's
// `parent.field === node`.
func skipOuter(node *ast.Node) *ast.Node {
	for node != nil {
		switch node.Kind {
		case ast.KindParenthesizedExpression:
			node = node.AsParenthesizedExpression().Expression
		case ast.KindComputedPropertyName:
			node = node.AsComputedPropertyName().Expression
		default:
			return node
		}
	}
	return nil
}

// isProperty is ESTree's Property: a property, shorthand or not, and a method or accessor written in
// an object.
func isProperty(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment,
		ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		return node.Parent != nil && node.Parent.Kind == ast.KindObjectLiteralExpression
	}
	return false
}

// propertyKey is an ESTree Property's key.
func propertyKey(property *ast.Node) *ast.Node {
	return skipOuter(property.Name())
}

// isESObjectKey is upstream's isESObjectKey: the key of a property in an object literal.
func isESObjectKey(node *ast.Node) bool {
	parent := esParent(node)
	return parent != nil && isProperty(parent) && propertyKey(parent) == node
}

// isIndexedAccessLiteral is upstream's: a string that is the key of a computed member access,
// `map['p-2']`.
func isIndexedAccessLiteral(node *ast.Node) bool {
	if node.Kind != ast.KindStringLiteral {
		return false
	}
	parent := esParent(node)
	return parent != nil && parent.Kind == ast.KindElementAccessExpression &&
		skipOuter(parent.AsElementAccessExpression().ArgumentExpression) == node
}

// binaryOperator is a binary expression's operator, and whether ESTree calls it a BinaryExpression
// rather than a logical, assignment or sequence expression.
func isESBinaryExpression(node *ast.Node) (ast.Kind, bool) {
	if node.Kind != ast.KindBinaryExpression {
		return 0, false
	}
	operator := node.AsBinaryExpression().OperatorToken.Kind
	if ast.IsLogicalOrCoalescingBinaryOperator(operator) || ast.IsAssignmentOperator(operator) || operator == ast.KindCommaToken {
		return operator, false
	}
	return operator, true
}

// isInsideDisallowedBinaryExpression is upstream's: any ancestor a binary expression other than `+`.
func isInsideDisallowedBinaryExpression(node *ast.Node) bool {
	for parent := esParent(node); parent != nil; parent = esParent(parent) {
		if operator, isBinary := isESBinaryExpression(parent); isBinary && operator != ast.KindPlusToken {
			return true
		}
	}
	return false
}

// isInsideConditionalExpressionTest is upstream's: any ancestor the test of a conditional.
func isInsideConditionalExpressionTest(node *ast.Node) bool {
	for child, parent := node, esParent(node); parent != nil; child, parent = parent, esParent(parent) {
		if parent.Kind == ast.KindConditionalExpression && skipOuter(parent.AsConditionalExpression().Condition) == child {
			return true
		}
	}
	return false
}

// isInsideLogicalExpressionLeft is upstream's: any ancestor the left side of `&&`, `||` or `??`.
func isInsideLogicalExpressionLeft(node *ast.Node) bool {
	for child, parent := node, esParent(node); parent != nil; child, parent = parent, esParent(parent) {
		if parent.Kind != ast.KindBinaryExpression {
			continue
		}
		binary := parent.AsBinaryExpression()
		if ast.IsLogicalOrCoalescingBinaryOperator(binary.OperatorToken.Kind) && skipOuter(binary.Left) == child {
			return true
		}
	}
	return false
}

// isInsideMemberExpression is upstream's: any ancestor a member access.
func isInsideMemberExpression(node *ast.Node) bool {
	for parent := esParent(node); parent != nil; parent = esParent(parent) {
		if parent.Kind == ast.KindPropertyAccessExpression || parent.Kind == ast.KindElementAccessExpression {
			return true
		}
	}
	return false
}

// isInsideObjectValue is upstream's: the nearest property above, short of a call or function, holds
// the node in its value. A method is a function to what its body holds and not to its key, since in
// ESTree the key sits beside the function expression and the body inside it.
func isInsideObjectValue(node *ast.Node) bool {
	for current, previous := node, (*ast.Node)(nil); current != nil; {
		switch current.Kind {
		case ast.KindCallExpression, ast.KindArrowFunction, ast.KindFunctionExpression:
			return false
		case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindConstructor:
			if previous != nil && previous != current.Name() {
				return false
			}
		}
		parent := esParent(current)
		if parent == nil {
			return false
		}
		if parent.Kind == ast.KindPropertyAssignment && isProperty(parent) &&
			skipOuter(parent.AsPropertyAssignment().Initializer) == current {
			return true
		}
		// The child the method is reached from, inside any transparent node between them, so a computed
		// key's brackets still read as the key.
		previous = current
		for previous.Parent != nil && previous.Parent != parent {
			previous = previous.Parent
		}
		current = parent
	}
	return false
}

// objectPath is upstream's getESObjectPath: where a key or value sits in the object around it,
// `variants.size.sm`, `compoundVariants[0].class`, `["data-x"].y`. Upstream returns undefined or ""
// where it cannot say, and a matcher treats both as matching any path, so both are "" here.
//
// asQuasi asks for a template's runs, which upstream reaches as TemplateElements.
func objectPath(node *ast.Node, asQuasi bool) string {
	if asQuasi {
		// A quasi in an object value takes its property's path. Anywhere else its parent is the
		// template, which upstream cannot place, so it has none.
		if isInsideObjectValue(node) {
			return objectPath(nearestProperty(node), false)
		}
		return ""
	}
	if node.Parent == nil || !hasObjectPath(node) {
		return ""
	}

	var elements []string
	if isProperty(node) {
		key, isNamed := propertyKeyName(propertyKey(node))
		if !isNamed {
			return ""
		}
		elements = append(elements, objectPathElement(key))
	}
	if node.Kind == ast.KindStringLiteral && isInsideObjectValue(node) {
		if property := nearestProperty(node); property != nil {
			return objectPath(property, false)
		}
	}
	if isESObjectKey(node) {
		return objectPath(esParent(node), false)
	}
	parent := esParent(node)
	if parent != nil && parent.Kind == ast.KindArrayLiteralExpression && !isProperty(node) {
		for index, element := range parent.AsArrayLiteralExpression().Elements.Nodes {
			if skipOuter(element) == node {
				elements = append([]string{"[" + strconv.Itoa(index) + "]"}, elements...)
				break
			}
		}
	}
	if parent != nil {
		elements = append([]string{objectPath(parent, false)}, elements...)
	}

	var joined strings.Builder
	for _, element := range elements {
		if element == "" {
			continue
		}
		if joined.Len() > 0 && !(strings.HasPrefix(element, "[") && strings.HasSuffix(element, "]")) {
			joined.WriteByte('.')
		}
		joined.WriteString(element)
	}
	return joined.String()
}

// hasObjectPath is the node kinds upstream builds a path through: a property, an object, an array,
// an identifier and a literal.
func hasObjectPath(node *ast.Node) bool {
	if isProperty(node) {
		return true
	}
	switch node.Kind {
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression, ast.KindIdentifier,
		ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindRegularExpressionLiteral,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword:
		return true
	}
	return false
}

// nearestProperty is upstream's findMatchingParentNodes for a Property: the nearest property above.
func nearestProperty(node *ast.Node) *ast.Node {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if isProperty(parent) {
			return parent
		}
	}
	return nil
}

// propertyKeyName is how upstream names a key in a path: an identifier by its name, computed or not,
// and a literal by its value as a string. Any other key, a member access or a template, cuts the path
// to "" there.
func propertyKeyName(key *ast.Node) (string, bool) {
	if key == nil {
		return "", false
	}
	switch key.Kind {
	case ast.KindIdentifier, ast.KindStringLiteral:
		return key.Text(), true
	case ast.KindNumericLiteral:
		return javaScriptNumberString(key.Text()), true
	case ast.KindBigIntLiteral:
		return strings.TrimSuffix(key.Text(), "n"), true
	case ast.KindTrueKeyword:
		return "true", true
	case ast.KindFalseKeyword:
		return "false", true
	// `null?.toString() ?? raw`: a null key's value has no string, so its source does.
	case ast.KindNullKeyword:
		return "null", true
	case ast.KindRegularExpressionLiteral:
		return key.Text(), true
	}
	return "", false
}

// objectPathIdentifier is upstream's createObjectPathElement test, `^[A-Z_a-z]\w*$`.
var objectPathIdentifier = regexp.MustCompile(`^[A-Z_a-z][0-9A-Z_a-z]*$`)

// objectPathElement is upstream's createObjectPathElement: a name that is an identifier as it is, and
// any other in brackets and quotes, unescaped, as upstream writes it.
func objectPathElement(name string) string {
	if name == "" {
		return ""
	}
	if objectPathIdentifier.MatchString(name) {
		return name
	}
	return `["` + name + `"]`
}

// javaScriptNumberString is a numeric literal's value as JavaScript's String() writes it, `1.0` as
// `1` and `0x10` as `16`, for a numeric key in a path.
func javaScriptNumberString(text string) string {
	digits := strings.ToLower(strings.ReplaceAll(text, "_", ""))
	var value float64
	switch {
	case strings.HasPrefix(digits, "0x"), strings.HasPrefix(digits, "0o"), strings.HasPrefix(digits, "0b"):
		integer, err := strconv.ParseUint(digits, 0, 64)
		if err != nil {
			return text
		}
		value = float64(integer)
	default:
		parsed, err := strconv.ParseFloat(digits, 64)
		if err != nil {
			return text
		}
		value = parsed
	}
	magnitude := math.Abs(value)
	if value == 0 || (magnitude >= 1e-6 && magnitude < 1e21) {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	formatted := strconv.FormatFloat(value, 'e', -1, 64)
	// JavaScript writes 1e-7 where Go writes 1e-07.
	mantissa, exponent, _ := strings.Cut(formatted, "e")
	sign := exponent[:1]
	exponent = strings.TrimLeft(exponent[1:], "0")
	return mantissa + "e" + sign + exponent
}
