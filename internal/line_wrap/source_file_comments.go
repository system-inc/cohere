package line_wrap

import (
	"go/ast"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
)

// proseComment reports whether a comment-only line is a prose line a reflow may touch: `// ` then a word, not
// a directive, not a doc-comment code block (`//\t` or `//` with extra spaces), not a list item, not a heading.
func proseComment(comment string) bool {
	if !strings.HasPrefix(comment, "// ") || len(comment) < 4 {
		return false
	}
	if comment[3] == ' ' || comment[3] == '\t' || isDirective(comment) {
		return false
	}
	fields := strings.Fields(comment[3:])
	return len(fields) > 0 && !listMarker(fields[0]) && !strings.HasPrefix(fields[0], "#")
}

// listMarker reports whether a word starting a comment line would read as a list item marker.
func listMarker(word string) bool {
	if word == "-" || word == "*" || word == "+" || word == "•" {
		return true
	}
	if len(word) < 2 || (word[len(word)-1] != '.' && word[len(word)-1] != ')') {
		return false
	}
	for _, character := range word[:len(word)-1] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// commentEdit reflows the paragraph from a long comment line onward: words that do not fit move to the start
// of the next line of the paragraph, cascading until a line absorbs them, or onto new lines at its end.
func (sourceFile *sourceFile) commentEdit(line int) (edit, bool) {
	comment, isComment := sourceFile.commentOnlyLine(line)
	if !isComment || !proseComment(comment.text) {
		return edit{}, false
	}
	text := sourceFile.lineText(line)
	indent := indentation(text)
	paragraph := []string{comment.text}
	for next := line + 1; next <= sourceFile.lineCount(); next++ {
		nextComment, nextIsComment := sourceFile.commentOnlyLine(next)
		if !nextIsComment || indentation(sourceFile.lineText(next)) != indent || !proseComment(nextComment.text) {
			break
		}
		paragraph = append(paragraph, nextComment.text)
	}
	room := consistency_no_long_line.Limit - width(indent) - len("// ")
	var output []string
	var carry []string
	consumed := 0
	for index, original := range paragraph {
		if index > 0 && len(carry) == 0 {
			break
		}
		consumed = index + 1
		words := append(carry, strings.Fields(original[3:])...)
		filled := fill(words, room)
		output = append(output, filled[0])
		carry = nil
		if len(filled) > 1 {
			carry = strings.Fields(strings.Join(filled[1:], " "))
		}
	}
	if len(carry) > 0 {
		output = append(output, fill(carry, room)...)
	}
	if len(output) == 1 && output[0] == strings.Join(strings.Fields(comment.text[3:]), " ") {
		return edit{}, false
	}
	_, end := sourceFile.lineBounds(line + consumed - 1)
	lines := make([]string, len(output))
	for index, filled := range output {
		lines[index] = "// " + filled
	}
	return edit{start: comment.start, end: end, text: strings.Join(lines, "\n"+indent)}, true
}

// fill packs words greedily into lines of at most room columns. A word wider than room gets a line of its
// own. A line never starts with a word that would read as a list marker: that word moves with its neighbour.
func fill(words []string, room int) []string {
	var lines [][]string
	current := []string{}
	columns := 0
	for _, word := range words {
		wordWidth := width(word)
		if len(current) > 0 && columns+1+wordWidth > room {
			if listMarker(word) && len(current) > 1 {
				moved := current[len(current)-1]
				lines = append(lines, current[:len(current)-1])
				current = []string{moved}
				columns = width(moved)
			} else {
				lines = append(lines, current)
				current = []string{}
				columns = 0
			}
		}
		if len(current) > 0 {
			columns++
		}
		current = append(current, word)
		columns += wordWidth
	}
	lines = append(lines, current)
	result := make([]string, len(lines))
	for index, line := range lines {
		result[index] = strings.Join(line, " ")
	}
	return result
}

// residueShape names what kept a long line long after wrapping.
func (sourceFile *sourceFile) residueShape(line int) string {
	if comment, isComment := sourceFile.commentOnlyLine(line); isComment {
		switch {
		case strings.HasPrefix(comment.text, "/*"):
			return "BlockComment"
		case strings.HasPrefix(comment.text, "//\t") || strings.HasPrefix(comment.text, "//  "):
			return "CommentCodeBlock"
		case strings.HasPrefix(comment.text, "// ") &&
			len(strings.Fields(comment.text[3:])) > 0 &&
			listMarker(strings.Fields(comment.text[3:])[0]):
			return "CommentListItem"
		case proseComment(comment.text):
			return "CommentLongWord"
		}
		return "CommentOther"
	}
	lineStart, _ := sourceFile.lineBounds(line)
	cross := lineStart + crossing(sourceFile.lineText(line))
	if _, inComment := sourceFile.commentAt(cross); inComment {
		return "TrailingComment"
	}
	literal, inLiteral := sourceFile.literalAt(cross)
	if !inLiteral {
		return sourceFile.codeShape(cross)
	}
	kind := "String"
	if strings.HasPrefix(literal.Value, "`") {
		kind = "RawString"
	}
	literalStart := sourceFile.offset(literal.Pos())
	if strings.TrimSpace(string(sourceFile.source[lineStart:literalStart])) == "" {
		return kind + "ThenCode"
	}
	if keyed := sourceFile.codeShape(literalStart); keyed == "KeyValue" {
		return kind + "AfterKey"
	}
	return kind + "AfterCode"
}

// codeShape names the construct around an offset that a person would break to shorten the line: a case,
// assignment or declaration list of several items when there is one, else the innermost expression.
func (sourceFile *sourceFile) codeShape(offset int) string {
	shape := "Code"
	ast.Inspect(sourceFile.file, func(node ast.Node) bool {
		if node == nil || sourceFile.offset(node.Pos()) > offset || sourceFile.offset(node.End()) <= offset {
			return node == nil
		}
		switch typed := node.(type) {
		case *ast.CaseClause:
			if len(typed.List) > 1 {
				shape = "CaseList"
				return false
			}
		case *ast.AssignStmt:
			if len(typed.Lhs) > 1 || len(typed.Rhs) > 1 {
				shape = "AssignmentList"
				return false
			}
		case *ast.ValueSpec:
			if len(typed.Names) > 1 || len(typed.Values) > 1 {
				shape = "ValueSpecList"
				return false
			}
		case *ast.FuncType:
			shape = "SignatureWithoutList"
		case *ast.IndexExpr, *ast.SliceExpr, *ast.IndexListExpr:
			shape = "IndexOrSlice"
		case *ast.SelectorExpr:
			shape = "SelectorChain"
		case *ast.BinaryExpr:
			shape = "Operator" + typed.Op.String()
		case *ast.KeyValueExpr:
			shape = "KeyValue"
		case *ast.CallExpr:
			shape = "CallAlreadyBroken"
		case *ast.CompositeLit:
			shape = "CompositeAlreadyBroken"
		}
		return true
	})
	return shape
}
