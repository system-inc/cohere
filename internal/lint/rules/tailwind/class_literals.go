// Package tailwind holds the rules that read Tailwind class names out of source.
//
// These are the eight rules from `eslint-plugin-better-tailwindcss` plus our own, and they are the
// one namespace in the catalog that nothing external covers: rslint ports the React and core rules,
// and zero of these. They are hand-ports.
//
// They are also the rules with no compiler behind them. A wrong prop is a type error and a wrong
// import is a type error, but a wrong class name is a string that renders slightly wrong on one
// breakpoint and goes unnoticed for a month. Lint is the only verification layer the entire visual
// surface has.
package tailwind

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// ClassLiteral is one string in the source that holds Tailwind class names.
//
// Range covers the string's contents without its quotes, so a fix can rewrite the classes without
// having to reason about which quote character the author used.
type ClassLiteral struct {
	Node  *ast.Node
	Text  string
	Range core.TextRange
	// Origin says which reading rule found this literal, so a report can explain itself and a test
	// can assert that every surface is covered rather than only the obvious one.
	Origin ClassLiteralOrigin
	// Edges says which of the literal's ends will touch class text once it is substituted into a
	// template's hole, so that the whitespace at that end is all that separates two classes. See
	// holeEdges. A  operand's edges touch what it is concatenated with, upstream's
	// isConcatenatedLeft and isConcatenatedRight.
	Edges classValueEdges
	// Concatenated is which of the literal's ends a  joins to another value, the half of Edges that
	// no-concatenated-classes reports.
	Concatenated classValueEdges
}

// ClassLiteralOrigin is where a class string was written.
type ClassLiteralOrigin string

const (
	// ClassLiteralOriginAttribute is `className="..."` or `class="..."`.
	ClassLiteralOriginAttribute ClassLiteralOrigin = "Attribute"
	// ClassLiteralOriginCallee is a string argument to a known class-combining function, such as
	// `mergeClassNames('...')`.
	ClassLiteralOriginCallee ClassLiteralOrigin = "Callee"
	// ClassLiteralOriginVariable is a string assigned to a variable whose name says it holds
	// classes, such as `const buttonClassName = '...'`, or written as `export default`, which
	// upstream reads as the variable `default`.
	ClassLiteralOriginVariable ClassLiteralOrigin = "Variable"
	// ClassLiteralOriginTag is a tagged template, twc.div and its template, or a template after a
	// comment that names it, `/* tw */` before the backtick.
	ClassLiteralOriginTag ClassLiteralOrigin = "Tag"
)

// DefaultClassLiteralSettings is upstream 4.7.0's default selectors (options/default-options.js),
// embedded from the installed package by tools/generate_class_literals.
//
// Defaults rather than a required option, because a rule that declines every file when unconfigured
// is indistinguishable from a rule with nothing to report. A project writing its own `attributes`,
// `callees`, `tags` or `variables`, in a rule's options or in settings["better-tailwindcss"], replaces that
// kind's selectors whole, as upstream's legacy options do. Our own repositories name
// `mergeClassNames` and `createVariantClassNames` there.
func DefaultClassLiteralSettings() ClassLiteralSettings {
	return ClassLiteralSettings{Selectors: DefaultSelectors()}
}

// compiledSelector is one selector, its patterns and matchers compiled.
type compiledSelector struct {
	name *namePatterns
	path *namePatterns
	// matchers is nil for a selector with no matchers, which reads direct strings only.
	matchers       []compiledMatcher
	targetCall     *SelectorTarget
	targetArgument *SelectorTarget
}

func newSelectorGroup(selectors []Selector, kind SelectorKind) *selectorGroup {
	group := &selectorGroup{lowercase: kind == SelectorKindAttribute}
	for _, selector := range selectors {
		if selector.Kind != kind {
			continue
		}
		compiled := compiledSelector{
			matchers:       compileMatchers(selector.Match),
			targetCall:     selector.TargetCall,
			targetArgument: selector.TargetArgument,
		}
		if compiled.targetCall == nil {
			compiled.targetCall = selector.CallTarget
		}
		if selector.Name != "" {
			compiled.name = newNamePatterns([]string{selector.Name}, group.lowercase)
			group.hasNames = true
		}
		if selector.Path != "" {
			compiled.path = newNamePatterns([]string{selector.Path}, false)
			group.hasPaths = true
		}
		group.selectors = append(group.selectors, compiled)
	}
	return group
}

// newNamePatterns compiles patterns as JavaScript does. One that does not compile is skipped rather
// than fatal: a rule that refuses to run because one pattern in a config file is malformed reports a
// clean tree, which is the failure mode this whole tool exists to remove.
func newNamePatterns(patterns []string, lowercase bool) *namePatterns {
	compiledPatterns := &namePatterns{lowercase: lowercase}
	for _, pattern := range patterns {
		if lowercase {
			pattern = strings.ToLower(pattern)
		}
		compiled, compileError := esregexp.Compile(pattern, "")
		if compileError != nil {
			continue
		}
		compiledPatterns.compiled = append(compiledPatterns.compiled, compiled)
	}
	return compiledPatterns
}

// compiledClassLiteralSurfaces holds the surfaces for each distinct settings key, for the life of the
// process. Keyed by the settings' value, never by the options that asked, so every selection that
// merges to the same settings shares one entry and one name memo. Each is never written after it is
// stored, so the walk's workers share them freely, and the key is the whole input, so an entry cannot
// go stale.
var compiledClassLiteralSurfaces sync.Map

// ClassLiteralSurfacesFor returns the compiled surfaces for these settings, compiling them on the first
// ask for their value.
func ClassLiteralSurfacesFor(settings ClassLiteralSettings) *ClassLiteralSurfaces {
	key := settings.key()
	if existing, isCompiled := compiledClassLiteralSurfaces.Load(key); isCompiled {
		return storedClassLiteralSurfaces(key, existing)
	}
	compiled, _ := compiledClassLiteralSurfaces.LoadOrStore(key, &ClassLiteralSurfaces{
		compiled: NewClassLiteralReader(settings),
		cacheKey: "tailwind.classValues:" + key,
	})
	return storedClassLiteralSurfaces(key, compiled)
}

// storedClassLiteralSurfaces is an entry of compiledClassLiteralSurfaces as the surfaces it holds.
func storedClassLiteralSurfaces(key string, stored any) *ClassLiteralSurfaces {
	surfaces, isSurfaces := stored.(*ClassLiteralSurfaces)
	if !isSurfaces {
		// Invariant: ClassLiteralSurfacesFor is the one writer of compiledClassLiteralSurfaces, and it stores only
		// a *ClassLiteralSurfaces.
		panic(fmt.Sprintf("tailwind: the surfaces for %q are %T, not *ClassLiteralSurfaces", key, stored))
	}
	return surfaces
}

// DefaultClassLiteralSurfaces is DefaultClassLiteralSettings compiled, once for the process, for a rule
// handed no options.
var DefaultClassLiteralSurfaces = sync.OnceValue(func() *ClassLiteralSurfaces {
	return ClassLiteralSurfacesFor(DefaultClassLiteralSettings())
})

// NewClassLiteralReader compiles the settings into a reader.
func NewClassLiteralReader(settings ClassLiteralSettings) *ClassLiteralReader {
	return &ClassLiteralReader{
		attributes: newSelectorGroup(settings.Selectors, SelectorKindAttribute),
		callees:    newSelectorGroup(settings.Selectors, SelectorKindCallee),
		tags:       newSelectorGroup(settings.Selectors, SelectorKindTag),
		variables:  newSelectorGroup(settings.Selectors, SelectorKindVariable),
	}
}

// ListenerKinds are the node kinds a rule must subscribe to in order to see every class literal.
//
// Returned as a list so a rule registers the same set rather than each one remembering the kinds, and
// so a new surface reaches every rule at once, as tags and `export default` did (#btxd64n).
func ListenerKinds() []ast.Kind {
	return []ast.Kind{
		ast.KindJsxAttribute,
		ast.KindCallExpression,
		ast.KindVariableDeclaration,
		ast.KindExportAssignment,
		ast.KindTaggedTemplateExpression,
		ast.KindNoSubstitutionTemplateLiteral,
		ast.KindTemplateExpression,
	}
}

// classValues is everything one surface carries in value positions: its strings, and its templates
// with holes, each template with the position it was found in.
type classValues struct {
	literals  []ClassLiteral
	templates []classTemplateValue
}

// classTemplateValue is one template with holes found in a value position.
type classTemplateValue struct {
	node   *ast.Node
	origin ClassLiteralOrigin
	// edges says which of the template's outer ends touch class text, when it is itself inside
	// another template's hole or a  operand, and concatenated which a  joins.
	edges        classValueEdges
	concatenated classValueEdges
}

// leadingComment is upstream's getLeadingComment: the text of the comment that is the token right
// before the node, trimmed, or nothing when that token is not a comment.
//
// The trivia before the node holds two kinds to the scanner: a comment on the previous token's line is
// that token's trailing comment, and one after a line break is the node's leading comment. To ESTree's
// token list both are tokens, and the last of either is the one right before the node.
func leadingComment(node *ast.Node) string {
	sourceFile := ast.GetSourceFileOfNode(node)
	if sourceFile == nil {
		return ""
	}
	source := sourceFile.Text()
	var factory ast.NodeFactory
	var last ast.CommentRange
	found := false
	for commentRange := range scanner.GetTrailingCommentRanges(&factory, source, node.Pos()) {
		last, found = commentRange, true
	}
	for commentRange := range scanner.GetLeadingCommentRanges(&factory, source, node.Pos()) {
		if !found || commentRange.Pos() > last.Pos() {
			last, found = commentRange, true
		}
	}
	if !found {
		return ""
	}
	comment := source[last.Pos():last.End()]
	if last.Kind == ast.KindMultiLineCommentTrivia {
		comment = strings.TrimSuffix(strings.TrimPrefix(comment, "/*"), "*/")
	} else {
		comment = strings.TrimPrefix(comment, "//")
	}
	// JavaScript's trim, as upstream's `token.value.trim()`, which strips what Go's set does not, a
	// no-break space among them.
	return text.TrimWhitespace(comment)
}

// readVariableSelector reads a variable's value through one selector. One with matchers skips a value
// that is a call or a function, which a callee selector reads if anything does, and whose returns only
// an anonymousFunctionReturn matcher could otherwise reach.
func readVariableSelector(selector compiledSelector, value *ast.Node, matched *matchedNodes) {
	if selector.matchers != nil {
		switch skipOuter(value).Kind {
		case ast.KindArrowFunction, ast.KindCallExpression, ast.KindFunctionExpression:
			return
		}
	}
	readSelector(selector, value, matched)
}

// readSelector reads one value through one selector: its matchers' walk, or with none, the value
// itself when it is a string or a template.
func readSelector(selector compiledSelector, value *ast.Node, matched *matchedNodes) {
	if selector.matchers == nil {
		readDirect(value, matched)
		return
	}
	matchNodes(value, selector.matchers, matched)
}

// readDirect is upstream's getLiteralsByESExpression and getLiteralsByJSXAttributeValue: a string or a
// template, or one inside a JSX expression's braces, and nothing nested.
func readDirect(value *ast.Node, matched *matchedNodes) {
	inner := skipOuter(value)
	if inner == nil {
		return
	}
	switch inner.Kind {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression:
		matched.add(inner)
	case ast.KindJsxExpression:
		if expression := inner.AsJsxExpression().Expression; expression != nil {
			readDirect(expression, matched)
		}
	}
}

// targetItems is upstream's getTargetItems: all of them, the first, the last, or one by index,
// negative from the end, with defaultTarget when no target is written.
func targetItems(items []*ast.Node, target *SelectorTarget, defaultTarget string) []*ast.Node {
	if len(items) == 0 {
		return nil
	}
	word := defaultTarget
	if target != nil {
		word = target.Word
	}
	switch word {
	case "all":
		return items
	case "first":
		return items[:1]
	case "last":
		return items[len(items)-1:]
	}
	index := target.Index
	if index < 0 {
		index += len(items)
	}
	if index < 0 || index >= len(items) {
		return nil
	}
	return items[index : index+1]
}

// targetArguments is upstream's getTargetArguments: the arguments a target picks, each spread read as
// what it spreads. An index counts the arguments as written, a spread as one.
func targetArguments(arguments *ast.NodeList, target *SelectorTarget) []*ast.Node {
	if arguments == nil || len(arguments.Nodes) == 0 {
		return nil
	}
	unspread := func(argument *ast.Node) *ast.Node {
		if argument.Kind == ast.KindSpreadElement {
			return argument.AsSpreadElement().Expression
		}
		return argument
	}
	if target == nil || target.Word != "" {
		expressions := make([]*ast.Node, 0, len(arguments.Nodes))
		for _, argument := range arguments.Nodes {
			expressions = append(expressions, unspread(argument))
		}
		return targetItems(expressions, target, "all")
	}
	picked := targetItems(arguments.Nodes, target, "all")
	for index, argument := range picked {
		picked[index] = unspread(argument)
	}
	return picked
}

// calleeName is calleeNameAndPath's name alone, which allocates nothing.
func calleeName(callee *ast.Node) string {
	switch callee.Kind {
	case ast.KindIdentifier:
		return callee.Text()
	case ast.KindPropertyAccessExpression:
		access := callee.AsPropertyAccessExpression()
		if access.Expression == nil || skipOuter(access.Expression).Kind == ast.KindSuperKeyword {
			return ""
		}
		if access.Name() != nil && access.Name().Kind == ast.KindIdentifier {
			return access.Name().Text()
		}
	case ast.KindElementAccessExpression:
		access := callee.AsElementAccessExpression()
		if access.Expression == nil || skipOuter(access.Expression).Kind == ast.KindSuperKeyword {
			return ""
		}
		if argument := skipOuter(access.ArgumentExpression); argument != nil && argument.Kind == ast.KindStringLiteral {
			return argument.Text()
		}
	}
	return ""
}

// calleeNameAndPath is upstream's getESCalleeName (parsers/es.js at 4.7.0) for both of its readings:
// the name is an identifier's text or the last property of a member access, and the path is the whole
// chain joined by dots, present only when every link in it has a name. `this.cn` has the name `cn` and
// no path, and `super.cn` has neither. A computed access counts when its key is a plain string.
func calleeNameAndPath(callee *ast.Node) (string, string) {
	inner := skipOuter(callee)
	switch inner.Kind {
	case ast.KindIdentifier:
		return inner.Text(), inner.Text()

	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		property := calleeName(inner)
		var object *ast.Node
		if inner.Kind == ast.KindPropertyAccessExpression {
			object = inner.AsPropertyAccessExpression().Expression
		} else {
			object = inner.AsElementAccessExpression().Expression
		}
		if property == "" {
			return "", ""
		}
		_, objectPath := calleeNameAndPath(object)
		if objectPath == "" {
			return property, ""
		}
		return property, objectPath + "." + property
	}
	return "", ""
}

// classValuesOf is what a surface's selectors matched, in source order, each string with the edges
// that touch other class text and each template with holes with its own.
func classValuesOf(matched matchedNodes, origin ClassLiteralOrigin) classValues {
	if len(matched) == 0 {
		return classValues{}
	}
	slices.SortFunc(matched, func(left, right *ast.Node) int { return left.Pos() - right.Pos() })
	values := classValues{}
	for _, node := range matched {
		concatenated := concatenationEdges(node)
		edges := holeEdgesOf(node)
		edges.Leading = edges.Leading || concatenated.Leading
		edges.Trailing = edges.Trailing || concatenated.Trailing
		if node.Kind == ast.KindTemplateExpression {
			values.templates = append(
				values.templates,
				classTemplateValue{node: node, origin: origin, edges: edges, concatenated: concatenated},
			)
			continue
		}
		literal := classLiteralFrom(node, origin)
		literal.Edges = edges
		literal.Concatenated = concatenated
		values.literals = append(values.literals, literal)
	}
	return values
}

// concatenationEdges is upstream's getStringConcatenationMeta: a value is concatenated on its left when
// it, or anything around it, is the right side of a `+`, and on its right when the left side. Upstream
// asks every ancestor up to the file, through calls too, and so does this.
func concatenationEdges(node *ast.Node) classValueEdges {
	edges := classValueEdges{}
	for child, parent := node, esParent(node); parent != nil; child, parent = parent, esParent(parent) {
		if operator, isBinary := isEsBinaryExpression(parent); !isBinary || operator != ast.KindPlusToken {
			continue
		}
		binary := parent.AsBinaryExpression()
		if skipOuter(binary.Right) == child {
			edges.Leading = true
		}
		if skipOuter(binary.Left) == child {
			edges.Trailing = true
		}
	}
	return edges
}

// holeEdgesOf is which ends of a value touch the text of the template whose hole holds it, from the
// nearest hole above it, short of a boundary that starts another reading: a call, a function, a
// declaration or an attribute. See holeEdges.
func holeEdgesOf(node *ast.Node) classValueEdges {
	for child, parent := node, node.Parent; parent != nil; child, parent = parent, parent.Parent {
		switch parent.Kind {
		case ast.KindCallExpression, ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindVariableDeclaration,
			ast.KindJsxAttribute, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
			return classValueEdges{}
		case ast.KindTemplateSpan:
			span := parent.AsTemplateSpan()
			if span.Expression != child {
				return classValueEdges{}
			}
			template := parent.Parent.AsTemplateExpression()
			spans := template.TemplateSpans.Nodes
			index := slices.Index(spans, parent)
			before := template.Head.Text()
			if index > 0 {
				before = spans[index-1].AsTemplateSpan().Literal.Text()
			}
			return holeEdges(before, span.Literal.Text(), index == 0, index == len(spans)-1, holeEdgesOf(parent.Parent))
		}
	}
	return classValueEdges{}
}

// classValueEdges says which ends of a value touch class text once it is substituted, so that the
// whitespace at that end is the only thing between two classes and must not be trimmed away.
//
// A value on a surface touches nothing at either end. A value inside a template's hole touches the
// text on each side of the hole unless that text supplies whitespace of its own.
type classValueEdges struct {
	Leading  bool
	Trailing bool
}

// holeEdges is which ends of a hole's value touch class text, from the template text either side.
//
// In `flex ${open ? ' hidden ' : ”}` the text before the hole ends with a space, so the value's
// leading space separates nothing and can go, and nothing follows the hole, so its trailing space
// can go too: the result is `'hidden'`, which is what Prettier's Tailwind plugin writes. In
// `flex${open ? ' hidden' : ”}` the leading space is the only thing keeping `hidden` from fusing
// with `flex`, so it stays.
//
// The plugin trims there too, and that is a defect in it rather than a convention to copy: its
// `canCollapseWhitespaceIn` tests whether the quasi before a hole starts with whitespace where the
// question is whether it ends with it, so it rewrites `flex${c ? '  block  ' : ”}` to
// `flex${c ? 'block' : ”}` and the two classes become `flexblock`. Measured 2026-10-02 against
// 0.8.1. This matches it everywhere the trim is safe, which is every hole string the gate found, and
// declines only the rewrites that change what renders.
//
// An empty run between two holes touches the neighbouring hole's value, which nothing here can
// read, so it counts as touching. An empty run at a template's own end inherits that template's
// edges, which is what makes a template nested inside another template's hole come out right.
func holeEdges(before string, after string, firstHole bool, lastHole bool, template classValueEdges) classValueEdges {
	edges := classValueEdges{}
	switch {
	case before == "" && firstHole:
		edges.Leading = template.Leading
	case before == "":
		edges.Leading = true
	default:
		edges.Leading = !isSpace(rune(before[len(before)-1]))
	}
	switch {
	case after == "" && lastHole:
		edges.Trailing = template.Trailing
	case after == "":
		edges.Trailing = true
	default:
		edges.Trailing = !isSpace(rune(after[0]))
	}
	return edges
}

// classLiteralFrom records the literal and the span of its contents.
//
// The range excludes the surrounding quotes so that a fix replaces class text and nothing else. Any
// literal whose source form is shorter than two characters cannot have quotes to strip, so it is
// reported with its own range rather than a negative one.
//
// The range starts at the literal's token, not at its Loc. Loc includes the whitespace before the
// literal, so for one on its own line or after a comma the quote strip cut the opening quote, the
// fixed file stopped parsing, and the edit engine refused that file's whole fix batch, every other
// rule's fixes included (#vf1hd6j, measured at 174 files in ahra).
func classLiteralFrom(literal *ast.Node, origin ClassLiteralOrigin) ClassLiteral {
	contentRange := rule.TokenRange(ast.GetSourceFileOfNode(literal), literal)
	if contentRange.End()-contentRange.Pos() >= 2 {
		contentRange = core.NewTextRange(contentRange.Pos()+1, contentRange.End()-1)
	}

	return ClassLiteral{
		Node:   literal,
		Text:   literal.Text(),
		Range:  contentRange,
		Origin: origin,
	}
}

// SplitClasses breaks a class string the way Tailwind reads it.
//
// Fragments containing `${` are dropped rather than treated as classes. A literal that survived to
// here with a hole in it is only partly known at lint time, and reporting on the visible half would
// produce findings the author cannot act on.
func SplitClasses(classString string) []string {
	var classes []string
	for _, field := range text.WhitespaceFields(classString) {
		if strings.Contains(field, "${") {
			continue
		}
		classes = append(classes, field)
	}
	return classes
}
