package unreachable

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
)

// deleteStatement is the edits that delete the statement at statementCursor, in file, when it knows the context the
// statement stands in, and nil otherwise. x/tools' refactor.DeleteStmt, translated whole, though this pass reports
// only a statement of a block's or a clause's list: findDead comes to every other statement it walks, a loop's or an
// if's body, an else, a labeled statement's statement, reachable, so the arms below for the heads of for, if and
// switch statements cannot be reached from here, and are kept so the translation stays the original's.
//
// A statement alone on its lines is deleted with those lines, its comments included. Where other syntax shares a
// line, only the comments that can only be the statement's go with it. The tree does not record where semicolons
// are, so in `first() /*A*/; second()` the comment could be either statement's, and deleting either keeps it.
func deleteStatement(file *token.File, statementCursor inspector.Cursor) []analysis.TextEdit {
	statement, isStatement := statementCursor.Node().(ast.Stmt)
	if !isStatement {
		// Invariant: report finds the cursor of a statement it was handed. The original asserts the same.
		panic(fmt.Sprintf("unreachable: the cursor to delete holds a %T, not a statement", statementCursor.Node()))
	}
	statementStartLine := file.Line(statement.Pos())
	statementEndLine := file.Line(statement.End())

	// leftSyntax and rightSyntax are the parts of the parent on the statement's first and last lines.
	var leftSyntax token.Pos
	var rightSyntax token.Pos

	// use remembers the positions, left and right, of the parent's syntax that are on the statement's lines.
	use := func(left token.Pos, right token.Pos) {
		if file.Line(left) == statementStartLine {
			leftSyntax = left
		}
		if file.Line(right) == statementEndLine {
			rightSyntax = right
		}
	}

	leftComments, rightComments := commentsBeside(file, enclosingFileOf(statementCursor), statement)

	// leftStatement and rightStatement are the end and start of sibling statements on the statement's lines.
	var leftStatement token.Pos
	var rightStatement token.Pos
	inStatementList := false
	parentCursor := statementCursor.Parent()
	switch parent := parentCursor.Node().(type) {
	case *ast.BlockStmt:
		use(parent.Lbrace, parent.Rbrace)
		inStatementList = true
	case *ast.CaseClause:
		use(parent.Colon, enclosingBlockRightBrace(parentCursor))
		inStatementList = true
	case *ast.CommClause:
		// Unreached from this pass, which never walks a clause's communication.
		if parent.Comm == statement {
			// Perhaps the whole clause was meant, so the original deletes nothing.
			return nil
		}
		use(parent.Colon, enclosingBlockRightBrace(parentCursor))
		inStatementList = true
	case *ast.ForStmt:
		// Unreached from this pass, as every arm below is: see the doc above.
		use(parent.For, parent.Body.Lbrace)
		// The initialization, condition and post statement are no list, so a sibling on the line is found here.
		if parent.Init != nil && parent.Cond != nil && statement == parent.Init &&
			file.Line(parent.Cond.Pos()) == file.Line(statement.End()) {
			rightStatement = parent.Cond.Pos()
		} else if parent.Post != nil && parent.Cond != nil && statement == parent.Post &&
			file.Line(parent.Cond.End()) == file.Line(statement.Pos()) {
			leftStatement = parent.Cond.End()
		}
	case *ast.IfStmt:
		switch statement {
		case parent.Init:
			use(parent.If, parent.Body.Lbrace)
		case parent.Else:
			// Deleting the else's block would leave its `else`, whose position the tree does not hold.
			return nil
		}
	case *ast.SwitchStmt:
		use(parent.Switch, parent.Body.Lbrace)
	case *ast.TypeSwitchStmt:
		if statement == parent.Assign {
			// The `.(type)` is not deleted.
			return nil
		}
		use(parent.Switch, parent.Body.Lbrace)
	default:
		// A context the original does not know, so it deletes nothing.
		return nil
	}

	if inStatementList {
		// The siblings, if any, on the statement's lines. The sibling before may be no statement but the last of a
		// case's expressions, which shares the clause's children.
		previous, hasPrevious := statementCursor.PrevSibling()
		if hasPrevious && file.Line(previous.Node().End()) == statementStartLine {
			if _, isPreviousStatement := previous.Node().(ast.Stmt); isPreviousStatement {
				leftStatement = previous.Node().End()
			}
		}
		next, hasNext := statementCursor.NextSibling()
		if hasNext && file.Line(next.Node().Pos()) == statementEndLine {
			rightStatement = next.Node().Pos()
		}
	}

	// The edit's left and right limits.
	var leftEdit token.Pos
	var rightEdit token.Pos
	switch {
	case leftStatement.IsValid():
		// A statement before on the line keeps the comments between: `first() /*A*/; second()`.
		leftEdit = statement.Pos()
	case leftSyntax.IsValid():
		// Never so from this pass: a dead statement always follows a sibling in its list, between the parent's
		// opening and itself, so when that opening shares its first line, the sibling's end does too, and
		// leftStatement holds it. Kept as the original's.
		if leftmost, _, found := filterPositions(leftComments, leftSyntax, statement.Pos()); found {
			leftEdit = leftmost
		} else {
			leftEdit = statement.Pos()
		}
	default:
		// The whole of the first line, from its start.
		leftEdit = statement.Pos()
		for file.Line(leftEdit) == statementStartLine {
			leftEdit--
		}
		if leftEdit < statement.Pos() {
			leftEdit++
		}
	}
	switch {
	case rightStatement.IsValid():
		// A statement after on the line keeps the comments between.
		rightEdit = statement.End()
	case rightSyntax.IsValid():
		// The comments between the statement and the parent's closing go with it.
		if _, rightmost, found := filterPositions(rightComments, statement.End(), rightSyntax); found {
			rightEdit = rightmost
		} else {
			rightEdit = statement.End()
		}
	default:
		// The whole of the last line, its newline included, stopping at the file's end.
		fileEnd := token.Pos(file.Base()) + token.Pos(file.Size())
		rightEdit = statement.End()
		for fileEnd >= rightEdit && file.Line(rightEdit) == statementEndLine {
			rightEdit++
		}
		// The newline stays when something else is left before the statement on its line.
		if leftSyntax.IsValid() || leftStatement.IsValid() {
			rightEdit--
		}
	}

	return []analysis.TextEdit{{Pos: leftEdit, End: rightEdit}}
}

// commentsBeside is the comments of enclosingFile, in file, on statement's lines: those on its first line that end
// before it, and those on its last line that start after it. A comment that starts on an earlier line and ends on
// its first counts as before it. The file's comments are in order, so the first that starts after its last line ends
// the search.
func commentsBeside(
	file *token.File, enclosingFile *ast.File, statement ast.Stmt,
) (leftComments []*ast.Comment, rightComments []*ast.Comment) {
	statementStartLine := file.Line(statement.Pos())
	statementEndLine := file.Line(statement.End())
	for _, group := range enclosingFile.Comments {
		for _, comment := range group.List {
			if file.Line(comment.End()) < statementStartLine {
				continue
			}
			if file.Line(comment.Pos()) > statementEndLine {
				return leftComments, rightComments
			}
			if file.Line(comment.End()) == statementStartLine && comment.End() <= statement.Pos() {
				leftComments = append(leftComments, comment)
			} else if file.Line(comment.Pos()) == statementEndLine && comment.Pos() >= statement.End() {
				rightComments = append(rightComments, comment)
			}
		}
	}
	return leftComments, rightComments
}

// filterPositions is the start of the leftmost of comments that starts after start and before end, the end of the
// rightmost that ends at or before end, and whether any comment was either. x/tools' refactor.filterPos.
func filterPositions(comments []*ast.Comment, start token.Pos, end token.Pos) (token.Pos, token.Pos, bool) {
	leftmost := end
	rightmost := token.NoPos
	found := false
	for _, comment := range comments {
		if comment.Pos() > start && comment.Pos() < leftmost {
			leftmost = comment.Pos()
			found = true
		}
		if comment.End() <= end && comment.End() > rightmost {
			rightmost = comment.End()
			found = true
		}
	}
	return leftmost, rightmost, found
}

// enclosingFileOf is the file whose tree holds the node at cursor. x/tools' astutil.EnclosingFile.
func enclosingFileOf(cursor inspector.Cursor) *ast.File {
	for enclosing := range cursor.Enclosing((*ast.File)(nil)) {
		if file, isFile := enclosing.Node().(*ast.File); isFile {
			return file
		}
	}
	// Invariant: an inspector is built from files, so every node it holds is in one.
	panic("unreachable: a statement is in no file")
}

// enclosingBlockRightBrace is the closing brace of the switch's or select's body around the clause at clauseCursor.
func enclosingBlockRightBrace(clauseCursor inspector.Cursor) token.Pos {
	body, isBlock := clauseCursor.Parent().Node().(*ast.BlockStmt)
	if !isBlock {
		// Invariant: the parser puts a case or communication clause only in a switch's or a select's body. The
		// original asserts the same.
		panic(fmt.Sprintf("unreachable: a clause's parent is a %T, not a body", clauseCursor.Parent().Node()))
	}
	return body.Rbrace
}
