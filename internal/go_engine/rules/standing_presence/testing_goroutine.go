package standing_presence

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// testingGoroutineEdges are the edges testinggoroutine reads, in the order of the pass's files: each go statement it
// hears that statically calls a function or method the package declares with a body, whose body it judges as the
// goroutine's region, one level, following no call from there.
//
// It mirrors rules/vet/testing_goroutine, which exposes none of it: a package that imports testing (gather and
// imports, analyzer.go:103-124, functions.go:107-111), a go statement anywhere but in a function declaration whose
// parameters are not written `*testing.T` or `*testing.B` (isPruned, package_state.go:45-59,
// hasBenchmarkOrTestParameters and isTestingTOrB, analyzer.go:165-190), whose typeutil.StaticCallee has a declaration
// in the package (goAsynchronousCall, analyzer.go:204-222). The rule first asks whether the callee is a variable first
// assigned a function literal, which is then its region; a variable is no static callee, so asking for the static
// callee alone finds the same declarations, as the census counter reads it.
func testingGoroutineEdges(pass *analysis.Pass, declarations map[*types.Func]*ast.FuncDecl) []presenceEdge {
	importsTesting := slices.ContainsFunc(pass.Pkg.Imports(), func(imported *types.Package) bool {
		return imported.Path() == "testing"
	})
	if !importsTesting {
		return nil
	}
	var edges []presenceEdge
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			// The rule never descends into a declaration that takes no test, and hears an initializer's whole.
			caller, isFunction := declaration.(*ast.FuncDecl)
			if isFunction && !takesTestOrBenchmark(caller) {
				continue
			}
			ast.Inspect(declaration, func(node ast.Node) bool {
				statement, isGo := node.(*ast.GoStmt)
				if !isGo {
					return true
				}
				callee := typeutil.StaticCallee(pass.TypesInfo, statement.Call)
				if target := declarations[callee]; target != nil && target.Body != nil {
					edges = append(edges, presenceEdge{caller: caller, callee: target,
						reason: TestingGoroutine.Reason()})
				}
				return true
			})
		}
	}
	return edges
}

// takesTestOrBenchmark is whether declaration's parameters are written `*testing.T` or `*testing.B`, by the package's
// name `testing` (rules/vet/testing_goroutine/analyzer.go:165-190).
func takesTestOrBenchmark(declaration *ast.FuncDecl) bool {
	return slices.ContainsFunc(declaration.Type.Params.List, func(parameter *ast.Field) bool {
		star, isStar := parameter.Type.(*ast.StarExpr)
		if !isStar {
			return false
		}
		selector, isSelector := star.X.(*ast.SelectorExpr)
		if !isSelector {
			return false
		}
		packageName, isIdentifier := selector.X.(*ast.Ident)
		return isIdentifier && packageName.Name == "testing" && (selector.Sel.Name == "B" || selector.Sel.Name == "T")
	})
}
