package assign

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
)

// deleteStatement is the edits that remove statement, the node at cursor, or nil when its parent is not one whose
// statements it knows how to remove (a label, the `else` of an `if`, whose keyword the tree holds no position for, or
// the communication of a select's case). A statement alone on its lines goes with those lines and the comments on
// them; one that shares a line with other syntax goes with only the comments between it and that syntax, and with
// none beside a neighbouring statement, since the tree records no semicolon to say which of the two a comment there
// belongs to. x/tools' refactor.DeleteStmt.
func deleteStatement(file *token.File, cursor inspector.Cursor, statement ast.Stmt) []analysis.TextEdit {
	startLine := file.Line(statement.Pos())
	endLine := file.Line(statement.End())
	// leftSyntax and rightSyntax are the parent's tokens on the statement's first and last lines that bound it, and
	// invalid when the parent has none there.
	leftSyntax, rightSyntax := token.NoPos, token.NoPos
	keepSyntaxOnStatementLines := func(left token.Pos, right token.Pos) {
		if file.Line(left) == startLine {
			leftSyntax = left
		}
		if file.Line(right) == endLine {
			rightSyntax = right
		}
	}

	leftComments, rightComments := commentsBeside(enclosingFile(cursor), file, statement)

	// leftStatement and rightStatement are where a neighbouring statement on the same line ends before the
	// statement and begins after it, and invalid when there is none.
	leftStatement, rightStatement := token.NoPos, token.NoPos
	inStatementList := false
	switch parent := cursor.Parent().Node().(type) {
	case *ast.BlockStmt:
		keepSyntaxOnStatementLines(parent.Lbrace, parent.Rbrace)
		inStatementList = true
	case *ast.CaseClause:
		keepSyntaxOnStatementLines(parent.Colon, enclosingBlock(cursor.Parent()).Rbrace)
		inStatementList = true
	case *ast.CommClause:
		// Removing the communication would leave a case with none, and the user more likely meant the whole case.
		// Kept for fidelity, it cannot fire for assign: a communication assigns what a receive yields, which has an
		// effect, so it is never a self-assignment.
		if parent.Comm == statement {
			return nil
		}
		keepSyntaxOnStatementLines(parent.Colon, enclosingBlock(cursor.Parent()).Rbrace)
		inStatementList = true
	case *ast.ForStmt:
		keepSyntaxOnStatementLines(parent.For, parent.Body.Lbrace)
		// The init, condition and post of a loop are no statement list, so a neighbour on the line is found by hand.
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
			keepSyntaxOnStatementLines(parent.If, parent.Body.Lbrace)
		case parent.Else:
			// Removing the else block would mean removing its `else`, whose position the tree does not hold. Kept for
			// fidelity, it cannot fire for assign: an else is a block or an if, never an assignment.
			return nil
		}
	case *ast.SwitchStmt:
		keepSyntaxOnStatementLines(parent.Switch, parent.Body.Lbrace)
	case *ast.TypeSwitchStmt:
		// The assignment of a type switch holds its `.(type)`, which cannot stand alone. Kept for fidelity, it cannot
		// fire for assign: that assignment's left side is a name and its right a type assertion, never the same.
		if statement == parent.Assign {
			return nil
		}
		keepSyntaxOnStatementLines(parent.Switch, parent.Body.Lbrace)
	default:
		return nil
	}

	if inStatementList {
		if previous, found := cursor.PrevSibling(); found && file.Line(previous.Node().End()) == startLine {
			if _, isStatement := previous.Node().(ast.Stmt); isStatement {
				leftStatement = previous.Node().End()
			}
		}
		if next, found := cursor.NextSibling(); found && file.Line(next.Node().Pos()) == endLine {
			rightStatement = next.Node().Pos()
		}
	}

	// leftEdit and rightEdit bound the edit.
	leftEdit, rightEdit := token.NoPos, token.NoPos
	switch {
	case leftStatement.IsValid():
		// In `previous() /* comment */; statement`, the comment may be the previous statement's, so it stays.
		leftEdit = statement.Pos()
	case leftSyntax.IsValid():
		if leftmost, _, found := commentBounds(leftComments, leftSyntax, statement.Pos()); found {
			leftEdit = leftmost
		} else {
			leftEdit = statement.Pos()
		}
	default:
		// The statement starts its line, so the edit starts at the line's start.
		for leftEdit = statement.Pos(); file.Line(leftEdit) == startLine; leftEdit-- {
		}
		if leftEdit < statement.Pos() {
			leftEdit++
		}
	}
	switch {
	case rightStatement.IsValid():
		// As on the left, a comment before a following statement may be that statement's.
		rightEdit = statement.End()
	case rightSyntax.IsValid():
		if _, rightmost, found := commentBounds(rightComments, statement.End(), rightSyntax); found {
			rightEdit = rightmost
		} else {
			rightEdit = statement.End()
		}
	default:
		// The statement ends its line, so the edit runs through the line's end, and its newline unless something
		// before the statement keeps the line.
		fileEnd := token.Pos(file.Base()) + token.Pos(file.Size())
		for rightEdit = statement.End(); fileEnd >= rightEdit && file.Line(rightEdit) == endLine; rightEdit++ {
		}
		if leftSyntax.IsValid() || leftStatement.IsValid() {
			rightEdit--
		}
	}
	return []analysis.TextEdit{{Pos: leftEdit, End: rightEdit}}
}

// commentsBeside is the comments of syntaxFile before statement on its first line and after it on its last.
func commentsBeside(
	syntaxFile *ast.File, file *token.File, statement ast.Stmt,
) (leftComments []*ast.Comment, rightComments []*ast.Comment) {
	startLine := file.Line(statement.Pos())
	endLine := file.Line(statement.End())
	leftComments, rightComments = []*ast.Comment{}, []*ast.Comment{}
	for _, group := range syntaxFile.Comments {
		for _, comment := range group.List {
			if file.Line(comment.End()) < startLine {
				continue
			}
			if file.Line(comment.Pos()) > endLine {
				// The comments are in source order, so none after this one is on the statement's lines.
				return leftComments, rightComments
			}
			if file.Line(comment.End()) == startLine && comment.End() <= statement.Pos() {
				leftComments = append(leftComments, comment)
			} else if file.Line(comment.Pos()) == endLine && comment.Pos() >= statement.End() {
				rightComments = append(rightComments, comment)
			}
		}
	}
	return leftComments, rightComments
}

// commentBounds is the leftmost start after start and the rightmost end at or before end among comments, and whether
// any comment set either. x/tools' refactor.filterPos.
func commentBounds(comments []*ast.Comment, start token.Pos, end token.Pos) (token.Pos, token.Pos, bool) {
	leftmost, rightmost := end, token.NoPos
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

// enclosingFile is the file holding the node at cursor. x/tools' astutil.EnclosingFile.
func enclosingFile(cursor inspector.Cursor) *ast.File {
	for fileCursor := range cursor.Enclosing((*ast.File)(nil)) {
		if file, isFile := fileCursor.Node().(*ast.File); isFile {
			return file
		}
	}
	// Invariant: the inspector is built from a package's files, so every node it holds is within one.
	panic("assign: a statement is outside every file")
}

// enclosingBlock is the block holding clause, a switch's or select's case, whose closing brace bounds the clause's
// last statement.
func enclosingBlock(clause inspector.Cursor) *ast.BlockStmt {
	block, isBlock := clause.Parent().Node().(*ast.BlockStmt)
	if !isBlock {
		// Invariant: the parser puts every case of a switch or select in the statement's body, a block.
		panic("assign: a case is outside its statement's body")
	}
	return block
}
