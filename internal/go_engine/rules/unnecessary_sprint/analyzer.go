// Package unnecessary_sprint is staticcheck's S1039, a call of fmt.Sprint or fmt.Sprintf whose one argument is a
// string literal, `fmt.Sprint("foo")`, which is `"foo"` written longer, ported as a check on types and syntax that
// reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1039/s1039.go, whose logic it keeps and whose
// message and fix it reports word for word, over the same range, the whole call. The original matches its one query,
// `(CallExpr fn@(Or (Symbol "fmt.Sprint") (Symbol "fmt.Sprintf")) [lit@(BasicLit "STRING" _)])`, through staticcheck's
// pattern language, walked over the calls its type index records of the two functions; the port asks the same of
// each call the dispatcher hands it, reading pattern/match.go for what each construct of the query matches, and reports
// through staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere they hold.
package unnecessary_sprint

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	// The original's root is a call of a Symbol, so code.Matches walks the type index's calls of fmt.Sprint and of
	// fmt.Sprintf instead of the syntax. Those are the calls whose callee, through parentheses and a selector,
	// resolves to either function, which are the calls the gate in Check lets through, so the port hears every call
	// of the dispatcher's walk and asks the same of each. Where the index answers otherwise, inside fmt itself, Check
	// says why.
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("unnecessary_sprint: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1039",
	"S1039",
	"Flags fmt.Sprint or fmt.Sprintf given one string literal: staticcheck's S1039, ported.",
	&Listener,
)

// Check judges one call: fmt.Sprint given one string literal, or fmt.Sprintf given one with no percent sign in it,
// returns the literal's own value, and the fix replaces the call by the literal.
func Check(context *rule.Context, call *ast.CallExpr) {
	// The pattern's `[lit@...]` is a list of one: exactly one argument. A call's Ellipsis is no part of what
	// `(CallExpr fn args)` matches, and a spread string literal never type-checks anyway.
	if len(call.Args) != 1 {
		return
	}
	// `(BasicLit "STRING" _)` is a string literal, interpreted or raw, and the pattern sees through parentheses
	// around it, binding the literal itself, so `fmt.Sprint(("foo"))` is replaced by `"foo"`. A named constant, a
	// concatenation or a conversion is no literal, though its value is a string.
	literal, isLiteral := ast.Unparen(call.Args[0]).(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return
	}
	function, isFunction := staticcheck_code.PackageFunction(context, call.Fun, "fmt")
	if !isFunction {
		return
	}
	switch function.Name() {
	case "Sprint":
	case "Sprintf":
		// The original reads the literal as written, so a percent sign makes a possible format and is left alone,
		// while an escape that spells one, `"\x25d"`, holds no percent sign and is reported.
		if strings.ContainsRune(literal.Value, '%') {
			return
		}
	default:
		return
	}
	// code.Matches walks the type index's calls of the two functions, and the index never holds the package under
	// analysis, so inside fmt it knows no fmt.Sprint and the original reports nothing there. fmt's external tests are
	// a package of their own, which imports fmt, and are judged as any other.
	if function.Pkg() == context.Package {
		return
	}
	staticcheck_report.Report(context, call, fmt.Sprintf("unnecessary use of fmt.%s", function.Name()),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(edit.Fix("Replace with string literal", edit.ReplaceWithNode(context.Fset, call,
			literal))))
}
