package build_dependence

import (
	"go/ast"
	"go/types"
)

// unitVisitor is newUnit's one walk of a declaration: it counts the syntax nodes under it, the declaration itself
// included, as countNodes does, and gathers the universe's names its identifiers use.
type unitVisitor struct {
	typesInformation *types.Info
	nodes            int
	universeNames    uint64
}

// Visit counts node and reads the universe's name it uses, if any.
func (visitor *unitVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	visitor.nodes++
	if identifier, isIdentifier := node.(*ast.Ident); isIdentifier {
		object := visitor.typesInformation.Uses[identifier]
		if object != nil && object.Parent() == types.Universe {
			visitor.universeNames |= universeNameBit(object.Name())
		}
	}
	return visitor
}
