package build_dependence

import (
	"go/ast"
)

// bodyVisitor is references' walk of a body, which makes no closure for each body: a struct of one pointer is held
// in an interface as the pointer itself.
type bodyVisitor struct {
	walk *bodyWalk
}

// Visit collects what node reads.
func (visitor bodyVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	visitor.walk.visit(node)
	return visitor
}
