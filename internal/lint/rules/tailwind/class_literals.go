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
	"regexp"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
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
	// can assert that all three surfaces are covered rather than only the obvious one.
	Origin ClassLiteralOrigin
	// Edges says which of the literal's ends will touch class text once it is substituted into a
	// template's hole, so that the whitespace at that end is all that separates two classes. See
	// holeEdges.
	Edges classValueEdges
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
	// classes, such as `const buttonClassName = '...'`.
	ClassLiteralOriginVariable ClassLiteralOrigin = "Variable"
)

// ClassLiteralSettings names the three surfaces where class strings are written.
//
// All three matter, and that is not obvious. On the ahra tree the attribute surface holds 7,773
// literals and the other two hold 2,192, so a rule that reads only JSX attributes silently covers
// 78% of the class surface while reporting a clean tree for the rest. Every rule in this package
// reads through this type for that reason.
type ClassLiteralSettings struct {
	// AttributeNames are the JSX attributes that carry classes, typically `class` and `className`.
	AttributeNames []string
	// CalleeNames are functions whose string arguments are classes.
	CalleeNames []string
	// VariablePatterns match variable names whose string initializers are classes.
	VariablePatterns []string
}

// DefaultClassLiteralSettings mirrors what the oxlint configuration supplies today.
//
// Defaults rather than a required option, because a rule that declines every file when
// unconfigured is indistinguishable from a rule with nothing to report. That failure kept
// `boundary-no-project-import` inert for months.
func DefaultClassLiteralSettings() ClassLiteralSettings {
	return ClassLiteralSettings{
		AttributeNames:   []string{"class", "className"},
		CalleeNames:      []string{"mergeClassNames", "createVariantClassNames"},
		VariablePatterns: []string{`.*[Cc]lassName$`, `.*[Cc]lassNames$`},
	}
}

// ClassLiteralReader finds class-carrying strings in a file.
//
// Compiled once per file rather than per node: the variable patterns are regular expressions, and
// recompiling them at every declaration in a 3,000-file tree is the kind of cost that makes a rule
// expensive for no reason.
type ClassLiteralReader struct {
	attributeNames   map[string]bool
	calleeNames      map[string]bool
	variablePatterns []*regexp.Regexp
}

// NewClassLiteralReader compiles the settings into a reader.
//
// An unparseable variable pattern is skipped rather than fatal. A rule that refuses to run because
// one regular expression in a config file is malformed reports a clean tree, which is the failure
// mode this whole tool exists to remove.
func NewClassLiteralReader(settings ClassLiteralSettings) *ClassLiteralReader {
	reader := &ClassLiteralReader{
		attributeNames: map[string]bool{},
		calleeNames:    map[string]bool{},
	}

	for _, name := range settings.AttributeNames {
		reader.attributeNames[name] = true
	}
	for _, name := range settings.CalleeNames {
		reader.calleeNames[name] = true
	}
	for _, pattern := range settings.VariablePatterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		reader.variablePatterns = append(reader.variablePatterns, compiled)
	}

	return reader
}

// ListenerKinds are the node kinds a rule must subscribe to in order to see every class literal.
//
// Returned as a list so a rule registers the same set rather than each one remembering three kinds,
// and so adding a fourth surface later reaches every rule at once.
func ListenerKinds() []ast.Kind {
	return []ast.Kind{
		ast.KindJsxAttribute,
		ast.KindCallExpression,
		ast.KindVariableDeclaration,
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
	// another template's hole.
	edges classValueEdges
}

// classValuesIn dispatches on the three class surfaces.
func (r *ClassLiteralReader) classValuesIn(node *ast.Node) classValues {
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
	}

	return classValues{}
}

func (r *ClassLiteralReader) attributeValues(node *ast.Node) classValues {
	attribute := node.AsJsxAttribute()
	if attribute == nil {
		return classValues{}
	}

	name := attribute.Name()
	if name == nil || !r.attributeNames[name.Text()] {
		return classValues{}
	}

	return classValuesUnder(attribute.Initializer, ClassLiteralOriginAttribute)
}

func (r *ClassLiteralReader) calleeValues(node *ast.Node) classValues {
	call := node.AsCallExpression()
	if call == nil || call.Expression == nil {
		return classValues{}
	}

	// The callee's own text, so `mergeClassNames(...)` matches and `theme.mergeClassNames(...)`
	// does not. Matching on the trailing identifier instead would let any object with a similarly
	// named method silently opt in.
	if call.Expression.Kind != ast.KindIdentifier {
		return classValues{}
	}
	if !r.calleeNames[call.Expression.Text()] || call.Arguments == nil {
		return classValues{}
	}

	values := classValues{}
	for _, argument := range call.Arguments.Nodes {
		collectClassValues(argument, ClassLiteralOriginCallee, classValueEdges{}, &values)
	}
	return values
}

func (r *ClassLiteralReader) variableValues(node *ast.Node) classValues {
	declaration := node.AsVariableDeclaration()
	if declaration == nil || declaration.Initializer == nil {
		return classValues{}
	}

	name := declaration.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return classValues{}
	}

	matches := false
	for _, pattern := range r.variablePatterns {
		if pattern.MatchString(name.Text()) {
			matches = true
			break
		}
	}
	if !matches {
		return classValues{}
	}

	return classValuesUnder(declaration.Initializer, ClassLiteralOriginVariable)
}

// classValuesUnder collects every string and template with holes in a value position under an
// expression.
func classValuesUnder(node *ast.Node, origin ClassLiteralOrigin) classValues {
	values := classValues{}
	collectClassValues(node, origin, classValueEdges{}, &values)
	return values
}

// collectClassValues walks the shapes a class value can arrive through, keeping the strings and the
// templates with holes.
//
// `&&` contributes only its right side, because its left side is the result only when it is falsy,
// and a class string is never falsy unless it is empty. `||` and `??` contribute both, since either
// side can be the result. Anything else, a call, a member access, a comparison, is not a class list
// and is not entered: in `cn(getSize('sm px-2'))` the string is an argument to a function nobody
// named as a class callee.
func collectClassValues(node *ast.Node, origin ClassLiteralOrigin, edges classValueEdges, values *classValues) {
	if node == nil {
		return
	}

	switch node.Kind {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		literal := classLiteralFrom(node, origin)
		literal.Edges = edges
		values.literals = append(values.literals, literal)

	case ast.KindJsxExpression:
		if expression := node.AsJsxExpression(); expression != nil {
			collectClassValues(expression.Expression, origin, edges, values)
		}

	// `className={('flex flex')}` is legal and means exactly what the unparenthesized form means.
	//
	// typescript-go keeps parentheses as real nodes rather than discarding them, so a reader that
	// unwraps only by the kinds it expects walks straight past them and finds nothing. That is a
	// silent under-report on valid code: the rule stays green, the tree looks clean, and the
	// finding never existed. All three rules in this package had it until a probe went looking.
	//
	// Recursive rather than a single unwrap, because `(('a'))` nests and the depth is the author's
	// choice. Stripping is correct here precisely because these rules read a string's contents and
	// parentheses cannot change them; it is not correct everywhere, and a rule whose verdict
	// depends on the parse shape must not copy this.
	case ast.KindParenthesizedExpression:
		if parenthesized := node.AsParenthesizedExpression(); parenthesized != nil {
			collectClassValues(parenthesized.Expression, origin, edges, values)
		}

	case ast.KindAsExpression:
		collectClassValues(node.AsAsExpression().Expression, origin, edges, values)

	case ast.KindSatisfiesExpression:
		collectClassValues(node.AsSatisfiesExpression().Expression, origin, edges, values)

	case ast.KindConditionalExpression:
		conditional := node.AsConditionalExpression()
		collectClassValues(conditional.WhenTrue, origin, edges, values)
		collectClassValues(conditional.WhenFalse, origin, edges, values)

	case ast.KindBinaryExpression:
		binary := node.AsBinaryExpression()
		if binary.OperatorToken == nil {
			return
		}
		switch binary.OperatorToken.Kind {
		case ast.KindAmpersandAmpersandToken:
			collectClassValues(binary.Right, origin, edges, values)
		case ast.KindBarBarToken, ast.KindQuestionQuestionToken:
			collectClassValues(binary.Left, origin, edges, values)
			collectClassValues(binary.Right, origin, edges, values)
		}

	case ast.KindArrayLiteralExpression:
		if elements := node.AsArrayLiteralExpression().Elements; elements != nil {
			for _, element := range elements.Nodes {
				collectClassValues(element, origin, edges, values)
			}
		}

	case ast.KindTemplateExpression:
		values.templates = append(values.templates, classTemplateValue{node: node, origin: origin, edges: edges})
		template := node.AsTemplateExpression()
		if template.Head == nil || template.TemplateSpans == nil {
			return
		}
		spans := template.TemplateSpans.Nodes
		before := template.Head.Text()
		for index, spanNode := range spans {
			span := spanNode.AsTemplateSpan()
			if span == nil || span.Literal == nil {
				continue
			}
			after := span.Literal.Text()
			collectClassValues(span.Expression, origin, holeEdges(before, after, index == 0, index == len(spans)-1, edges), values)
			before = after
		}
	}
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
func SplitClasses(text string) []string {
	var classes []string
	for _, field := range strings.Fields(text) {
		if strings.Contains(field, "${") {
			continue
		}
		classes = append(classes, field)
	}
	return classes
}
