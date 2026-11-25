package tailwind

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

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
func (reader *ClassLiteralReader) ClassLiteralsIn(node *ast.Node) []ClassLiteral {
	return reader.classValuesIn(node).literals
}

// classValuesIn returns what a node carries, read once per file when the reader is bound to one.
//
// The slices in a remembered reading are shared by every rule that asks, so callers range over them
// and never write into them.
//
// A template literal is a surface only to a tag selector with a name, which most settings have none of,
// and every rule asks about every template. Its empty answer costs nothing to give again, so it is not
// remembered: 3.7 MB over ahra's files was the memo growing for nothing.
func (reader *ClassLiteralReader) classValuesIn(node *ast.Node) classValues {
	if reader.values == nil {
		return reader.readClassValues(node)
	}
	if !reader.tags.hasNames &&
		(node.Kind == ast.KindNoSubstitutionTemplateLiteral || node.Kind == ast.KindTemplateExpression) {
		return classValues{}
	}
	if remembered, isRead := reader.values[node]; isRead {
		return remembered
	}
	values := reader.readClassValues(node)
	reader.values[node] = values
	return values
}

// readClassValues dispatches on the class surfaces, upstream's createRuleListener.
func (reader *ClassLiteralReader) readClassValues(node *ast.Node) classValues {
	if node == nil {
		return classValues{}
	}

	switch node.Kind {
	case ast.KindJsxAttribute:
		return reader.attributeValues(node)

	case ast.KindCallExpression:
		return reader.calleeValues(node)

	case ast.KindVariableDeclaration:
		return reader.variableValues(node)

	case ast.KindExportAssignment:
		return reader.exportDefaultValues(node)

	case ast.KindTaggedTemplateExpression:
		return reader.tagValues(node)

	case ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression:
		return reader.bareTemplateValues(node)
	}

	return classValues{}
}

// exportDefaultValues is upstream's getLiteralsByESExportDefaultDeclaration: `export default <value>`
// is read as a variable named `default`. A function or class declared as the default is not an
// ExportAssignment here and not read there, and `export =` is TypeScript's, which upstream never reads.
func (reader *ClassLiteralReader) exportDefaultValues(node *ast.Node) classValues {
	assignment := node.AsExportAssignment()
	if assignment == nil || assignment.IsExportEquals || assignment.Expression == nil {
		return classValues{}
	}
	indices := reader.variables.matching("default", nil)
	if len(indices) == 0 {
		return classValues{}
	}
	matched := matchedNodes{}
	for _, index := range indices {
		readVariableSelector(reader.variables.selectors[index], assignment.Expression, &matched)
	}
	return classValuesOf(matched, ClassLiteralOriginVariable)
}

// tagValues is upstream's getLiteralsByTaggedTemplateExpression: a tagged template named by its tag,
// twc.div, or by the callee of a tag that is a call, twc.div(Button). A tag selector with no matchers
// reads the template; one with matchers walks from the tagged template itself.
func (reader *ClassLiteralReader) tagValues(node *ast.Node) classValues {
	tagged := node.AsTaggedTemplateExpression()
	tag := skipOuter(tagged.Tag)
	if tag != nil && tag.Kind == ast.KindCallExpression {
		tag = skipOuter(tag.AsCallExpression().Expression)
	}
	if tag == nil {
		return classValues{}
	}
	indices := reader.tags.matching(calleeName(tag), func() string {
		_, path := calleeNameAndPath(tag)
		return path
	})
	if len(indices) == 0 {
		return classValues{}
	}
	matched := matchedNodes{}
	for _, index := range indices {
		selector := reader.tags.selectors[index]
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
func (reader *ClassLiteralReader) bareTemplateValues(node *ast.Node) classValues {
	if !reader.tags.hasNames {
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
	for _, index := range reader.tags.matching(comment, nil) {
		selector := reader.tags.selectors[index]
		if selector.matchers == nil {
			readDirect(node, &matched)
			continue
		}
		matchNodes(node, selector.matchers, &matched)
	}
	return classValuesOf(matched, ClassLiteralOriginTag)
}

// attributeValues is upstream's getLiteralsByJSXAttribute: every attribute selector whose name
// matches, both lowercased, reads the attribute's value.
func (reader *ClassLiteralReader) attributeValues(node *ast.Node) classValues {
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
	indices := reader.attributes.matching(name.Text(), nil)
	if len(indices) == 0 {
		return classValues{}
	}
	matched := matchedNodes{}
	for _, index := range indices {
		readSelector(reader.attributes.selectors[index], attribute.Initializer, &matched)
	}
	return classValuesOf(matched, ClassLiteralOriginAttribute)
}

// calleeValues is upstream's getLiteralsByESCallExpression: a call is read once, at the outermost
// call of a curried chain, and named by the chain's first callee. Each callee selector matching that
// name or path reads its target calls' target arguments.
func (reader *ClassLiteralReader) calleeValues(node *ast.Node) classValues {
	if parent := esParent(node); parent != nil && parent.Kind == ast.KindCallExpression &&
		skipOuter(parent.AsCallExpression().Expression) == node {
		return classValues{}
	}
	first := node
	for callee := skipOuter(first.AsCallExpression().Expression); callee != nil &&
		callee.Kind == ast.KindCallExpression; callee = skipOuter(callee.AsCallExpression().Expression) {
		first = callee
	}

	// Upstream's getESCalleeName: a call is named by its identifier or the property a member call ends
	// in, so `cn(...)` and `utils.cn(...)` are both `cn`, and its path is the dotted chain, `twc.div`.
	callee := skipOuter(first.AsCallExpression().Expression)
	if callee == nil {
		return classValues{}
	}
	indices := reader.callees.matching(calleeName(callee), func() string {
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
		selector := reader.callees.selectors[index]
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
func (reader *ClassLiteralReader) variableValues(node *ast.Node) classValues {
	declaration := node.AsVariableDeclaration()
	if declaration == nil || declaration.Initializer == nil {
		return classValues{}
	}

	name := declaration.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return classValues{}
	}
	indices := reader.variables.matching(name.Text(), nil)
	if len(indices) == 0 {
		return classValues{}
	}
	matched := matchedNodes{}
	for _, index := range indices {
		readVariableSelector(reader.variables.selectors[index], declaration.Initializer, &matched)
	}
	return classValuesOf(matched, ClassLiteralOriginVariable)
}

// ClassSegmentsIn returns every run of static class text a node carries.
//
// Covers both plain strings and templates, over the same surfaces the other readers use, so a
// rule built on this one cannot silently cover less than a rule built on those.
func (reader *ClassLiteralReader) ClassSegmentsIn(node *ast.Node) []ClassSegment {
	if node == nil {
		return nil
	}

	// A plain string literal is one segment with no holes on either side, unless it sits inside a
	// template's hole, where an edge that touches the template's text once substituted keeps its
	// whitespace as a separator (see holeEdges).
	values := reader.classValuesIn(node)
	var segments []ClassSegment
	for _, literal := range values.literals {
		segments = append(segments, ClassSegment{
			Text:         literal.Text,
			Range:        literal.Range,
			LeadingHole:  literal.Edges.Leading,
			TrailingHole: literal.Edges.Trailing,
			Origin:       literal.Origin,
		})
	}

	// And every template with holes, broken into its static runs, whether or not literals were
	// found. They used to be alternatives, and once the strings inside a template's holes were read,
	// the template's own text would have been skipped whenever one of its holes held a string.
	//
	// Deliberately not routed through ClassTemplatesIn. That reader answers "where does static text
	// meet an interpolation without whitespace", so it returns nothing for a template whose seams
	// are all clean. `flex  ${x}  gap-2` has no boundary defects and two segments that still need
	// their doubled spaces collapsed, and reusing the boundary reader here would have silently
	// skipped exactly the templates that are otherwise well written.
	for _, template := range values.templates {
		segments = append(segments, segmentsOfTemplate(template)...)
	}
	return segments
}

// ClassTemplateSegmentsIn returns every template with holes a node carries, each as its static runs.
//
// Grouped per template rather than flattened, because a rule that orders classes orders each run on
// its own and has to know which runs touch which holes, and that is the template's structure. Every
// template in a value position is returned, including one inside a conditional or inside another
// template's hole, which is where Prettier's Tailwind plugin finds them too.
func (reader *ClassLiteralReader) ClassTemplateSegmentsIn(node *ast.Node) [][]ClassSegment {
	templates := [][]ClassSegment{}
	for _, template := range reader.classValuesIn(node).templates {
		if segments := segmentsOfTemplate(template); len(segments) > 0 {
			templates = append(templates, segments)
		}
	}
	return templates
}

// ClassTemplatesIn returns every seam a node's class text has with a runtime value: each template
// with holes the reader found, and each string a `+` joins to something, which upstream's
// no-concatenated-classes reports alike (isConcatenatedLiteral).
//
// Read from the same reading as ClassLiteralsIn, so a rule reading seams covers exactly the strings a
// rule reading classes does. Anything else is a difference in coverage that nobody declared.
func (reader *ClassLiteralReader) ClassTemplatesIn(node *ast.Node) []ClassTemplate {
	values := reader.classValuesIn(node)
	var templates []ClassTemplate
	for _, template := range values.templates {
		if classTemplate, hasBoundaries := templateBoundaries(template); hasBoundaries {
			templates = append(templates, classTemplate)
		}
	}
	for _, literal := range values.literals {
		if classTemplate, hasBoundaries := concatenationBoundaries(literal); hasBoundaries {
			templates = append(templates, classTemplate)
		}
	}
	return templates
}
