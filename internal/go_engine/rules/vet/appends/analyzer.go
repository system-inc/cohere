// Package appends is vet's appends pass, a call to the builtin append that passes the slice alone and no values to
// append to it, which is always a no-op and often a sign of an underlying mistake. A function the package declares
// under the name append, or a variable holding one, is not the builtin, so only a call the type checker resolves to
// the builtin is judged. It sits on the engine's syntax dispatcher, hearing each call on the walk the dispatcher makes
// once for every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/appends/appends.go, whose logic
// it keeps and whose message it reports word for word, over the same range. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture.
package appends

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// name is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const name = "appends"

// doc is the section of the original's doc.go its analyzerutil.MustExtractDoc extracts, written out, since production
// code must not import the original.
const doc = "check for missing values after append\n\n" +
	"This checker reports calls to append that pass\n" +
	"no values to be appended to the slice.\n\n" +
	"\ts := []string{\"a\", \"b\", \"c\"}\n" +
	"\t_ = append(s)\n\n" +
	"Such calls are always no-ops and often indicate an\n" +
	"underlying mistake."

// url is the original's address.
const url = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/appends"

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, as the original's
// Preorder visits them, so a call nested in another's arguments is reported after the call that holds it.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node:  checkCall,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+name, name, doc, &Listener)
	analyzer.URL = url
	return analyzer
}

// checkCall reports node when it calls the builtin append with the slice alone.
func checkCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	// Callee looks through parentheses around the called name, `(append)(values)`, and resolves it through Uses, so a
	// package's own function or a local variable named append is not the builtin, and a conversion, which calls
	// nothing, resolves to no object at all.
	builtin, isBuiltin := typeutil.Callee(context.TypesInfo, call).(*types.Builtin)
	if !isBuiltin || builtin.Name() != "append" || len(call.Args) != 1 {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: "append with no values",
	})
}
