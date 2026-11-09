// Package swapped_seek_arguments is staticcheck's SA1013, a call of an io.Seeker's Seek whose first argument is one
// of io's whence constants, where the offset belongs, ported as a check on syntax that reports through the kit every
// port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1013/sa1013.go, whose logic it keeps and
// whose message and fix it reports word for word, at the call's Pos and End, through staticcheck_report.Report, the
// port of the report.Report the original calls, proven against it beside it. The original matches
// (CallExpr fun@(SelectorExpr _ (Ident "Seek")) [arg1@(SelectorExpr _ (Symbol (Or "io.SeekStart" "io.SeekCurrent"
// "io.SeekEnd"))) arg2]) over every call; the port asks the same by types and syntax, without the pattern package:
// the call's function, its parentheses looked through, selects a name Seek; there are exactly two arguments; and the
// first, its parentheses looked through, selects one of io's three whence constants, as the pattern's Symbol names a
// package-level constant. The method must then be a method value whose signature is io.Seeker's Seek, as
// staticcheck_code.IsMethod answers against staticcheck's knowledge.Signatures, data the original reads too and
// imported in this wave, which wave 5 ports.
//
// The original's code.Matches first asks code.CouldMatchAny whether the package's type index holds any of the three
// constants, and skips the package when it holds none; the port drops that skip, since it never skips a call the
// pattern would match. A selector whose name resolves to io's package-level constant is a qualified identifier, so
// the file imports io directly, and the index holds every package a file imports directly, blank imports included.
//
// Its suggested fix is the call written again with the two arguments swapped, the function and both arguments as the
// pattern bound them, parentheses looked through and so dropped, printed by the kit's Render as the original's
// code.EditMatch prints its pattern's replacement with format.Node, and passed whole through staticcheck's
// analysis/edit, edit.Fix and edit.ReplaceWithString, borrowed in this wave, and staticcheck_report.Fixes; wave 5
// ports edit. One difference no fixture can reach: Render panics on a format error where code.EditMatch drops it, and
// format.Node fails only on syntax it cannot print. The original filters no generated file, and the port none. The
// original keeps no state from one call to the next. The registry runs the port in the original's place, under its
// name, and the original stays only as the oracle: the parity tests beside this file run both over the same packages,
// fixes included, and the two differ nowhere they hold.
package swapped_seek_arguments

import (
	"go/ast"
	"go/types"
	"slices"

	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("swapped_seek_arguments: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1013",
	"SA1013",
	"Flags an io.Seeker's Seek given one of io's whence constants first, where the offset belongs: "+
		"staticcheck's SA1013, ported as a check on syntax.",
	&Listener,
)

// whenceConstants are io's three whence constants, by the names the pattern's Symbol gives a package-level constant.
var whenceConstants = []string{"io.SeekStart", "io.SeekCurrent", "io.SeekEnd"}

// Check judges one call: an io.Seeker's Seek given a whence constant first is reported, with the fix that swaps its
// two arguments.
func Check(context *rule.Context, call *ast.CallExpr) {
	// The pattern's matcher looks through parentheses on every node, so `(seeker.Seek)(io.SeekStart, 0)` and
	// `seeker.Seek((io.SeekStart), 0)` match, and its two-element argument list matches exactly two arguments.
	function, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector || function.Sel.Name != "Seek" || len(call.Args) != 2 {
		return
	}
	// A dot import's bare SeekStart is an identifier, no selector, so the pattern refuses it, and so does the port.
	whence, isWhenceSelector := ast.Unparen(call.Args[0]).(*ast.SelectorExpr)
	if !isWhenceSelector || !isWhenceSymbol(context, whence.Sel) {
		return
	}
	if !staticcheck_code.IsMethod(context, function, "Seek", knowledge.Signatures["(io.Seeker).Seek"]) {
		return
	}
	// The fix is the replacement pattern `(CallExpr fun [arg2 arg1])` as pattern.NodeToAST builds it: the bound nodes
	// themselves, their parentheses already looked through, so format prints each from its own positions.
	swapped := &ast.CallExpr{Fun: function, Args: []ast.Expr{ast.Unparen(call.Args[1]), whence}}
	staticcheck_report.Report(context, call,
		"the first argument of io.Seeker is the offset, but an io.Seek* constant is being used instead",
		staticcheck_report.Fixes(edit.Fix("Swap arguments",
			edit.ReplaceWithString(call, staticcheck_report.Render(context, swapped)))))
}

// isWhenceSymbol is whether name names one of io's whence constants as the pattern's Symbol names a constant or a
// variable: one declared at its package's top level, called by its package's path and its own name.
func isWhenceSymbol(context *rule.Context, name *ast.Ident) bool {
	object := context.TypesInfo.ObjectOf(name)
	switch object.(type) {
	case *types.Const, *types.Var:
	default:
		return false
	}
	if object.Pkg() == nil || object.Parent() != object.Pkg().Scope() {
		return false
	}
	return slices.Contains(whenceConstants, object.Pkg().Path()+"."+object.Name())
}
