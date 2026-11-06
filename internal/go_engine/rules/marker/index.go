package marker

import (
	"go/ast"
	"go/token"
)

// Index is one file's comment groups found by line, built once so each node asks for its comments without a walk of
// its own. Lines are the file's own, never those a //line directive names, since a comment and the code it speaks for
// sit together in the file as written.
type Index struct {
	fileSet *token.FileSet
	// ownLinesEndingOn is each group that has its lines to itself, by the line it ends on: no code before it on its
	// first line and none after it on its last.
	ownLinesEndingOn map[int]*ast.CommentGroup
	// startingOn is every group by the line it starts on.
	startingOn map[int][]*ast.CommentGroup
}

// Above is the comment group directly above node: the one that ends on the line before node's first line and has its
// lines to itself, or nil. A group trailing the code on the line above speaks for that code, one with code after it
// on its last line speaks for that code, and one a blank line away speaks for nothing below it, so none of them is
// found.
func (index *Index) Above(node ast.Node) *ast.CommentGroup {
	return index.ownLinesEndingOn[index.line(node.Pos())-1]
}

// Trailing is every comment group at the end of node: starting on node's last line, after node ends. A comment before
// node on that line, `/* note */ work()`, is not at its end.
func (index *Index) Trailing(node ast.Node) []*ast.CommentGroup {
	trailing := []*ast.CommentGroup{}
	for _, group := range index.startingOn[index.line(node.End())] {
		if group.Pos() >= node.End() {
			trailing = append(trailing, group)
		}
	}
	return trailing
}

// ExcuseFor is the strongest excuse marker gets from the comments that speak for node: the group directly above it
// and the groups at the end of its last line.
func (index *Index) ExcuseFor(node ast.Node, marker string) Excuse {
	found := Read(index.Above(node), marker)
	for _, group := range index.Trailing(node) {
		found = max(found, Read(group, marker))
	}
	return found
}

// line is the file's own line of position, or 0 for no position.
func (index *Index) line(position token.Pos) int {
	return index.fileSet.PositionFor(position, false).Line
}
