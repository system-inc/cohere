package security_no_interpolated_sql_string

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/inert_value"
	"golang.org/x/tools/go/types/typeutil"
)

// queryReader reads query expressions into templates within one package.
type queryReader struct {
	context *rule.Context
	// initializers are the package's locals assigned once, where declared, and never through a pointer, each
	// with the expression that assigned it.
	initializers map[*types.Var]ast.Expr
}

// read is expression as a template: its texts where they are constants, and a hole for every value it writes in.
func (reader queryReader) read(expression ast.Expr, depth int) template {
	unwrapped := ast.Unparen(expression)
	if depth > readingDepth {
		return holeTemplate(hole{expression: unwrapped})
	}
	if value := reader.context.TypesInfo.Types[unwrapped].Value; value != nil && value.Kind() == constant.String {
		return textTemplate(constant.StringVal(value))
	}
	switch built := unwrapped.(type) {
	case *ast.BinaryExpr:
		if built.Op == token.ADD && isString(reader.context.TypesInfo.TypeOf(built)) {
			query := reader.read(built.X, depth+1)
			query.appendTemplate(reader.read(built.Y, depth+1))
			return query
		}
	case *ast.CallExpr:
		if query, readable := reader.readSprintf(built, depth); readable {
			return query
		}
	case *ast.Ident:
		// A local is read in place only when its initializer builds text itself. One that names a call's result
		// stays the hole, so a finding lands on the name in the query rather than on a line that writes no SQL.
		if initializer := reader.localInitializer(built); initializer != nil && reader.buildsText(initializer) {
			return reader.read(initializer, depth+1)
		}
	}
	return holeTemplate(hole{expression: unwrapped})
}

// buildsText is whether expression is a shape read reads as more than one hole: a constant, a sum, or a
// fmt.Sprintf.
func (reader queryReader) buildsText(expression ast.Expr) bool {
	unwrapped := ast.Unparen(expression)
	if reader.context.TypesInfo.Types[unwrapped].Value != nil {
		return true
	}
	switch built := unwrapped.(type) {
	case *ast.BinaryExpr:
		return built.Op == token.ADD
	case *ast.CallExpr:
		return isSprintf(reader.context, built)
	}
	return false
}

// readSprintf is call as a template when it is fmt.Sprintf with a constant format whose verbs take its arguments
// one after another, and whether it is.
func (reader queryReader) readSprintf(call *ast.CallExpr, depth int) (template, bool) {
	// A spread argument list, fmt.Sprintf(format, values...), hides which value each verb prints.
	if !isSprintf(reader.context, call) || len(call.Args) == 0 || call.Ellipsis.IsValid() {
		return template{}, false
	}
	format := reader.context.TypesInfo.Types[call.Args[0]].Value
	if format == nil || format.Kind() != constant.String {
		return template{}, false
	}
	texts, verbs, readable := readFormat(constant.StringVal(format))
	if !readable || len(verbs) != len(call.Args)-1 {
		return template{}, false
	}
	query := textTemplate(texts[0])
	for verbIndex, printed := range verbs {
		argument := ast.Unparen(call.Args[verbIndex+1])
		switch {
		case printed.letter == 'x' || printed.letter == 'X':
			query.appendTemplate(holeTemplate(hole{expression: argument, printsHex: true}))
		case printed.plain && (printed.letter == 's' || printed.letter == 'v') &&
			isString(reader.context.TypesInfo.TypeOf(argument)):
			// A plain %s or %v prints a string unchanged, so the string's own text and holes stand in its place.
			query.appendTemplate(reader.read(argument, depth+1))
		default:
			query.appendTemplate(holeTemplate(hole{expression: argument}))
		}
		query.appendTemplate(textTemplate(texts[verbIndex+1]))
	}
	return query, true
}

// canHoldQuote is whether found can print a single quote or a backslash into the text around it.
func (reader queryReader) canHoldQuote(found hole) bool {
	if found.printsHex {
		return false
	}
	if value := reader.context.TypesInfo.Types[found.expression].Value; value != nil {
		// A constant is fixed text, and only one that itself holds a quote or a backslash breaks the string.
		return value.Kind() == constant.String && strings.ContainsAny(constant.StringVal(value), `'\`)
	}
	return !reader.isInert(found.expression) && canHoldText(reader.context.TypesInfo.TypeOf(found.expression))
}

// canHoldText is whether found can print any text at all, rather than digits, a boolean or a fixed constant.
func (reader queryReader) canHoldText(found hole) bool {
	if found.printsHex || reader.context.TypesInfo.Types[found.expression].Value != nil {
		return false
	}
	return !reader.isInert(found.expression) && canHoldText(reader.context.TypesInfo.TypeOf(found.expression))
}

// isInert is whether expression, or the initializer of the local it names, prints only as a number or a boolean:
// package inert_value's judgment, which the shell rule shares, so `id = " + strconv.Itoa(id)` is as quiet as the
// `%d` of the same id.
func (reader queryReader) isInert(expression ast.Expr) bool {
	if inert_value.Is(reader.context.TypesInfo, expression) {
		return true
	}
	identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	if !isIdentifier {
		return false
	}
	initializer := reader.localInitializer(identifier)
	return initializer != nil && inert_value.Is(reader.context.TypesInfo, initializer)
}

// isEscaped is whether expression, or the initializer of the local it names, ends in the one escape the package
// comment accepts.
func (reader queryReader) isEscaped(expression ast.Expr) bool {
	resolved := ast.Unparen(expression)
	if identifier, isIdentifier := resolved.(*ast.Ident); isIdentifier {
		if initializer := reader.localInitializer(identifier); initializer != nil {
			resolved = ast.Unparen(initializer)
		}
	}
	outer, outerReceiver := reader.replaceStep(resolved)
	if outer == "" {
		return false
	}
	inner, _ := reader.replaceStep(outerReceiver)
	return (outer == `'` && inner == `\`) || (outer == `\` && inner == `'`)
}

// replaceStep reads strings.ReplaceAll(text, old, new), or strings.Replace with a count of -1, whose old is a
// single quote or a backslash and whose new is that character doubled. It returns which character the step
// doubles, with the text it doubles it in, and an empty character for anything else.
func (reader queryReader) replaceStep(expression ast.Expr) (string, ast.Expr) {
	call, isCall := ast.Unparen(expression).(*ast.CallExpr)
	if !isCall {
		return "", nil
	}
	function, isFunction := typeutil.Callee(reader.context.TypesInfo, call).(*types.Func)
	if !isFunction || function.Pkg() == nil || function.Pkg().Path() != "strings" {
		return "", nil
	}
	switch {
	case function.Name() == "ReplaceAll" && len(call.Args) == 3:
	case function.Name() == "Replace" && len(call.Args) == 4:
		count := reader.context.TypesInfo.Types[call.Args[3]].Value
		if count == nil || count.Kind() != constant.Int || constant.Sign(count) >= 0 {
			return "", nil
		}
	default:
		return "", nil
	}
	old := reader.context.TypesInfo.Types[call.Args[1]].Value
	replacement := reader.context.TypesInfo.Types[call.Args[2]].Value
	if old == nil || replacement == nil || old.Kind() != constant.String || replacement.Kind() != constant.String {
		return "", nil
	}
	character := constant.StringVal(old)
	if (character != `'` && character != `\`) || constant.StringVal(replacement) != character+character {
		return "", nil
	}
	return character, ast.Unparen(call.Args[0])
}

// localInitializer is the expression that assigned identifier's local, when it was assigned once, where declared.
func (reader queryReader) localInitializer(identifier *ast.Ident) ast.Expr {
	variable, isVariable := reader.context.TypesInfo.Uses[identifier].(*types.Var)
	if !isVariable {
		return nil
	}
	return reader.initializers[variable]
}
