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
	"encoding/json"
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

// ClassLiteralSettings is the selectors a rule reads class strings through, upstream's flat
// `selectors` after its legacy options and settings have merged into them.
//
// Every surface matters, and that is not obvious. On the ahra tree the attribute surface holds 7,773
// literals and the others hold 2,192, so a rule that reads only JSX attributes silently covers 78% of
// the class surface while reporting a clean tree for the rest. Every rule in this package reads
// through this type for that reason.
type ClassLiteralSettings struct {
	Selectors []Selector
}

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

// TailwindClassLiteralOptions are the selector options every better-tailwindcss rule accepts: the flat
// `selectors`, and the legacy `attributes`, `callees`, `tags` and `variables`, each a list of names,
// or of names with their matchers.
//
// A legacy kind that is written replaces that kind's selectors whole, an empty list included, which
// then reads nothing of that kind; a kind not written keeps the flat selectors of its kind, upstream's
// defaults when `selectors` is not written. That is upstream's createRule (mergeSelectors).
type TailwindClassLiteralOptions struct {
	Selectors  *[]Selector      `json:"selectors"`
	Attributes []LegacySelector `json:"attributes"`
	Callees    []LegacySelector `json:"callees"`
	Tags       []LegacySelector `json:"tags"`
	Variables  []LegacySelector `json:"variables"`

	// surfaces is these options compiled, set once by the decoder, so a rule asking on every file reads
	// a pointer rather than rebuilding the settings and their key. Nil on options a test builds by hand,
	// which compile on each ask instead.
	surfaces *ClassLiteralSurfaces
}

// ClassLiteralSurfaces is these options compiled: the one shared by every decode that merged to the
// same settings.
func (options TailwindClassLiteralOptions) ClassLiteralSurfaces() *ClassLiteralSurfaces {
	if options.surfaces != nil {
		return options.surfaces
	}
	return ClassLiteralSurfacesFor(options.ClassLiteralSettings())
}

// compileClassLiterals checks and compiles the options once, after the decoder has merged the settings
// into them. Promoted onto every options struct that embeds TailwindClassLiteralOptions, so the decoder
// can call it.
func (options *TailwindClassLiteralOptions) compileClassLiterals() error {
	if options.Selectors != nil {
		if err := checkSelectors(*options.Selectors); err != nil {
			return err
		}
	}
	options.surfaces = ClassLiteralSurfacesFor(options.ClassLiteralSettings())
	return nil
}

// ClassLiteralSettings is these options merged into one list of selectors, as upstream merges them.
func (options TailwindClassLiteralOptions) ClassLiteralSettings() ClassLiteralSettings {
	return ClassLiteralSettings{Selectors: mergeSelectors(options.Selectors, options.Attributes, options.Callees, options.Tags, options.Variables)}
}

// ClassLiteralReader finds class-carrying strings in a file.
//
// Rules get one from ClassLiteralSurfaces.ReaderFor, whose surfaces compile each distinct set of
// settings once per run and which hands every rule reading a file with those settings the same reader,
// so a node is read once however many rules listen to it. NewClassLiteralReader builds an unshared one, for a harness.
type ClassLiteralReader struct {
	attributes *selectorGroup
	callees    *selectorGroup
	tags       *selectorGroup
	variables  *selectorGroup

	// values holds what each node read as, for the one file this reader serves. Nil on a reader that
	// is not bound to a file, which then reads every node afresh.
	values map[*ast.Node]classValues
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

// selectorGroup is one kind's selectors, with each name's answer remembered: which of them it matches.
//
// The answers are a memo because names repeat and the patterns run on a backtracking engine: ahra's
// attributes are almost all `className`, asked about on every JSX attribute in 3,978 files, and
// upstream's defaults hold 25 callee selectors. An answer is a pure function of the name, so the memo
// is shared by every worker reading with these settings.
type selectorGroup struct {
	selectors []compiledSelector
	lowercase bool
	hasPaths  bool
	// hasNames is whether any selector names by name, which a bare template's comment can only match.
	hasNames bool
	byName   sync.Map
	byPath   sync.Map
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

// matching is the selectors a name, or a path, matches, by index. path is asked only when a selector
// with a path did not match by name, since it is a new string for every member call in the file. A nil
// path matches by name alone, for what has no path: an attribute, a variable, a comment.
func (group *selectorGroup) matching(name string, path func() string) []int {
	if len(group.selectors) == 0 {
		return nil
	}
	byName := group.answer(&group.byName, name, func(selector compiledSelector) *namePatterns { return selector.name })
	if !group.hasPaths || path == nil {
		return byName
	}
	byPath := group.answer(&group.byPath, path(), func(selector compiledSelector) *namePatterns { return selector.path })
	if len(byPath) == 0 {
		return byName
	}
	merged := make([]int, 0, len(byName)+len(byPath))
	for index := range group.selectors {
		if slices.Contains(byName, index) || slices.Contains(byPath, index) {
			merged = append(merged, index)
		}
	}
	return merged
}

func (group *selectorGroup) answer(memo *sync.Map, name string, patternsOf func(compiledSelector) *namePatterns) []int {
	if name == "" {
		return nil
	}
	key := name
	if group.lowercase {
		key = strings.ToLower(name)
	}
	if answer, isAnswered := memo.Load(key); isAnswered {
		return answer.([]int)
	}
	var answer []int
	for index, selector := range group.selectors {
		if patterns := patternsOf(selector); patterns != nil && patterns.matchesUnremembered(name) {
			answer = append(answer, index)
		}
	}
	memo.Store(key, answer)
	return answer
}

// namePatterns is a selector's name patterns, compiled. Its answers are remembered by the selector
// group that asks, once for all of the group's selectors.
type namePatterns struct {
	compiled  []*esregexp.RegExp
	lowercase bool
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
		compiled, err := esregexp.Compile(pattern, "")
		if err != nil {
			continue
		}
		compiledPatterns.compiled = append(compiledPatterns.compiled, compiled)
	}
	return compiledPatterns
}

// matchesUnremembered reports whether any pattern matches the whole name, upstream's matchesName,
// asking the patterns every time.
func (patterns *namePatterns) matchesUnremembered(name string) bool {
	if name == "" || len(patterns.compiled) == 0 {
		return false
	}
	if patterns.lowercase {
		name = strings.ToLower(name)
	}
	for _, pattern := range patterns.compiled {
		// The first match, and only if it is the name: `exec(name)[0] === name`. A pattern whose
		// first match is shorter does not match, though a longer match exists, exactly as upstream.
		match, err := pattern.Unwrap().FindStringMatch(name)
		if err == nil && match != nil && match.RuneIndex == 0 && match.String() == name {
			return true
		}
	}
	return false
}

// ClassLiteralSurfaces is one set of settings compiled for the run: the unbound reader each file binds,
// and the file-cache key it binds under.
//
// The settings depend on the configuration and never on the file, yet every rule used to build them
// on every file it read, the defaults and then their key: on an ahra cold run 17 MB in 193K objects
// for the defaults and 24 MB in 263K for the keys (#y2nj5ex). Now a rule's decoder compiles them once
// per configuration selection, and a rule reads a pointer.
type ClassLiteralSurfaces struct {
	compiled *ClassLiteralReader
	cacheKey string
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
		return existing.(*ClassLiteralSurfaces)
	}
	compiled, _ := compiledClassLiteralSurfaces.LoadOrStore(key, &ClassLiteralSurfaces{
		compiled: NewClassLiteralReader(settings),
		cacheKey: "tailwind.classValues:" + key,
	})
	return compiled.(*ClassLiteralSurfaces)
}

// DefaultClassLiteralSurfaces is DefaultClassLiteralSettings compiled, once for the process, for a rule
// handed no options.
var DefaultClassLiteralSurfaces = sync.OnceValue(func() *ClassLiteralSurfaces {
	return ClassLiteralSurfacesFor(DefaultClassLiteralSettings())
})

// ReaderFor returns the reader for these surfaces in this file, shared through the file's cache by
// every rule that reads with the same settings.
//
// Thirteen rules listen on the same kinds, so before this each class surface was read thirteen
// times per file, and each rule compiled its own copy of the variable patterns on every file:
// 13 × 3,978 files × 2 patterns in ahra, about 0.08s of CPU spent recompiling two regular
// expressions. Reading is a pure function of the node and the settings, so one reading serves them
// all. A rule configured with different surfaces gets its own reader, keyed apart.
//
// Under --timing the first rule to reach a node pays for reading it and the rest read the memo, so
// the family's total is the honest number and one rule's row carries the shared reading.
//
// A nil cache yields a reader bound to nothing shared, which still memoizes within the one rule.
func (surfaces *ClassLiteralSurfaces) ReaderFor(cache *rule.FileCache) *ClassLiteralReader {
	return rule.Cached(cache, surfaces.cacheKey, surfaces.bind)
}

// bind is a copy of the compiled reader with a memo of its own, for one file.
func (surfaces *ClassLiteralSurfaces) bind() *ClassLiteralReader {
	bound := *surfaces.compiled
	bound.values = map[*ast.Node]classValues{}
	return &bound
}

// key is the settings as one string, every selector in order with every field, so two settings share
// a key only when they would read every node the same way. Built once per configuration selection.
func (s ClassLiteralSettings) key() string {
	encoded, err := json.Marshal(s.Selectors)
	if err != nil {
		panic(fmt.Sprintf("selectors that decoded do not encode: %v", err))
	}
	return string(encoded)
}

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

// ClassLiteralsIn returns the class strings a node carries, or nothing.
//
// Every string that can become the class value is returned, each as its own literal: the surface's
// own string, and the strings in the branches of a conditional, the operands of `&&`, `||` and `??`
// that can be the result, the elements of an array, and the expressions inside a template's holes.
// Prettier's Tailwind plugin sorted every one of these, and until #vf1hd6j measured it this reader
// returned only the first: 364 nested literals in ahra and 299 in www-phi-health that no rule here
// had ever read.
//
// Value positions only, which is narrower than the plugin. Its `sortInside` visits every string
// under the braces, so in `size === 'sm' ? 'p-2' : 'p-4'` it also reads `'sm'`. Sorting a single
// word is a no-op, so the plugin loses nothing by it; reporting `'primary-large'` from a comparison
// as an unknown class would be a false positive. A condition is not a class list.
//
// Each literal stands alone, so a rule comparing classes within one literal never compares the two
// branches of a conditional, which are never applied together. The static text of a template with
// holes is not a literal here: a class assembled at runtime is `no-concatenated-classes`'s finding,
// and `ClassSegmentsIn` and `ClassTemplateSegmentsIn` read that text for the rules that edit it.
func (r *ClassLiteralReader) ClassLiteralsIn(node *ast.Node) []ClassLiteral {
	return r.classValuesIn(node).literals
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

// classValuesIn returns what a node carries, read once per file when the reader is bound to one.
//
// The slices in a remembered reading are shared by every rule that asks, so callers range over them
// and never write into them.
//
// A template literal is a surface only to a tag selector with a name, which most settings have none of,
// and every rule asks about every template. Its empty answer costs nothing to give again, so it is not
// remembered: 3.7 MB over ahra's files was the memo growing for nothing.
func (r *ClassLiteralReader) classValuesIn(node *ast.Node) classValues {
	if r.values == nil {
		return r.readClassValues(node)
	}
	if !r.tags.hasNames && (node.Kind == ast.KindNoSubstitutionTemplateLiteral || node.Kind == ast.KindTemplateExpression) {
		return classValues{}
	}
	if remembered, isRead := r.values[node]; isRead {
		return remembered
	}
	values := r.readClassValues(node)
	r.values[node] = values
	return values
}

// readClassValues dispatches on the class surfaces, upstream's createRuleListener.
func (r *ClassLiteralReader) readClassValues(node *ast.Node) classValues {
	if node == nil {
		return classValues{}
	}

	switch node.Kind {
	case ast.KindJsxAttribute:
		return r.attributeValues(node)

	case ast.KindCallExpression:
		return r.calleeValues(node)

	case ast.KindVariableDeclaration:
		return r.variableValues(node)

	case ast.KindExportAssignment:
		return r.exportDefaultValues(node)

	case ast.KindTaggedTemplateExpression:
		return r.tagValues(node)

	case ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression:
		return r.bareTemplateValues(node)
	}

	return classValues{}
}

// exportDefaultValues is upstream's getLiteralsByESExportDefaultDeclaration: `export default <value>`
// is read as a variable named `default`. A function or class declared as the default is not an
// ExportAssignment here and not read there, and `export =` is TypeScript's, which upstream never reads.
func (r *ClassLiteralReader) exportDefaultValues(node *ast.Node) classValues {
	assignment := node.AsExportAssignment()
	if assignment == nil || assignment.IsExportEquals || assignment.Expression == nil {
		return classValues{}
	}
	indices := r.variables.matching("default", nil)
	if len(indices) == 0 {
		return classValues{}
	}
	matched := matchedNodes{}
	for _, index := range indices {
		readVariableSelector(r.variables.selectors[index], assignment.Expression, &matched)
	}
	return classValuesOf(matched, ClassLiteralOriginVariable)
}

// tagValues is upstream's getLiteralsByTaggedTemplateExpression: a tagged template named by its tag,
// twc.div, or by the callee of a tag that is a call, twc.div(Button). A tag selector with no matchers
// reads the template; one with matchers walks from the tagged template itself.
func (r *ClassLiteralReader) tagValues(node *ast.Node) classValues {
	tagged := node.AsTaggedTemplateExpression()
	tag := skipOuter(tagged.Tag)
	if tag != nil && tag.Kind == ast.KindCallExpression {
		tag = skipOuter(tag.AsCallExpression().Expression)
	}
	if tag == nil {
		return classValues{}
	}
	indices := r.tags.matching(calleeName(tag), func() string {
		_, path := calleeNameAndPath(tag)
		return path
	})
	if len(indices) == 0 {
		return classValues{}
	}
	matched := matchedNodes{}
	for _, index := range indices {
		selector := r.tags.selectors[index]
		if selector.matchers == nil {
			readDirect(tagged.Template, &matched)
			continue
		}
		matchNodes(node, selector.matchers, &matched)
	}
	return classValuesOf(matched, ClassLiteralOriginTag)
}

// bareTemplateValues is upstream's getLiteralsByESBareTemplateLiteral: a template no tag holds, read
// when the comment right before it matches a tag selector's name, `/* tw */` before the backtick. Only
// a name: a tag selector's path never matches a comment.
func (r *ClassLiteralReader) bareTemplateValues(node *ast.Node) classValues {
	if !r.tags.hasNames {
		return classValues{}
	}
	if parent := esParent(node); parent != nil && parent.Kind == ast.KindTaggedTemplateExpression {
		return classValues{}
	}
	comment := leadingComment(node)
	if comment == "" {
		return classValues{}
	}
	matched := matchedNodes{}
	for _, index := range r.tags.matching(comment, nil) {
		selector := r.tags.selectors[index]
		if selector.matchers == nil {
			readDirect(node, &matched)
			continue
		}
		matchNodes(node, selector.matchers, &matched)
	}
	return classValuesOf(matched, ClassLiteralOriginTag)
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

// attributeValues is upstream's getLiteralsByJSXAttribute: every attribute selector whose name
// matches, both lowercased, reads the attribute's value.
func (r *ClassLiteralReader) attributeValues(node *ast.Node) classValues {
	attribute := node.AsJsxAttribute()
	if attribute == nil || attribute.Initializer == nil {
		return classValues{}
	}

	// A namespaced attribute reads as `namespace:name`, which is what upstream's getAttributeName
	// builds and what Text gives a JsxNamespacedName.
	name := attribute.Name()
	if name == nil {
		return classValues{}
	}
	indices := r.attributes.matching(name.Text(), nil)
	if len(indices) == 0 {
		return classValues{}
	}
	matched := matchedNodes{}
	for _, index := range indices {
		readSelector(r.attributes.selectors[index], attribute.Initializer, &matched)
	}
	return classValuesOf(matched, ClassLiteralOriginAttribute)
}

// calleeValues is upstream's getLiteralsByESCallExpression: a call is read once, at the outermost
// call of a curried chain, and named by the chain's first callee. Each callee selector matching that
// name or path reads its target calls' target arguments.
func (r *ClassLiteralReader) calleeValues(node *ast.Node) classValues {
	if parent := esParent(node); parent != nil && parent.Kind == ast.KindCallExpression &&
		skipOuter(parent.AsCallExpression().Expression) == node {
		return classValues{}
	}
	first := node
	for callee := skipOuter(first.AsCallExpression().Expression); callee != nil && callee.Kind == ast.KindCallExpression; callee = skipOuter(callee.AsCallExpression().Expression) {
		first = callee
	}

	// Upstream's getESCalleeName: a call is named by its identifier or the property a member call ends
	// in, so `cn(...)` and `utils.cn(...)` are both `cn`, and its path is the dotted chain, `twc.div`.
	callee := skipOuter(first.AsCallExpression().Expression)
	if callee == nil {
		return classValues{}
	}
	indices := r.callees.matching(calleeName(callee), func() string {
		_, path := calleeNameAndPath(callee)
		return path
	})
	if len(indices) == 0 {
		return classValues{}
	}

	// The chain, outermost call last, built only for a call some selector reads, since nearly none is.
	chain := []*ast.Node{node}
	for call := node; call != first; {
		call = skipOuter(call.AsCallExpression().Expression)
		chain = append(chain, call)
	}
	slices.Reverse(chain)
	matched := matchedNodes{}
	for _, index := range indices {
		selector := r.callees.selectors[index]
		for _, call := range targetItems(chain, selector.targetCall, "first") {
			for _, argument := range targetArguments(call.AsCallExpression().Arguments, selector.targetArgument) {
				readSelector(selector, argument, &matched)
			}
		}
	}
	return classValuesOf(matched, ClassLiteralOriginCallee)
}

// variableValues is upstream's getLiteralsByESVariableDeclarator: a variable named by an identifier,
// read through every variable selector its name matches. A selector with matchers skips an initializer
// that is a call or a function, which a callee selector reads if anything does.
func (r *ClassLiteralReader) variableValues(node *ast.Node) classValues {
	declaration := node.AsVariableDeclaration()
	if declaration == nil || declaration.Initializer == nil {
		return classValues{}
	}

	name := declaration.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return classValues{}
	}
	indices := r.variables.matching(name.Text(), nil)
	if len(indices) == 0 {
		return classValues{}
	}
	matched := matchedNodes{}
	for _, index := range indices {
		readVariableSelector(r.variables.selectors[index], declaration.Initializer, &matched)
	}
	return classValuesOf(matched, ClassLiteralOriginVariable)
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
	value = skipOuter(value)
	if value == nil {
		return
	}
	switch value.Kind {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression:
		matched.add(value)
	case ast.KindJsxExpression:
		if expression := value.AsJsxExpression().Expression; expression != nil {
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
	callee = skipOuter(callee)
	switch callee.Kind {
	case ast.KindIdentifier:
		return callee.Text(), callee.Text()

	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		property := calleeName(callee)
		var object *ast.Node
		if callee.Kind == ast.KindPropertyAccessExpression {
			object = callee.AsPropertyAccessExpression().Expression
		} else {
			object = callee.AsElementAccessExpression().Expression
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
			values.templates = append(values.templates, classTemplateValue{node: node, origin: origin, edges: edges, concatenated: concatenated})
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
		if operator, isBinary := isESBinaryExpression(parent); !isBinary || operator != ast.KindPlusToken {
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
