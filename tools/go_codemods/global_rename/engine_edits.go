package global_rename

import (
	"go/ast"
	"sort"

	"github.com/system-inc/cohere/tools/go_codemods/rename"
)

// collectEdits lists every identifier of every accepted rename, across all root packages and variants, once
// per span, and the comment edits that respell an old name in its own declaration's comments.
func (engine *engine) collectEdits() {
	var edits []rename.Edit
	for key, newName := range engine.accepted {
		oldName := engine.candidates[key].OldName
		for _, site := range engine.sites[key] {
			edits = append(
				edits,
				rename.Edit{Filename: site.filename, Offset: site.offset, OldText: oldName, NewText: newName},
			)
		}
	}
	kept, disagreements := rename.Deduplicate(edits)
	engine.result.Edits = kept
	engine.result.Notes = append(engine.result.Notes, disagreements...)
	var twinEdits []rename.Edit
	var tagEdits []rename.Edit
	for key := range engine.accepted {
		twinEdits = append(twinEdits, engine.twinEdits[key]...)
		tagEdits = append(tagEdits, engine.tagEdits[key]...)
	}
	engine.result.TagEdits = tagEdits
	engine.result.TwinEdits, _ = rename.Deduplicate(twinEdits)
	engine.result.CommentEdits = engine.commentEdits()
	sort.Slice(engine.result.TagEdits, func(left, right int) bool {
		if engine.result.TagEdits[left].Filename != engine.result.TagEdits[right].Filename {
			return engine.result.TagEdits[left].Filename < engine.result.TagEdits[right].Filename
		}
		return engine.result.TagEdits[left].Offset < engine.result.TagEdits[right].Offset
	})
}

// commentEdits respells each accepted non-local rename's old name, as a whole word, in the doc and line
// comments of its own declaration, so `// GroupID is` reads `// GroupId is` after the rename.
func (engine *engine) commentEdits() []rename.Edit {
	var edits []rename.Edit
	seenFiles := map[string]bool{}
	for _, rootsPackage := range engine.roots {
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			if seenFiles[filename] || !engine.editableFiles[filename] {
				continue
			}
			seenFiles[filename] = true
			respell := func(names []*ast.Ident, groups ...*ast.CommentGroup) {
				for _, identifier := range names {
					object := rootsPackage.TypesInfo.Defs[identifier]
					if object == nil {
						continue
					}
					key := engine.keyer.key(object)
					newName, renamed := engine.accepted[key]
					if !renamed || engine.candidates[key].local {
						continue
					}
					for _, group := range groups {
						if group == nil {
							continue
						}
						for _, comment := range group.List {
							offset := rootsPackage.Fset.PositionFor(comment.Pos(), false).Offset
							for _, index := range wholeWordIndexes(comment.Text, identifier.Name) {
								edits = append(edits, rename.Edit{
									Filename: filename,
									Offset:   offset + index,
									OldText:  identifier.Name,
									NewText:  newName,
								})
							}
						}
					}
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.FuncDecl:
					respell([]*ast.Ident{typed.Name}, typed.Doc)
				case *ast.GenDecl:
					if len(typed.Specs) != 1 {
						return true
					}
					switch spec := typed.Specs[0].(type) {
					case *ast.TypeSpec:
						respell([]*ast.Ident{spec.Name}, typed.Doc)
					case *ast.ValueSpec:
						respell(spec.Names, typed.Doc)
					}
				case *ast.TypeSpec:
					respell([]*ast.Ident{typed.Name}, typed.Doc, typed.Comment)
				case *ast.ValueSpec:
					respell(typed.Names, typed.Doc, typed.Comment)
				case *ast.Field:
					respell(typed.Names, typed.Doc, typed.Comment)
				}
				return true
			})
		}
	}
	kept, _ := rename.Deduplicate(edits)
	return kept
}

// wholeWordIndexes lists where word occurs in text bounded by characters no identifier holds.
func wholeWordIndexes(text string, word string) []int {
	var indexes []int
	isIdentifierByte := func(character byte) bool {
		return character == '_' || (character >= '0' && character <= '9') || (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') || character >= 0x80
	}
	for start := 0; start+len(word) <= len(text); start++ {
		if text[start:start+len(word)] != word {
			continue
		}
		if start > 0 && isIdentifierByte(text[start-1]) {
			continue
		}
		end := start + len(word)
		if end < len(text) && isIdentifierByte(text[end]) {
			continue
		}
		indexes = append(indexes, start)
	}
	return indexes
}
