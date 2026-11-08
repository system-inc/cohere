// Package invalid_template is staticcheck's SA1001, a text or html template given as a constant to Parse that does
// not parse, ported as a check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1001/sa1001.go, whose logic it keeps and
// whose messages, the template package's own error, it reports word for word, at the same range, the template
// argument. The original matches its query, `(CallExpr (Symbol name@(Or "(*text/template.Template).Parse"
// "(*html/template.Template).Parse")) [s])`, through staticcheck's pattern language; the port asks the same of each
// call directly, reading pattern/match.go for what each construct of the query matches, asks
// staticcheck_code.IsCallToAny and staticcheck_code.ExprToString where the original asks code.IsCallToAny and
// code.ExprToString, reads the template argument's position from staticcheck's knowledge package, the table of
// argument positions the original reads, which this wave imports as data and a later wave ports, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry still runs the original,
// which stays the oracle: the parity tests beside this file run both over the same packages.
//
// The two differ on one shape. The query looks through parentheses around the method, but the original then asserts
// that the call's function is a selector, and panics on `(tt.New("").Parse)(text)`, which loses every finding of the
// package. The port looks through the same parentheses and judges the call as written without them, as
// testdata/src/panics records and TestPortReportsWhereTheOriginalPanics holds.
package invalid_template

import (
	"go/ast"
	"go/types"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA1001",
	Doc: "Flags a text or html template given as a constant to Parse that does not parse: staticcheck's SA1001, " +
		"ported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every call of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("invalid_template: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(node ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: Preorder hands this function only the calls it was asked for.
			panic("invalid_template: the inspector handed over a node that is not a call")
		}
		Check(context, call)
	})
	return nil, nil
}

// Check judges one call: Parse of a text or html template made by that package's New in the same expression, given
// one constant template, is marked with the error parsing it gives, when the error is a syntax error, one that says
// unexpected or bad character. A template made elsewhere may have its delimiters changed first, so only one made
// in the call's own receiver is judged.
func Check(context *rule.Context, call *ast.CallExpr) {
	// The query's Symbol looks through parentheses around the method; the original then asserts the function is a
	// selector and panics where it is parenthesized, so the port looks through them as the query does.
	method, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return
	}
	parse := parseOf(context, method)
	if parse == nil {
		return
	}
	// `[s]` is a list of exactly one argument. Parse takes one, so a call that type-checks always has it.
	if len(call.Args) != 1 {
		return
	}
	if !staticcheck_code.IsCallToAny(context, method.X, "text/template.New", "html/template.New") {
		return
	}
	// The matcher reaches s through its outer parentheses, and the constant is the same through them.
	text, isString := staticcheck_code.ExprToString(context, ast.Unparen(call.Args[0]))
	if !isString {
		return
	}
	parseError := parse(text)
	if parseError == nil {
		return
	}
	if strings.Contains(parseError.Error(), "unexpected") || strings.Contains(parseError.Error(), "bad character") {
		// The finding covers the argument as written, parentheses and all, as the original reports it through the
		// call's own argument rather than the binding.
		staticcheck_report.Report(context, call.Args[knowledge.Arg("(*text/template.Template).Parse.text")],
			parseError.Error())
	}
}

// parseOf is the parse of the template package whose Template's Parse method names, as the query's Symbol and its
// Or of the two methods match it, or nil when method names neither. A method's object spells its receiver in
// FullName, so a function, a variable or another type's Parse is neither.
func parseOf(context *rule.Context, method *ast.SelectorExpr) func(text string) error {
	function, isFunction := context.TypesInfo.ObjectOf(method.Sel).(*types.Func)
	if !isFunction {
		return nil
	}
	switch function.FullName() {
	case "(*text/template.Template).Parse":
		return func(text string) error {
			_, parseError := texttemplate.New("").Parse(text)
			return parseError
		}
	case "(*html/template.Template).Parse":
		return func(text string) error {
			_, parseError := htmltemplate.New("").Parse(text)
			return parseError
		}
	}
	return nil
}
