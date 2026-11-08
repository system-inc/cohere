// Package defers is vet's defers pass, a call to time.Since inside a defer statement but outside a function literal.
// A defer evaluates its function value and its arguments where it stands, and only the outermost call waits, so
// `defer recordLatency(time.Since(start))` measures nothing: time.Since runs at once, and the latency recorded is the
// moment between start and the defer. Calls inside a function literal run when the literal does, so they are left
// alone.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/defers/defers.go, whose logic it
// keeps and whose message it reports word for word, at the same position. That includes the deferred call itself:
// `defer time.Since(start)` is reported too, as the original reports it, since its argument is evaluated early and
// its result thrown away. The registry runs the port in the original's place, and the original stays the oracle: the
// parity tests beside this file run both over the same packages. The two agree everywhere, so there is no differences
// fixture.
package defers

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "defers",
	Doc: "report common mistakes in defer statements\n\n" +
		"The defers analyzer reports a diagnostic when a defer statement would\n" +
		"result in a non-deferred call to time.Since, as experience has shown\n" +
		"that this is nearly always a mistake.\n\n" +
		"For example:\n\n" +
		"\tstart := time.Now()\n" +
		"\t...\n" +
		"\tdefer recordLatency(time.Since(start)) // error: call to time.Since is not deferred\n\n" +
		"The correct code is:\n\n" +
		"\tdefer func() { recordLatency(time.Since(start)) }()",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/defers",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	// A package that does not import time cannot name time.Since, so it is passed over before the walk, as the
	// original passes it over. Nothing reaches time.Since without the import, so this saves the walk and decides
	// nothing: no input makes a finding that skipping this check would add.
	if !importsPackage(pass.Pkg, "time") {
		return nil, nil
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("defers: inspect's result is missing")
	}
	// All visits each defer statement in preorder, as the original's Preorder does, and hands it over typed. A defer
	// inside a function literal inside a defer is a defer statement of its own, so it is visited in its turn.
	for deferred := range inspector.All[*ast.DeferStmt](inspected) {
		// Inspect visits the deferred call itself first, then what it holds in preorder: its function value, its
		// arguments, and every expression inside them.
		ast.Inspect(deferred.Call, func(node ast.Node) bool {
			switch visited := node.(type) {
			case *ast.CallExpr:
				// Callee looks through parentheses and resolves a qualified, aliased or dot-imported name to its
				// object. A call through a variable or field holding time.Since resolves to that variable, so it is
				// not judged, as the original does not judge it. Of isFunctionNamed's checks only the kind and the
				// path decide anything here: package time declares no method named Since, and a function local to
				// package time would be judged only in a package that imports itself, so the checks for a receiver
				// and for the package's scope are kept for fidelity, and no input makes either one fire.
				if isFunctionNamed(typeutil.Callee(pass.TypesInfo, visited), "time", "Since") {
					pass.Report(analysis.Diagnostic{
						Pos:     visited.Pos(),
						Message: "call to time.Since is not deferred",
					})
				}
			case *ast.FuncLit:
				// A function literal's body runs when the literal is called, so what it holds is not evaluated by
				// the defer, and the walk does not enter it.
				return false
			}
			return true
		})
	}
	return nil, nil
}

// importsPackage is whether importer imports the package at path directly. x/tools' typesinternal.Imports.
func importsPackage(importer *types.Package, path string) bool {
	for _, dependency := range importer.Imports() {
		if dependency.Path() == path {
			return true
		}
	}
	return false
}

// isFunctionNamed is whether object is a function, not a method, declared at the top level of the package at
// packagePath under one of names, and false when object is nil. x/tools' typesinternal.IsFunctionNamed.
func isFunctionNamed(object types.Object, packagePath string, names ...string) bool {
	function, isFunction := object.(*types.Func)
	return isFunction &&
		isPackageLevel(object) &&
		function.Pkg().Path() == packagePath &&
		function.Signature().Recv() == nil &&
		slices.Contains(names, function.Name())
}

// isPackageLevel is whether object is declared in its package's scope. x/tools' typesinternal.IsPackageLevel.
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}
