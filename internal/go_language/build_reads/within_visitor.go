package build_reads

import "go/ast"

// withinVisitor walks an element of a composite literal, or its type, below the root the walk is in, so a function
// literal it is or holds is walked with its own results, as inspect walks one below its root. An expression holds a
// return statement only inside a function literal, so it needs no results of its own. One serves every element of a
// literal, so walking each element alone allocates nothing more (inspectComposite).
type withinVisitor struct {
	walk *useWalk
}

// Visit visits node and goes on into its children when visit says so.
func (within *withinVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil || !within.walk.visit(node, false, nil) {
		return nil
	}
	return within
}
