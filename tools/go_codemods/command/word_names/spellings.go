package main

// An external name's doc comment, under -rename-owned-spellings. The engine respells a renamed name's old
// spelling wherever it stands as a word in the declaration's own comments, which is right for a name that is only
// ours and wrong for one that cites an outside name: `charCodeAt is css.charCodeAt(index)` would come out citing a
// css.characterCodeAt no JavaScript has. So for an external name only the doc's opening name, its subject, takes the
// new name; every other occurrence keeps the outside spelling, and a declaration whose own doc comment names the
// spelling nowhere gains its note from external.go.

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"sort"
	"strings"

	"github.com/system-inc/cohere/tools/go_codemods/global_rename"
	"github.com/system-inc/cohere/tools/go_codemods/rename"
)

// plannedExternal is what one proposed external name's comments become if the engine accepts the rename.
type plannedExternal struct {
	oldName string
	newName string
	// spelling is the outside name the declaration cites.
	spelling string
	filename string
	// commentRanges are the byte ranges of the declaration's own doc and line comments, in which the engine's
	// respellings are dropped.
	commentRanges [][2]int
	// subject is the respelling of the doc's opening name, or nil when the doc does not open with it.
	subject *rename.Edit
	// note inserts the note, or is nil when a doc comment of the declaration already names the spelling.
	note *rename.Edit
	// noteIsDoc is whether the note is the declaration's whole doc, which it had none of.
	noteIsDoc bool
	// refusal says why the name cannot be renamed under the rule, or is "".
	refusal string
}

// externalRespelling is what became of one accepted external name's doc comment, for the report.
type externalRespelling struct {
	position token.Position
	oldName  string
	newName  string
	outcome  string
}

// Outcomes of an external name's doc comment.
const (
	outcomeSpellingNamed = "the doc already names the outside spelling, left as it is"
	outcomeNoteAdded     = "the doc gains its note"
	outcomeNoteAsDoc     = "the declaration gains its note as its doc"
)

// planExternal reads the declaration an external name's identifier declares and plans its comments: which
// ranges the engine may not respell, the subject's respelling, and the note, when no doc names the spelling.
func planExternal(
	fileSet *token.FileSet,
	file *ast.File,
	identifier *ast.Ident,
	external externalName,
	newName string,
) plannedExternal {
	position := fileSet.PositionFor(identifier.Pos(), false)
	planned := plannedExternal{
		oldName:  identifier.Name,
		newName:  newName,
		spelling: external.spelling,
		filename: position.Filename,
	}
	declaration := declarationOf(file, identifier)
	offset := func(position token.Pos) int { return fileSet.PositionFor(position, false).Offset }
	for _, group := range declaration.comments {
		planned.commentRanges = append(planned.commentRanges, [2]int{offset(group.Pos()), offset(group.End())})
	}
	doc := declaration.doc
	if doc != nil {
		opening := doc.List[0]
		if rest, found := strings.CutPrefix(
			opening.Text,
			"// "+identifier.Name,
		); found && (rest == "" || !isIdentifierByte(rest[0])) {
			planned.subject = &rename.Edit{Filename: position.Filename, Offset: offset(opening.Pos()) + len("// "),
				OldText: identifier.Name, NewText: newName}
		}
		if docNames(doc, external.spelling, planned.subject, offset) {
			return planned
		}
	}
	if external.note == "" {
		planned.refusal = reasonNoNote
		return planned
	}
	source, readError := os.ReadFile(position.Filename)
	if readError != nil {
		planned.refusal = "reading its file: " + readError.Error()
		return planned
	}
	if doc != nil {
		// A closing paragraph of the doc, indented as the doc's own first line is.
		start := offset(doc.Pos())
		indentation := string(source[lineStart(source, start):start])
		text := "\n" + indentation + "//"
		for _, line := range strings.Split(external.note, "\n") {
			text += "\n" + indentation + "// " + line
		}
		planned.note = &rename.Edit{Filename: position.Filename, Offset: offset(doc.End()), NewText: text}
		return planned
	}
	if !strings.HasPrefix(external.note, newName+" ") {
		planned.refusal = reasonNoteOpening + newName
		return planned
	}
	// A new doc on the lines above the declaration, at the declaration's indentation. It is inserted at the
	// line's start, where no identifier's edit stands.
	start := offset(declaration.start)
	begin := lineStart(source, start)
	indentation := string(source[begin:start])
	var text strings.Builder
	for _, line := range strings.Split(external.note, "\n") {
		text.WriteString(indentation + "// " + line + "\n")
	}
	planned.note = &rename.Edit{Filename: position.Filename, Offset: begin, NewText: text.String()}
	planned.noteIsDoc = true
	return planned
}

// lineStart is the offset of the start of the line holding offset.
func lineStart(source []byte, offset int) int {
	return strings.LastIndexByte(string(source[:offset]), '\n') + 1
}

// docNames is whether a doc comment names spelling as a word anywhere but at its subject, the opening name the
// rename respells.
func docNames(doc *ast.CommentGroup, spelling string, subject *rename.Edit, offset func(token.Pos) int) bool {
	for _, comment := range doc.List {
		start := offset(comment.Pos())
		for _, index := range wordIndexes(comment.Text, spelling) {
			if subject != nil && start+index == subject.Offset {
				continue
			}
			return true
		}
	}
	return false
}

// wordIndexes lists where word stands in text with no identifier character on either side.
func wordIndexes(text string, word string) []int {
	var indexes []int
	for start := 0; start+len(word) <= len(text); start++ {
		end := start + len(word)
		if text[start:end] != word ||
			(start > 0 && isIdentifierByte(text[start-1])) ||
			(end < len(text) && isIdentifierByte(text[end])) {
			continue
		}
		indexes = append(indexes, start)
	}
	return indexes
}

func isIdentifierByte(character byte) bool {
	return character == '_' ||
		isDigit(character) ||
		isLowercaseLetter(character) ||
		isUppercaseLetter(character) ||
		character >= 0x80
}

// declaration is the node an identifier declares, as its comments are read: the doc that documents it, every
// comment group the engine respells for it, and where a new doc would go.
type declaration struct {
	doc      *ast.CommentGroup
	comments []*ast.CommentGroup
	start    token.Pos
}

// declarationOf finds the declaration of identifier in file. A spec of a parenthesized group is documented by
// its own doc; a lone spec, by its declaration's. The engine respells a lone spec's declaration doc and every
// spec's own doc and line comment, so those are the comments returned.
func declarationOf(file *ast.File, identifier *ast.Ident) declaration {
	var found declaration
	holds := func(names []*ast.Ident) bool {
		for _, name := range names {
			if name == identifier {
				return true
			}
		}
		return false
	}
	add := func(groups ...*ast.CommentGroup) {
		for _, group := range groups {
			if group != nil {
				found.comments = append(found.comments, group)
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncDecl:
			if typed.Name == identifier {
				found = declaration{doc: typed.Doc, start: typed.Pos()}
				add(typed.Doc)
				return false
			}
		case *ast.GenDecl:
			for _, specification := range typed.Specs {
				var names []*ast.Ident
				var specificationDoc *ast.CommentGroup
				var lineComment *ast.CommentGroup
				switch spec := specification.(type) {
				case *ast.TypeSpec:
					names, specificationDoc, lineComment = []*ast.Ident{spec.Name}, spec.Doc, spec.Comment
				case *ast.ValueSpec:
					names, specificationDoc, lineComment = spec.Names, spec.Doc, spec.Comment
				}
				if !holds(names) {
					continue
				}
				found = declaration{doc: specificationDoc, start: specification.Pos()}
				if !typed.Lparen.IsValid() {
					found.doc, found.start = typed.Doc, typed.Pos()
				}
				if len(typed.Specs) == 1 {
					add(typed.Doc)
				}
				add(specificationDoc, lineComment)
				return false
			}
		case *ast.Field:
			if holds(typed.Names) {
				found = declaration{doc: typed.Doc, start: typed.Pos()}
				add(typed.Doc, typed.Comment)
				return false
			}
		}
		return true
	})
	return found
}

// keepExternalSpellings replaces the engine's comment respellings in each accepted external name's own comments
// with its plan: the subject respelled, every other occurrence kept, the note added where no doc names the
// spelling. It returns what became of each, in position order.
// Mutates: result each plan replaces the engine's respellings in the result's comment edits, which the write reads
func keepExternalSpellings(result *global_rename.Result, externals map[string]plannedExternal) []externalRespelling {
	accepted := map[string]global_rename.Rename{}
	for _, renamed := range result.Accepted {
		key := fmt.Sprintf("%s:%d", renamed.Position.Filename, renamed.Position.Offset)
		if _, isExternal := externals[key]; isExternal {
			accepted[key] = renamed
		}
	}
	inExternalComment := func(edit rename.Edit) bool {
		for key := range accepted {
			planned := externals[key]
			if planned.filename != edit.Filename || edit.OldText != planned.oldName {
				continue
			}
			for _, commentRange := range planned.commentRanges {
				if edit.Offset >= commentRange[0] && edit.Offset < commentRange[1] {
					return true
				}
			}
		}
		return false
	}
	var kept []rename.Edit
	for _, edit := range result.CommentEdits {
		if !inExternalComment(edit) {
			kept = append(kept, edit)
		}
	}
	var respellings []externalRespelling
	for key, renamed := range accepted {
		planned := externals[key]
		respelling := externalRespelling{position: renamed.Position, oldName: renamed.OldName, newName: renamed.NewName,
			outcome: outcomeSpellingNamed}
		if planned.subject != nil {
			kept = append(kept, *planned.subject)
		}
		if planned.note != nil {
			kept = append(kept, *planned.note)
			respelling.outcome = outcomeNoteAdded
			if planned.noteIsDoc {
				respelling.outcome = outcomeNoteAsDoc
			}
		}
		respellings = append(respellings, respelling)
	}
	sort.Slice(kept, func(left, right int) bool {
		if kept[left].Filename != kept[right].Filename {
			return kept[left].Filename < kept[right].Filename
		}
		return kept[left].Offset < kept[right].Offset
	})
	result.CommentEdits = kept
	sort.Slice(respellings, func(left, right int) bool {
		if respellings[left].position.Filename != respellings[right].position.Filename {
			return respellings[left].position.Filename < respellings[right].position.Filename
		}
		return respellings[left].position.Offset < respellings[right].position.Offset
	})
	return respellings
}
