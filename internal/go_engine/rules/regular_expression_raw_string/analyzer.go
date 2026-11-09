// Package regular_expression_raw_string is staticcheck's S1007, a regular expression compiled from an interpreted
// string literal whose only escapes are escaped backslashes, `regexp.MustCompile("\\d+")`, which a raw string literal
// writes once, ported as a check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1007/s1007.go, whose logic it keeps and whose
// message it reports word for word, at the same range, the literal. The original matches its one query,
// `(CallExpr (Symbol fn@(Or "regexp.MustCompile" "regexp.Compile")) [lit@(BasicLit "STRING" _)])`, through
// staticcheck's pattern language; the port asks the same of each call directly, reading pattern/match.go for what each
// construct of the query matches, and analysis/code/visit.go for which calls code.Matches hands it, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere they hold.
package regular_expression_raw_string

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/function_name"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("regular_expression_raw_string: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1007",
	"S1007",
	"Flags a regular expression compiled from a string literal that escapes only backslashes: staticcheck's "+
		"S1007, ported.",
	&Listener,
)

// Check judges one call: regexp.MustCompile or regexp.Compile of one interpreted string literal, holding an escaped
// backslash and no backtick, whose every escape is an escaped backslash, is a pattern a raw string literal writes with
// half the backslashes, and the finding names the function called.
func Check(context *rule.Context, call *ast.CallExpr) {
	function := compileFunctionName(context, call.Fun)
	if function == "" {
		return
	}
	// `[lit@(...)]` is a list of one, so the call has exactly one argument.
	if len(call.Args) != 1 {
		return
	}
	// `(BasicLit "STRING" _)` is a string literal written as one, through any parentheses, since the pattern sees
	// through them and binds the literal inside, where the finding lands: a constant naming a string, a conversion or
	// a concatenation is not one.
	literal, isLiteral := ast.Unparen(call.Args[0]).(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return
	}
	// A raw string literal opens with a backtick, and is the form the finding asks for already.
	if literal.Value[0] != '"' {
		return
	}
	if !strings.Contains(literal.Value, `\\`) {
		return
	}
	// A raw string literal can't hold a backtick.
	if strings.Contains(literal.Value, "`") {
		return
	}
	if !escapesOnlyBackslashes(literal.Value) {
		return
	}
	staticcheck_report.Report(context, literal,
		fmt.Sprintf("should use raw string (`...`) with %s to avoid having to escape twice", function),
		staticcheck_report.FilterGenerated())
}

// compileFunctionNames are the two functions the pattern's Or names, by the full names the finding reports.
var compileFunctionNames = []string{"regexp.MustCompile", "regexp.Compile"}

// compileFunctionName is the name function calls by, regexp.MustCompile or regexp.Compile, as the pattern's
// `fn@(Symbol (Or "regexp.MustCompile" "regexp.Compile"))` binds it, and empty for any other callee. Symbol matches an
// identifier or a selector, parenthesized or not and instantiated or not, and binds the full name of the function it
// names, so a dot import or a renamed import still binds the name spelled with the package's path, and a method, a
// variable holding the function or a function of the package's own named alike does not match.
func compileFunctionName(context *rule.Context, function ast.Expr) string {
	// The name is the function without its parentheses or its instantiation, itself parenthesized or not.
	name := ast.Unparen(function)
	switch instantiated := name.(type) {
	case *ast.IndexExpr:
		name = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		name = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch name := name.(type) {
	case *ast.Ident:
		object = context.TypesInfo.ObjectOf(name)
	case *ast.SelectorExpr:
		object = context.TypesInfo.ObjectOf(name.Sel)
	default:
		return ""
	}
	// The full name Index compares spells a method with its receiver, `(T).Compile`, and a function by its package's
	// path, which is regexp for the standard library's regexp alone.
	compile, isFunction := object.(*types.Func)
	if !isFunction {
		return ""
	}
	// code.Matches asks the type index for the two functions by path, and the index knows the packages a package
	// imports, never the package itself, so regexp's own calls of its two functions are never walked.
	if compile.Pkg() == context.Package {
		return ""
	}
	index := function_name.Index(compile, compileFunctionNames...)
	if index < 0 {
		return ""
	}
	return compileFunctionNames[index]
}

// escapesOnlyBackslashes is whether every backslash of value, a string literal as written, opens an escaped backslash,
// read left to right: a backslash followed by anything else is another escape, which a raw string literal can't
// write. It reads the literal's runes as the original does, and since a backslash is one byte that no multibyte rune
// holds, its answer is the same as over the bytes.
func escapesOnlyBackslashes(value string) bool {
	escaping := false
	for _, character := range value {
		if !escaping && character == '\\' {
			escaping = true
			continue
		}
		if escaping && character == '\\' {
			escaping = false
			continue
		}
		if escaping {
			return false
		}
	}
	return true
}
