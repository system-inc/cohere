package main

// A comment line a respelling pushes past the house's 120 columns. The line wrap after a pass settles code, not
// prose, so a respelled citation that lengthens its line would leave a long-line finding the pass created. The
// pass reflows such a paragraph itself: from the line the respelling made too long to the paragraph's end, the
// words are filled back to the limit, and every line before it stays as it was. A paragraph is a run of plain
// `// ` lines; a blank `//`, an indented code line, a list item, a heading or a directive ends it.

import (
	"go/ast"
	"go/token"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// maximumColumns is the house's line limit, a tab counted as tabColumns.
const (
	maximumColumns = 120
	tabColumns     = 4
)

// reflowLongComments replaces the comment edits of every paragraph whose respelled line runs past the limit with
// one edit that reflows the paragraph from that line on. It returns the number of paragraphs reflowed and of
// lines left long, a single word wider than the limit.
// Mutates: result the reflowed paragraphs replace their edits in the result's comment edits, which the write reads
func reflowLongComments(loaded *workspace.Workspace, result *global_rename.Result) (int, int) {
	editsByFile := rename.GroupByFile(result.CommentEdits)
	type parsed struct {
		file    *ast.File
		fileSet *token.FileSet
	}
	files := map[string]parsed{}
	for _, rootsPackage := range loaded.Roots {
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			if _, edited := editsByFile[filename]; edited {
				files[filename] = parsed{file: file, fileSet: rootsPackage.Fset}
			}
		}
	}
	reflowed, leftLong := 0, 0
	var kept []rename.Edit
	for _, filename := range sortedKeys(editsByFile) {
		fileEdits := editsByFile[filename]
		found, known := files[filename]
		source, readError := os.ReadFile(filename)
		if !known || readError != nil {
			kept = append(kept, fileEdits...)
			continue
		}
		replaced := map[rename.EditKey]bool{}
		var reflows []rename.Edit
		for _, group := range found.file.Comments {
			offset := func(position token.Pos) int { return found.fileSet.PositionFor(position, false).Offset }
			lines := commentLines(group, offset, source, fileEdits)
			for index := 0; index < len(lines); index++ {
				if !lines[index].edited || lines[index].width() <= maximumColumns {
					continue
				}
				// A code line, a list item, or a comment after code on its line is not prose to refill.
				if !lines[index].inParagraph() || strings.TrimLeft(lines[index].indentation, " \t") != "" {
					leftLong++
					continue
				}
				end := index
				for end+1 < len(lines) && lines[end+1].inParagraph() {
					end++
				}
				newText, fits := fill(lines[index : end+1])
				if !fits {
					leftLong++
				}
				span := source[lines[index].start:lines[end].end]
				reflows = append(reflows, rename.Edit{
					Filename: filename,
					Offset:   lines[index].start,
					OldText:  string(span),
					NewText:  newText,
				})
				for _, line := range lines[index : end+1] {
					for _, edit := range line.edits {
						replaced[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}] = true
					}
				}
				reflowed++
				index = end
			}
		}
		for _, edit := range fileEdits {
			if !replaced[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}] {
				kept = append(kept, edit)
			}
		}
		kept = append(kept, reflows...)
	}
	sort.Slice(kept, func(left, right int) bool {
		if kept[left].Filename != kept[right].Filename {
			return kept[left].Filename < kept[right].Filename
		}
		return kept[left].Offset < kept[right].Offset
	})
	result.CommentEdits = kept
	return reflowed, leftLong
}

// commentLines reads a group's `//` comments with fileEdits applied to each.
func commentLines(
	group *ast.CommentGroup,
	offset func(token.Pos) int,
	source []byte,
	fileEdits []rename.Edit,
) []commentLine {
	var lines []commentLine
	for _, comment := range group.List {
		if !strings.HasPrefix(comment.Text, "//") {
			return nil
		}
		line := commentLine{start: offset(comment.Pos()), end: offset(comment.End())}
		line.indentation = string(source[lineStart(source, line.start):line.start])
		var onLine []rename.Edit
		for _, edit := range fileEdits {
			if edit.Offset >= line.start && edit.Offset < line.end {
				onLine = append(onLine, edit)
			}
		}
		sort.Slice(onLine, func(left, right int) bool { return onLine[left].Offset > onLine[right].Offset })
		text := string(source[line.start:line.end])
		for _, edit := range onLine {
			relative := edit.Offset - line.start
			text = text[:relative] + edit.NewText + text[relative+len(edit.OldText):]
		}
		line.text, line.edits, line.edited = text, onLine, len(onLine) > 0
		lines = append(lines, line)
	}
	return lines
}

// fill flows the lines' words into lines no wider than the limit, at the first line's indentation, and reports
// whether every line fits.
func fill(lines []commentLine) (string, bool) {
	var words []string
	for _, line := range lines {
		words = append(words, strings.Fields(strings.TrimPrefix(line.text, "//"))...)
	}
	indentation := lines[0].indentation
	prefix := columns(indentation) + len("// ")
	var built []string
	current := ""
	fits := true
	for _, word := range words {
		if current != "" && prefix+utf8.RuneCountInString(current)+1+utf8.RuneCountInString(word) > maximumColumns {
			built = append(built, current)
			current = ""
		}
		if current == "" {
			current = word
		} else {
			current += " " + word
		}
		if prefix+utf8.RuneCountInString(current) > maximumColumns {
			fits = false
		}
	}
	built = append(built, current)
	return "// " + strings.Join(built, "\n"+indentation+"// "), fits
}

// columns is the width of indentation, a tab counted as four.
func columns(indentation string) int {
	width := 0
	for _, character := range indentation {
		if character == '\t' {
			width += tabColumns
		} else {
			width++
		}
	}
	return width
}
