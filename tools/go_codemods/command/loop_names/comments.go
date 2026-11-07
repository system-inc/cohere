package main

import (
	"go/ast"
	"go/types"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/internal/go_rename/rename"
)

// docCommentEdits respells a renamed receiver, parameter or named result where its function's doc comment
// names it, so the comment keeps naming what the code calls it: a bare mention of the letter becomes the new name
// between backticks, which reads as the identifier it is (`character`) where the bare word would read as prose,
// and plain where brackets or parentheses already make it code (`pattern[index]`).
// A letter inside a code span is left: there it is code the comment quotes, an upstream expression or a regular
// expression's flag, not the parameter. A Mutates line is rename.Edits' own and respelled there.
func docCommentEdits(loadedPackage rename.Package, accepted map[types.Object]string) []rename.Edit {
	var edits []rename.Edit
	for _, file := range loadedPackage.Files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Doc == nil {
				continue
			}
			renamed := map[string]string{}
			for _, fields := range []*ast.FieldList{function.Recv, function.Type.Params, function.Type.Results} {
				if fields == nil {
					continue
				}
				for _, field := range fields.List {
					for _, name := range field.Names {
						if newName, isRenamed := accepted[loadedPackage.Info.Defs[name]]; isRenamed &&
							len(name.Name) == 1 {
							renamed[name.Name] = newName
						}
					}
				}
			}
			if len(renamed) == 0 {
				continue
			}
			for _, comment := range function.Doc.List {
				if strings.HasPrefix(strings.TrimLeft(comment.Text, "/ \t"), "Mutates:") {
					continue
				}
				commentPosition := loadedPackage.FileSet.PositionFor(comment.Pos(), false)
				for _, offset := range letterMentions(comment.Text, renamed) {
					letter := comment.Text[offset : offset+1]
					newText := "`" + renamed[letter] + "`"
					if isBetweenCodePunctuation(comment.Text, offset) {
						newText = renamed[letter]
					}
					edits = append(edits, rename.Edit{
						Filename: commentPosition.Filename,
						Offset:   commentPosition.Offset + offset,
						OldText:  letter,
						NewText:  newText,
					})
				}
			}
		}
	}
	return edits
}

// letterMentions finds every offset in one comment line where a renamed letter stands as a bare word naming the
// parameter: outside a code span, never `a`, which prose spells as an article, and never a letter followed by a
// period and another letter (`e.g.`, `i.e.`), a hyphen (`n-th`), or an apostrophe other than a possessive's.
func letterMentions(text string, renamed map[string]string) []int {
	isWordByte := func(character byte) bool {
		return character == '_' || unicode.IsLetter(rune(character)) || unicode.IsDigit(rune(character))
	}
	var offsets []int
	inCodeSpan := false
	for offset := 0; offset < len(text); offset++ {
		if text[offset] == '`' {
			inCodeSpan = !inCodeSpan
			continue
		}
		letter := text[offset : offset+1]
		if _, isRenamed := renamed[letter]; !isRenamed || inCodeSpan || letter == "a" {
			continue
		}
		if offset > 0 && (isWordByte(text[offset-1]) || strings.ContainsRune(".'-", rune(text[offset-1]))) {
			continue
		}
		rest := text[offset+1:]
		if len(rest) > 0 && isWordByte(rest[0]) {
			continue
		}
		if strings.HasPrefix(rest, "-") || (strings.HasPrefix(rest, "'") && !strings.HasPrefix(rest, "'s")) ||
			(strings.HasPrefix(rest, ".") && len(rest) > 1 && isWordByte(rest[1])) {
			continue
		}
		offsets = append(offsets, offset)
	}
	return offsets
}

// isBetweenCodePunctuation reports whether the letter at offset sits against an opening bracket or parenthesis
// before it or a closing one after it, `pattern[i]` or `f(n)`, where the comment already writes it as code.
func isBetweenCodePunctuation(text string, offset int) bool {
	return (offset > 0 && strings.ContainsRune("[(", rune(text[offset-1]))) ||
		(offset+1 < len(text) && strings.ContainsRune("])", rune(text[offset+1])))
}
