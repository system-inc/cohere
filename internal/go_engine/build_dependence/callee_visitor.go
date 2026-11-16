package build_dependence

import (
	"go/ast"
)

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
