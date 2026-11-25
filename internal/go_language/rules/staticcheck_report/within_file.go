package staticcheck_report

import (
	"go/ast"
	"go/token"
)

// withinFile walks a node to learn whether every node within it that has a position starts in tokenFile, setting
// outside at the first that doesn't, where it stops.
type withinFile struct {
	tokenFile *token.File
	outside   bool
}

// Visit goes on into node's children while every node so far starts in the file, and stops at the first that doesn't.
func (within *withinFile) Visit(node ast.Node) ast.Visitor {
	if node == nil || within.outside {
		return nil
	}
	// The bounds the file set's own lookup reads: a file holds the positions from its base to its base plus its size.
	if position := node.Pos(); position.IsValid() &&
		(int(position) < within.tokenFile.Base() || int(position) > within.tokenFile.Base()+within.tokenFile.Size()) {
		within.outside = true
		return nil
	}
	return within
}
