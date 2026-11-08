// Package appends is vet's appends pass, a call to the builtin append that passes the slice alone and no values to
// append to it, which is always a no-op and often a sign of an underlying mistake. A function the package declares
// under the name append, or a variable holding one, is not the builtin, so only a call the type checker resolves to
// the builtin is judged.
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
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place. Doc is the section of the
// original's doc.go its analyzerutil.MustExtractDoc extracts, written out, since production code must not import the
// original.
var Analyzer = &analysis.Analyzer{
	Name: "appends",
	Doc: "check for missing values after append\n\n" +
		"This checker reports calls to append that pass\n" +
		"no values to be appended to the slice.\n\n" +
		"\ts := []string{\"a\", \"b\", \"c\"}\n" +
		"\t_ = append(s)\n\n" +
		"Such calls are always no-ops and often indicate an\n" +
		"underlying mistake.",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/appends",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("appends: inspect's result is missing")
	}
	// All visits each call in preorder, as the original's Preorder does, and hands it over typed, so a call nested in
	// another's arguments is reported after the call that holds it.
	for call := range inspector.All[*ast.CallExpr](inspected) {
		// Callee looks through parentheses around the called name, `(append)(values)`, and resolves it through Uses,
		// so a package's own function or a local variable named append is not the builtin, and a conversion, which
		// calls nothing, resolves to no object at all.
		builtin, isBuiltin := typeutil.Callee(pass.TypesInfo, call).(*types.Builtin)
		if !isBuiltin || builtin.Name() != "append" || len(call.Args) != 1 {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:     call.Pos(),
			End:     call.End(),
			Message: "append with no values",
		})
	}
	return nil, nil
}
