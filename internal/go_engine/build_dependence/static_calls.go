package build_dependence

import (
	"go/ast"
	"go/types"
)

// staticCallees are the functions bodies call statically, each as its generic declares it and once: a plain or
// qualified call of a function, or a method call on a receiver that is no interface, its method declared by a type
// rather than an interface, an instantiation's type arguments looked past. These are the calls the counter's
// direct-callers column found, whose callee's body, and so whose facts, a build can change while the caller's own types
// stay the same (#0ghk7vj). They are a slice of the collector's store, which the next package's walk writes over, so
// newRecord reads them into the record's own slice; nil when bodies call none.
func (collecting *collector) staticCallees(bodies ...ast.Node) []types.Object {
	clear(collecting.calleeSeen)
	start := len(collecting.calleeStore)
	for _, body := range bodies {
		ast.Walk(calleeVisitor{collecting: collecting}, body)
	}
	collecting.calleeSeenMost = max(collecting.calleeSeenMost, len(collecting.calleeSeen))
	if len(collecting.calleeStore) == start {
		return nil
	}
	// The slice's capacity ends with it, so nothing appended to it could write over the next unit's callees.
	return collecting.calleeStore[start:len(collecting.calleeStore):len(collecting.calleeStore)]
}

// calleeVisitor is staticCallees' walk of a body, which makes no closure for each body: a struct of one pointer is
// held in an interface as the pointer itself.
type calleeVisitor struct {
	collecting *collector
}

// Visit collects the function a call calls statically, once for the unit.
func (visitor calleeVisitor) Visit(node ast.Node) ast.Visitor {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return visitor
	}
	function := staticCallee(visitor.collecting.typesInformation, call)
	if function != nil && !visitor.collecting.calleeSeen[function] {
		visitor.collecting.calleeSeen[function] = true
		visitor.collecting.calleeStore = append(visitor.collecting.calleeStore, function)
	}
	return visitor
}

// staticCallee is the function call calls statically, as its generic declares it, nil for any other call.
func staticCallee(typesInformation *types.Info, call *ast.CallExpr) types.Object {
	callee := ast.Unparen(call.Fun)
	switch instantiated := callee.(type) {
	case *ast.IndexExpr:
		callee = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		callee = ast.Unparen(instantiated.X)
	}
	var function *types.Func
	switch callee := callee.(type) {
	case *ast.Ident:
		function, _ = typesInformation.Uses[callee].(*types.Func)
	case *ast.SelectorExpr:
		selection := typesInformation.Selections[callee]
		if selection == nil {
			function, _ = typesInformation.Uses[callee.Sel].(*types.Func)
			break
		}
		if selection.Kind() != types.MethodVal || types.IsInterface(selection.Recv()) {
			return nil
		}
		function, _ = selection.Obj().(*types.Func)
		if function != nil && types.IsInterface(function.Signature().Recv().Type()) {
			return nil
		}
	}
	if function == nil {
		return nil
	}
	return function.Origin()
}
