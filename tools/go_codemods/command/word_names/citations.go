package main

// A comment that cites a renamed name by its old spelling. The engine respells the old name in its own
// declaration's comments; a comment elsewhere that cites it, `the paired fixtures in TestBlockScopedVarIgnores...`,
// would point at a name that no longer exists. The pass respells such a citation where the old name can mean only
// the renamed declaration: it is written in camel case, as no English word is, nothing left in the workspace is
// declared by it, and every declaration that had it takes the same new name. An external name that is itself the
// outside name keeps its old spelling in every comment, since there it cites the outside name, and so does an old
// name a string literal spells: the literal is a name outside Go, React's "propTypes", JSON Schema's "minItems",
// upstream's "srcToken", and a comment spelling it may cite that.

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"

	"github.com/system-inc/cohere/tools/go_codemods/global_rename"
	"github.com/system-inc/cohere/tools/go_codemods/rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"
)

// citationEdits respells every comment citation of an accepted rename's old name, in the editable files of the
// root packages, and returns how many it found. The edits join the result's comment edits.
// Mutates: result the citation edits join the result's comment edits, which the write reads
func citationEdits(
	loaded *workspace.Workspace,
	result *global_rename.Result,
	externals map[string]plannedExternal,
) int {
	renamedAt := map[token.Position]bool{}
	newNames := map[string]string{}
	ambiguous := map[string]bool{}
	for _, renamed := range result.Accepted {
		position := renamed.Position
		position.Column, position.Line = 0, 0
		renamedAt[position] = true
		if renamed.Kind == global_rename.KindLocal || !isCamelCase(renamed.OldName) {
			continue
		}
		if existing, seen := newNames[renamed.OldName]; seen && existing != renamed.NewName {
			ambiguous[renamed.OldName] = true
		}
		newNames[renamed.OldName] = renamed.NewName
	}
	// An old name that is itself the outside name, charCodeAt, cites the outside name wherever a comment spells it.
	for _, planned := range externals {
		if planned.spelling == planned.oldName {
			ambiguous[planned.oldName] = true
		}
	}
	// A name some declaration keeps is still a name a comment may mean.
	for _, rootsPackage := range loaded.Roots {
		for identifier, object := range rootsPackage.TypesInfo.Defs {
			if object == nil || newNames[identifier.Name] == "" {
				continue
			}
			position := rootsPackage.Fset.PositionFor(identifier.Pos(), false)
			position.Column, position.Line = 0, 0
			if !renamedAt[position] {
				ambiguous[identifier.Name] = true
			}
		}
	}
	for name := range literalNames(loaded) {
		if newNames[name] != "" {
			ambiguous[name] = true
		}
	}
	for name := range ambiguous {
		delete(newNames, name)
	}
	var edits []rename.Edit
	seenFiles := map[string]bool{}
	for _, rootsPackage := range loaded.Roots {
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			if seenFiles[filename] || !loaded.Editable(rootsPackage, file) {
				continue
			}
			seenFiles[filename] = true
			for _, group := range file.Comments {
				for _, comment := range group.List {
					start := rootsPackage.Fset.PositionFor(comment.Pos(), false).Offset
					for _, word := range identifierWords(comment.Text) {
						if newName := newNames[word.text]; newName != "" {
							edits = append(edits, rename.Edit{
								Filename: filename,
								Offset:   start + word.offset,
								OldText:  word.text,
								NewText:  newName,
							})
						}
					}
				}
			}
		}
	}
	all, _ := rename.Deduplicate(append(append([]rename.Edit(nil), result.CommentEdits...), edits...))
	sort.Slice(all, func(left, right int) bool {
		if all[left].Filename != all[right].Filename {
			return all[left].Filename < all[right].Filename
		}
		return all[left].Offset < all[right].Offset
	})
	added := len(all) - len(result.CommentEdits)
	result.CommentEdits = all
	return added
}

// literalNames is every identifier a string literal of the root packages spells whole, a struct tag aside.
func literalNames(loaded *workspace.Workspace) map[string]bool {
	names := map[string]bool{}
	for _, rootsPackage := range loaded.Roots {
		for _, file := range rootsPackage.Syntax {
			tags := map[*ast.BasicLit]bool{}
			ast.Inspect(file, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.Field:
					if typed.Tag != nil {
						tags[typed.Tag] = true
					}
				case *ast.BasicLit:
					if typed.Kind != token.STRING || tags[typed] {
						return true
					}
					if value, unquoteError := strconv.Unquote(
						typed.Value,
					); unquoteError == nil && token.IsIdentifier(value) {
						names[value] = true
					}
				}
				return true
			})
		}
	}
	return names
}

// isCamelCase is whether a name holds a capital after its first letter, as an identifier does and a word does not.
func isCamelCase(name string) bool {
	for index := 1; index < len(name); index++ {
		if isUppercaseLetter(name[index]) {
			return true
		}
	}
	return false
}

// identifierWord is one run of identifier characters in a comment, and its offset there.
type identifierWord struct {
	text   string
	offset int
}

// identifierWords splits text into its runs of identifier characters.
func identifierWords(text string) []identifierWord {
	var words []identifierWord
	start := -1
	for index := 0; index <= len(text); index++ {
		if index < len(text) && isIdentifierByte(text[index]) {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 {
			words = append(words, identifierWord{text: text[start:index], offset: start})
			start = -1
		}
	}
	return words
}
