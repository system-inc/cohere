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

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/core"
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
// Only genuine string literals are returned. A template with a hole in it is knowable only in part,
// and a class assembled at runtime is `no-concatenated-classes`'s finding rather than this reader's
// problem.
func (r *ClassLiteralReader) ClassLiteralsIn(node *ast.Node) []ClassLiteral {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case ast.KindJsxAttribute:
		return r.attributeLiterals(node)

	case ast.KindCallExpression:
		return r.calleeLiterals(node)

	case ast.KindVariableDeclaration:
		return r.variableLiterals(node)
	}

	return nil
}

func (r *ClassLiteralReader) attributeLiterals(node *ast.Node) []ClassLiteral {
	attribute := node.AsJsxAttribute()
	if attribute == nil {
		return nil
	}

	name := attribute.Name()
	if name == nil || !r.attributeNames[name.Text()] {
		return nil
	}

	literal := stringLiteralOf(attribute.Initializer)
	if literal == nil {
		return nil
	}
	return []ClassLiteral{classLiteralFrom(literal, ClassLiteralOriginAttribute)}
}

func (r *ClassLiteralReader) calleeLiterals(node *ast.Node) []ClassLiteral {
	call := node.AsCallExpression()
	if call == nil || call.Expression == nil {
		return nil
	}

	// The callee's own text, so `mergeClassNames(...)` matches and `theme.mergeClassNames(...)`
	// does not. Matching on the trailing identifier instead would let any object with a similarly
	// named method silently opt in.
	if call.Expression.Kind != ast.KindIdentifier {
		return nil
	}
	if !r.calleeNames[call.Expression.Text()] {
		return nil
	}

	var literals []ClassLiteral
	if call.Arguments == nil {
		return nil
	}
	for _, argument := range call.Arguments.Nodes {
		if literal := stringLiteralOf(argument); literal != nil {
			literals = append(literals, classLiteralFrom(literal, ClassLiteralOriginCallee))
		}
	}
	return literals
}

func (r *ClassLiteralReader) variableLiterals(node *ast.Node) []ClassLiteral {
	declaration := node.AsVariableDeclaration()
	if declaration == nil || declaration.Initializer == nil {
		return nil
	}

	name := declaration.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return nil
	}

	matches := false
	for _, pattern := range r.variablePatterns {
		if pattern.MatchString(name.Text()) {
			matches = true
			break
		}
	}
	if !matches {
		return nil
	}

	literal := stringLiteralOf(declaration.Initializer)
	if literal == nil {
		return nil
	}
	return []ClassLiteral{classLiteralFrom(literal, ClassLiteralOriginVariable)}
}

// stringLiteralOf unwraps the shapes a class string can arrive in.
//
// A JSX attribute may hold the literal directly (`className="a b"`) or inside an expression
// container (`className={"a b"}`), and both are equally static. A template literal with no
// substitutions is also fully known, but is deliberately excluded: its raw text can contain escapes
// whose decoded length differs from the source, which would make an offset-based fix wrong.
func stringLiteralOf(node *ast.Node) *ast.Node {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case ast.KindStringLiteral:
		return node

	case ast.KindJsxExpression:
		expression := node.AsJsxExpression()
		if expression == nil {
			return nil
		}
		return stringLiteralOf(expression.Expression)
	}

	return nil
}

// classLiteralFrom records the literal and the span of its contents.
//
// The range excludes the surrounding quotes so that a fix replaces class text and nothing else. Any
// literal whose source form is shorter than two characters cannot have quotes to strip, so it is
// reported with its own range rather than a negative one.
func classLiteralFrom(literal *ast.Node, origin ClassLiteralOrigin) ClassLiteral {
	contentRange := literal.Loc
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
