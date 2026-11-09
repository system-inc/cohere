package intermediate_representation

import (
	"go/ast"
	"go/token"
)

type node struct {
	source ast.Node
}

func (node *node) setSource(source ast.Node) { node.source = source }

func (node *node) Source() ast.Node { return node.source }

func (node *node) Pos() token.Pos {
	if node.source != nil {
		return node.source.Pos()
	}
	return token.NoPos
}
