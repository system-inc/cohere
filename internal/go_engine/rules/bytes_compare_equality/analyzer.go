// Package bytes_compare_equality is staticcheck's S1004, a call of bytes.Compare compared with zero for equality,
// `bytes.Compare(a, b) == 0`, which is `bytes.Equal(a, b)` written longer, ported as a check on types and syntax that
// reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1004/s1004.go, whose logic it keeps and whose
// message it reports word for word, at the same range, the comparison, with the original's fix where that compiles.
// The original matches its one query,
// `(BinaryExpr (CallExpr (Symbol "bytes.Compare") args) op@(Or "==" "!=") (IntegerLiteral "0"))`, through
// staticcheck's pattern language, and builds its fix from two more, `(CallExpr (SelectorExpr (Ident "bytes") (Ident
// "Equal")) args)` and its negation, through edit.ReplaceWithPattern. The port asks the same of each binary
// expression directly, reading pattern/match.go for what each construct of the query matches, builds the fix's
// syntax itself as pattern.NodeToAST builds it, around the call's own arguments, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ only where the original's fix names the package bytes by a name the
// file does not give it there, each case recorded in the differences fixture: the port moves the fix to the name the
// matched call uses, and withholds the fix where no name it could write calls bytes.Equal.
package bytes_compare_equality

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every binary expression, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.BinaryExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		binary, isBinary := node.(*ast.BinaryExpr)
		if !isBinary {
			// Invariant: the dispatcher hands Node only the binary expressions it was asked for.
			panic("bytes_compare_equality: the dispatcher handed over a node that is not a binary expression")
		}
		Check(context, binary)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1004",
	"S1004",
	"Flags bytes.Compare compared with zero for equality: staticcheck's S1004, ported.",
	&Listener,
)

// Check judges one binary expression: a call of bytes.Compare on its left, compared by == or != with an integer
// literal of value zero on its right, asks what bytes.Equal answers, and the fix calls bytes.Equal, negated for !=, on
// the call's own arguments, under the name equalFunction finds for it, or is left out where none names it.
func Check(context *rule.Context, binary *ast.BinaryExpr) {
	// The bytes package is free to use bytes.Compare as it sees fit, and so are its external tests.
	if context.Package.Path() == "bytes" || context.Package.Path() == "bytes_test" {
		return
	}
	// `op@(Or "==" "!=")` is the operator alone, so an ordering, `< 0` or `> 0`, is left alone.
	if binary.Op != token.EQL && binary.Op != token.NEQ {
		return
	}
	// The pattern sees through parentheses around the call, and holds the call on the left: `0 == bytes.Compare(a,
	// b)` is not the query's shape.
	call, isCall := ast.Unparen(binary.X).(*ast.CallExpr)
	if !isCall || !isBytesCompare(context, call.Fun) {
		return
	}
	// `(IntegerLiteral "0")` is staticcheck_code.IntegerLiteral, and the string "0" against its type and value is the
	// constant as it prints, not compared as a number: 0x0, 00 and -0 print as 0, and a named constant is no literal.
	zero, isLiteral := staticcheck_code.IntegerLiteral(context, binary.Y)
	if !isLiteral || zero.Value == nil || zero.Value.String() != "0" {
		return
	}

	// The message names bytes.Equal as the original's does, whatever the file calls the package.
	prefix := ""
	if binary.Op == token.NEQ {
		prefix = "!"
	}
	fixes := []analysis.SuggestedFix{}
	if equalName, isNamed := equalFunction(context, binary, call.Fun); isNamed {
		// `args` binds the call's own argument slice, which the message renders and the fix's call holds, the same
		// nodes, not copies, as pattern.NodeToAST places a binding's value, so format prints them from their own
		// positions.
		equal := &ast.CallExpr{Fun: equalName, Args: call.Args}
		var replacement ast.Expr = equal
		if binary.Op == token.NEQ {
			replacement = &ast.UnaryExpr{Op: token.NOT, X: equal}
		}
		fixes = append(fixes,
			edit.Fix("Simplify use of bytes.Compare", edit.ReplaceWithNode(context.FileSet, binary, replacement)))
	}
	staticcheck_report.Report(context, binary,
		"should use "+prefix+"bytes.Equal("+renderedArguments(context, call.Args)+") instead",
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(fixes...))
}

// equalFunction is how the fix names bytes.Equal where the comparison stands, and whether any name does. The original
// writes `bytes.Equal` always, the pattern's `(Ident "bytes")`, which fails to compile, or calls something else, where
// bytes names no import of the package bytes there: a file that imports it under another name or into its own scope,
// or a scope that declares a bytes of its own. The port writes `bytes.Equal` wherever bytes names that import, which
// is the original's fix to the byte, and elsewhere moves the fix to the name the matched call names bytes.Compare by,
// which stands in the same scope as the comparison: the package name of a selector, `b.Equal`, or for a dot import
// Equal alone, where Equal is bytes'. Where neither names it, a dot import whose Equal the code declares an Equal
// over, the port keeps the finding and withholds the fix. Each case is recorded in the differences fixture.
func equalFunction(context *rule.Context, binary *ast.BinaryExpr, compareFunction ast.Expr) (ast.Expr, bool) {
	scope := context.Package.Scope().Innermost(binary.Pos())
	if scope == nil {
		// Invariant: an expression stands in a file, and the type checker records a scope for every file, which
		// Innermost of the package's scope searches.
		panic("bytes_compare_equality: no scope holds a binary expression")
	}
	// The comparison's position counts only what is declared before it in its own block, as the comparison's place
	// does.
	if _, named := scope.LookupParent("bytes", binary.Pos()); importsBytes(named) {
		return edit.Selector("bytes", "Equal"), true
	}
	switch named := ast.Unparen(compareFunction).(type) {
	case *ast.SelectorExpr:
		// A selector that names a package's function selects it from the package's name, an identifier.
		qualifier, isIdentifier := named.X.(*ast.Ident)
		if isIdentifier && importsBytes(context.TypesInfo.Uses[qualifier]) {
			return edit.Selector(qualifier.Name, "Equal"), true
		}
	case *ast.Ident:
		// An identifier alone names bytes.Compare only through a dot import, which brings bytes.Equal into the file's
		// scope as Equal, unless a scope nearer the comparison declares an Equal of its own.
		compare := context.TypesInfo.Uses[named]
		if compare == nil || compare.Pkg() == nil {
			return nil, false
		}
		_, equal := scope.LookupParent("Equal", binary.Pos())
		if equal != nil && equal == context.LookupIn(compare.Pkg(), "Equal") {
			return &ast.Ident{Name: "Equal"}, true
		}
	}
	return nil, false
}

// importsBytes is whether object is the name of an import of the standard library's package bytes.
func importsBytes(object types.Object) bool {
	packageName, isPackageName := object.(*types.PkgName)
	return isPackageName && packageName.Imported().Path() == "bytes"
}

// isBytesCompare is whether function names bytes.Compare, as the pattern's `(Symbol "bytes.Compare")` matches it: an
// identifier or a selector, parenthesized or not and instantiated or not, whose object is the function bytes.Compare,
// so a dot import and a renamed import name it, and a method or a variable named Compare does not. Symbol also names
// a package's variable, constant or type by its package's path and name, and none spelled bytes.Compare exists, since
// only the standard library's bytes has that path and it declares Compare as a function alone.
func isBytesCompare(context *rule.Context, function ast.Expr) bool {
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
		return false
	}
	// FullName spells a method with its receiver, `(T).Compare`, and a function by its package's path, which is
	// bytes for the standard library's bytes alone.
	compare, isFunction := object.(*types.Func)
	return isFunction && compare.FullName() == "bytes.Compare"
}

// renderedArguments is arguments as report.RenderArgs renders them for a message: each printed by format.Node over
// the package's file set, as report.Render prints one, joined by a comma and a space.
func renderedArguments(context *rule.Context, arguments []ast.Expr) string {
	rendered := []string{}
	for _, argument := range arguments {
		text := &bytes.Buffer{}
		if formatError := format.Node(text, context.FileSet, argument); formatError != nil {
			// Invariant: format.Node fails only on a node it cannot print, and every argument is syntax the package
			// was parsed and type-checked from; report.Render panics on the same error.
			panic(formatError)
		}
		rendered = append(rendered, text.String())
	}
	return strings.Join(rendered, ", ")
}
