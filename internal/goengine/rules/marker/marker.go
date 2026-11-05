// Package marker reads the reasoned marker comments that excuse a house rule's finding, `// Not parallel: <reason>`,
// `// Ignored: <reason>`, `// Invariant: <reason>` and their kin, the one way every rule that accepts one reads it,
// save the two named below.
//
// A marker counts only where it opens a comment line, so a sentence that merely mentions it excuses nothing, and a
// marker with nothing after its colon is told apart from one that gives a reason, since it tells the next reader
// nothing. A comment speaks for a node from directly above it, on lines of its own, or from the end of the node's
// last line. A comment trailing the code on the line above a node belongs to that line, not to the node below it.
//
// Two rules keep a reading of their own on purpose and do not use this package: concurrency-no-buffered-wait-delay's
// `// WaitDelay:` and correctness-require-declared-walk's `// Reads:`. Each is a port of an Adamic rule, and keeps
// Adamic's line reading, in which a marker covers its own line and the line after its group, so
// `work() // WaitDelay: why` still excuses the line below. Their ports are held to excuse exactly what Adamic's rules
// excuse, and a `Reads:` glob feeds a planner that reads it the same way, so that reading is not drift for this
// package to absorb.
package marker

import (
	"go/ast"
	"go/token"
	"strings"
)

// Excuse is what a comment says about why a finding is excused. The values are ordered so a stronger excuse compares
// greater, and the excuse several comments give together is the max of theirs.
type Excuse int

const (
	// None is a comment, or no comment, that holds no marker.
	None Excuse = iota
	// WithoutReason is a marker with nothing after its colon. It excuses nothing, and a rule names the missing reason
	// in a message of its own.
	WithoutReason
	// WithReason is a marker followed by a reason, which excuses the finding.
	WithReason
)

// Read is the excuse group gives under marker, the marker's text up to and including its colon, matched as written.
// The marker counts where it opens any line of any comment in the group, after the comment's own slashes, stars and
// spaces, and the text after it on that line, less the */ that closes a block comment, is the reason. A nil group
// says nothing.
func Read(group *ast.CommentGroup, marker string) Excuse {
	if group == nil {
		return None
	}
	found := None
	for _, comment := range group.List {
		for _, line := range strings.Split(comment.Text, "\n") {
			// A starred continuation line of a block comment opens with its star, which is the comment's and not the
			// text's, as the slashes of a line comment are.
			opening := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "/*"))
			reason, hasMarker := strings.CutPrefix(opening, marker)
			if !hasMarker {
				continue
			}
			if strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(reason), "*/")) != "" {
				return WithReason
			}
			found = WithoutReason
		}
	}
	return found
}

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

// NewIndex indexes file's comment groups. Where code sits on a line is read from where the file's nodes begin and end
// in the AST, never from the source text.
func NewIndex(fileSet *token.FileSet, file *ast.File) *Index {
	index := &Index{
		fileSet:          fileSet,
		ownLinesEndingOn: map[int]*ast.CommentGroup{},
		startingOn:       map[int][]*ast.CommentGroup{},
	}
	// The earliest and latest place code begins or ends on each line. A token of code that is no node of its own, a
	// closing brace or a keyword, opens or closes a node on its line, the brace its block's end and the keyword its
	// statement's start, so these bound the code on a line.
	firstCode := map[int]token.Pos{}
	lastCode := map[int]token.Pos{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node.(type) {
		case nil:
			return true
		case *ast.Comment, *ast.CommentGroup:
			return false
		}
		for _, position := range []token.Pos{node.Pos(), node.End()} {
			// An empty field list or another implicit part of the tree has no position, and stands on no line.
			if !position.IsValid() {
				continue
			}
			line := index.line(position)
			if earliest, seen := firstCode[line]; !seen || position < earliest {
				firstCode[line] = position
			}
			if latest, seen := lastCode[line]; !seen || position > latest {
				lastCode[line] = position
			}
		}
		return true
	})
	for _, group := range file.Comments {
		startLine := index.line(group.Pos())
		endLine := index.line(group.End())
		index.startingOn[startLine] = append(index.startingOn[startLine], group)
		// A node ending where the comment starts, `work()// note`, is code before it as much as one ending earlier,
		// and one starting where it ends, `/* note */work()`, is code after it.
		if earliest, seen := firstCode[startLine]; seen && earliest <= group.Pos() {
			continue
		}
		if latest, seen := lastCode[endLine]; seen && latest >= group.End() {
			continue
		}
		index.ownLinesEndingOn[endLine] = group
	}
	return index
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
