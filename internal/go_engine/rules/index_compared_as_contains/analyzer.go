// Package index_compared_as_contains is staticcheck's S1003, a call of strings.Index, IndexAny or IndexRune, or of
// bytes', compared with -1 or 0 to ask whether one thing holds another, `strings.Index(x, y) != -1`, which is
// `strings.Contains(x, y)` written longer, ported as a check on syntax that reports through the kit every port of
// staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1003/s1003.go, whose message and fix it
// reports word for word, at the same range, the comparison. The original uses no pattern: it walks every binary
// expression, reads the right operand through code.ExprToInt, and judges the left by syntax alone, a call of a
// selector whose left is an identifier spelled strings or bytes. The port asks the same of the right operand, through
// staticcheck_code.ExprToInt, and of the call's shape, but judges its qualifier by object: an import of the standard
// library's strings or bytes, under whatever name. It builds the fix's syntax as the original builds it, around the
// call's own qualifier and arguments, so the fix names the package as the call does, and reports through
// staticcheck_report.Report where the original reports through report.Report, with the fix made by staticcheck's
// analysis/edit, borrowed for this wave.
//
// It parts from the original where the original is wrong, by the ruling that a port judges by object (#zhfsft8): a
// variable, or another path's package, spelled strings or bytes is no call of the standard library's, and the
// original's fix for it calls a Contains that its methods or functions need not have, so the port drops it; and a
// renamed import of strings or bytes is the same call under another name, which the port finds, its fix under that
// name. The registry runs the port in the original's place, under its name, and the original stays only as the oracle:
// the parity tests beside this file run both over the same packages, and the two differ only where the differences
// fixture records it.
package index_compared_as_contains

import (
	"go/ast"
	"go/token"
	"go/types"

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
			panic("index_compared_as_contains: the dispatcher handed over a node that is not a binary expression")
		}
		Check(context, binary)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1003",
	"S1003",
	"Flags strings.Index or bytes.Index compared to ask for containment: staticcheck's S1003, ported.",
	&Listener,
)

// Check judges one binary expression: a call of strings' or bytes' Index, IndexAny or IndexRune through the name its
// file imports the package under, on its left, compared with -1 or 0 on its right in a way that asks for
// containment, and the fix calls Contains, ContainsAny or ContainsRune through the same name on the call's own
// arguments, negated where the comparison asks for absence.
func Check(context *rule.Context, binary *ast.BinaryExpr) {
	// code.ExprToInt reads the constant the type checker recorded, so -1 parenthesized, a named constant and a
	// conversion count as the literal does, and a constant too large for an int64 counts as none.
	value, isInteger := staticcheck_code.ExprToInt(context, binary.Y)
	if !isInteger {
		return
	}
	contained, asksContainment := containment(value, binary.Op)
	if !asksContainment {
		return
	}

	// The original reads the left operand without looking through parentheses, at any level: `(strings.Index(x,
	// y)) != -1` and `(strings).Index(x, y) != -1` are left alone, and so is the call on the right.
	call, isCall := binary.X.(*ast.CallExpr)
	if !isCall {
		return
	}
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return
	}
	packageName, isIdentifier := selector.X.(*ast.Ident)
	if !isIdentifier {
		return
	}
	// The qualifier is judged by its object, where the original judges it by its spelling: an import of the
	// standard library's strings or bytes under any name, so a renamed import is found and a variable or another
	// path's package spelled strings or bytes is not. A dot import calls Index through no qualifier at all, and is
	// left alone, as the original leaves it, since a bare Contains in its fix could name a local of the same name.
	imported, isPackageName := context.TypesInfo.ObjectOf(packageName).(*types.PkgName)
	if !isPackageName {
		return
	}
	if imported.Imported().Path() != "strings" && imported.Imported().Path() != "bytes" {
		return
	}
	var name string
	switch selector.Sel.Name {
	case "IndexRune":
		name = "ContainsRune"
	case "IndexAny":
		name = "ContainsAny"
	case "Index":
		name = "Contains"
	default:
		return
	}

	// The replacement holds the call's own qualifier and argument slice, the same nodes, not copies, as the
	// original's does, so format prints them from their own positions and the fix names the package as the call
	// does. It holds no ellipsis, as the original's does not, which loses nothing here, since none of the six
	// functions is variadic.
	var replacement ast.Expr = &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: packageName, Sel: &ast.Ident{Name: name}},
		Args: call.Args,
	}
	if !contained {
		replacement = &ast.UnaryExpr{Op: token.NOT, X: replacement}
	}
	fix := edit.Fix("Simplify use of "+staticcheck_report.Render(context, call.Fun),
		edit.ReplaceWithNode(context.Fset, binary, replacement))
	staticcheck_report.Report(context, binary,
		"should use "+staticcheck_report.Render(context, replacement)+" instead",
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(fix))
}

// containment is whether comparing an index with value by operator asks if the one holds the other, and if it does,
// contained, whether the comparison is true when it holds it. It is the original's table of allowed operators by
// value: an index compared with -1 by > or != asks for containment and by == for absence, and with 0 by >= for
// containment and by < for absence. Every other pairing, `> 0`, `>= -1`, `!= 0`, `== 0`, asks something else.
func containment(value int64, operator token.Token) (contained bool, asksContainment bool) {
	switch {
	case value == -1 && (operator == token.GTR || operator == token.NEQ):
		return true, true
	case value == -1 && operator == token.EQL:
		return false, true
	case value == 0 && operator == token.GEQ:
		return true, true
	case value == 0 && operator == token.LSS:
		return false, true
	default:
		return false, false
	}
}
