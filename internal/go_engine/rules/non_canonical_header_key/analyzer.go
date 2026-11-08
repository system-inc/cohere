// Package non_canonical_header_key is staticcheck's SA1008, an http.Header map indexed directly by a constant key
// that is not in canonical form, which the Header's methods would never find, ported as a check on syntax that
// reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1008/sa1008.go, whose logic it keeps
// and whose message and fixes it reports word for word, across the whole index expression as the original's do. It
// judges the index expressions the original's inspector reaches: an assignment with an http.Header indexed on its
// left is a write, and nothing inside it is judged, its right side included, the original's
// admitted miss of `h1["foo"] = h2["foo"]`, kept for parity. Any other index of a value whose type is named
// net/http.Header, or an alias of it, by a constant string that http.CanonicalHeaderKey would change is reported:
// a key written as a literal is fixed to the canonical literal, one written as an identifier is wrapped in
// http.CanonicalHeaderKey, and any other spelling, a parenthesized literal or a qualified constant among them, is
// reported with no fix, as the original's switch on the key's own node type leaves it. The fixes are printed by
// staticcheck's analysis/edit, borrowed in this wave as the original uses it, through staticcheck_report.Fixes. The
// original filters no generated files, and neither does the port. The registry runs the port in the original's place,
// under its name, and the original stays only as the oracle: the parity tests beside this file run both over the same
// packages, and the two differ nowhere they hold.
package non_canonical_header_key

import (
	"fmt"
	"go/ast"
	"net/http"
	"strconv"

	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// headerType is the qualified name of the map type whose keys the check judges.
const headerType = "net/http.Header"

// Listener is what the port hears of each package from the dispatcher: every index expression, in preorder, with its
// ancestors. The original's inspector stops descending at an assignment that writes a Header, so an index anywhere
// inside one, its right side included, is never judged; the dispatcher walks every node, so the port leaves alone an
// index with such an assignment among its ancestors, the same indexes.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.IndexExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, stack []ast.Node) {
		index, isIndex := node.(*ast.IndexExpr)
		if !isIndex {
			// Invariant: the dispatcher hands Node only the index expressions it was asked for.
			panic("non_canonical_header_key: the dispatcher handed over a node that is not an index expression")
		}
		for _, ancestor := range stack {
			assignment, isAssignment := ancestor.(*ast.AssignStmt)
			if isAssignment && WritesHeader(context, assignment) {
				return
			}
		}
		CheckIndex(context, index)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1008",
	"SA1008",
	"Flags an http.Header map indexed directly by a constant key not in canonical form, which the Header's "+
		"methods never find: staticcheck's SA1008, ported as a check on syntax.",
	&Listener,
)

// WritesHeader is whether assignment indexes an http.Header on its left, a write the original never judges, and
// whose every part, its right side included, it leaves unvisited.
func WritesHeader(context *rule.Context, assignment *ast.AssignStmt) bool {
	for _, left := range assignment.Lhs {
		// Only an index written as one counts; a parenthesized one is visited as any other index is.
		index, isIndex := left.(*ast.IndexExpr)
		if isIndex && typeutil.IsTypeWithName(context.TypesInfo.TypeOf(index.X), headerType) {
			return true
		}
	}
	return false
}

// CheckIndex judges one index expression: an http.Header read by a constant string key that is not canonical is
// reported, with a fix when the key is a literal or an identifier.
func CheckIndex(context *rule.Context, index *ast.IndexExpr) {
	if !typeutil.IsTypeWithName(context.TypesInfo.TypeOf(index.X), headerType) {
		return
	}
	key, isString := staticcheck_code.ExprToString(context, index.Index)
	if !isString {
		return
	}
	canonical := http.CanonicalHeaderKey(key)
	if key == canonical {
		return
	}
	message := fmt.Sprintf("keys in http.Header are canonicalized, %q is not canonical; fix the constant or use "+
		"http.CanonicalHeaderKey", key)
	// The key's own node decides the fix, its parentheses not looked through, as the original's switch reads it.
	switch keyNode := index.Index.(type) {
	case *ast.BasicLit:
		staticcheck_report.Report(context, index, message, staticcheck_report.Fixes(edit.Fix("Canonicalize header key",
			edit.ReplaceWithString(keyNode, strconv.Quote(canonical)))))
	case *ast.Ident:
		call := &ast.CallExpr{
			Fun:  edit.Selector("http", "CanonicalHeaderKey"),
			Args: []ast.Expr{keyNode},
		}
		staticcheck_report.Report(context, index, message, staticcheck_report.Fixes(edit.Fix(
			"Wrap in http.CanonicalHeaderKey", edit.ReplaceWithNode(context.Fset, keyNode, call))))
	default:
		staticcheck_report.Report(context, index, message)
	}
}
