package standing_presence

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// requireParallelEdges are the edges testing-require-parallel reads, in the order of the pass's files: each
// (*testing.T).Run in a _test.go file handed, by name, a function or method the package declares with a body and one
// parameter, whose body it judges for its first statement.
//
// It mirrors rules/testing_require_parallel/analyzer.go, which exposes none of it: a file whose own name, past no
// `//line` directive, ends in _test.go (enterFile, analyzer.go:86-87), a call of two arguments through a method value
// that is testing's (*T).Run (judgeSubtest, analyzer.go:108-125, isTestingTMethod and isPointerToTestingT,
// analyzer.go:259-277), and a function named by its argument, an instantiation's taken to its origin, declared in the
// package with a body and one parameter (subtestFunction, analyzer.go:140-187). A function literal is the caller's own
// body, no edge.
func requireParallelEdges(pass *analysis.Pass, declarations map[*types.Func]*ast.FuncDecl) []presenceEdge {
	var edges []presenceEdge
	for _, file := range pass.Files {
		if !strings.HasSuffix(pass.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
			continue
		}
		for _, declaration := range file.Decls {
			caller, _ := declaration.(*ast.FuncDecl)
			ast.Inspect(declaration, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if target := subtestDeclaration(pass, call, declarations); target != nil {
					edges = append(edges, presenceEdge{caller: caller, callee: target,
						reason: RequireParallel.Reason()})
				}
				return true
			})
		}
	}
	return edges
}

// subtestDeclaration is the declaration whose body call runs as a subtest, when call is a (*testing.T).Run handed a
// function of the package by name, and nil otherwise.
func subtestDeclaration(
	pass *analysis.Pass,
	call *ast.CallExpr,
	declarations map[*types.Func]*ast.FuncDecl,
) *ast.FuncDecl {
	if len(call.Args) != 2 {
		return nil
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return nil
	}
	selection, isSelection := pass.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal {
		return nil
	}
	if method, isFunction := selection.Obj().(*types.Func); !isFunction || !isTestingRun(method) {
		return nil
	}
	unwrapped := ast.Unparen(call.Args[1])
	switch instantiated := unwrapped.(type) {
	case *ast.IndexExpr:
		unwrapped = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		unwrapped = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch named := unwrapped.(type) {
	case *ast.Ident:
		object = pass.TypesInfo.Uses[named]
	case *ast.SelectorExpr:
		if selected, isSelected := pass.TypesInfo.Selections[named]; isSelected {
			object = selected.Obj()
		} else {
			object = pass.TypesInfo.Uses[named.Sel]
		}
	}
	function, isFunction := object.(*types.Func)
	if !isFunction {
		return nil
	}
	function = function.Origin()
	declaration := declarations[function]
	if declaration == nil || declaration.Body == nil || function.Signature().Params().Len() != 1 {
		return nil
	}
	return declaration
}

// isTestingRun is whether method is testing's (*T).Run, by its package and its receiver, a pointer to testing's T.
func isTestingRun(method *types.Func) bool {
	if method.Name() != "Run" || method.Pkg() == nil || method.Pkg().Path() != "testing" {
		return false
	}
	receiver := method.Signature().Recv()
	if receiver == nil {
		return false
	}
	pointer, isPointer := types.Unalias(receiver.Type()).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(pointer.Elem()).(*types.Named)
	return isNamed && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "testing" && named.Obj().Name() == "T"
}
