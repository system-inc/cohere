package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

type edit struct {
	start int
	end   int
	text  string
}

// fixImports makes a file's imports match what its code uses: an import whose name no selector uses
// is dropped (blank, dot and cgo imports stay), and a name the code uses that no import binds is
// imported the way one of the pool's files imports it. A name the pool's files import differently is
// an error, since the moved code was written against one of them.
func fixImports(text []byte, pool []*sourceFile, importName func(string) string) ([]byte, error) {
	fileSet := token.NewFileSet()
	syntax, parseFileError := parser.ParseFile(fileSet, "", text, parser.ParseComments)
	if parseFileError != nil {
		return nil, parseFileError
	}
	file := &sourceFile{source: text, syntax: syntax, tokenFile: fileSet.File(syntax.Pos())}
	used := usedSelectorNames(syntax)
	present := map[string]bool{}
	removed := map[*ast.ImportSpec]bool{}
	for _, spec := range syntax.Imports {
		bound := boundImportName(spec, importName)
		if bound == "_" || bound == "." || importPath(spec) == "C" || used[bound] {
			present[bound] = true
			continue
		}
		removed[spec] = true
	}

	usedNames := make([]string, 0, len(used))
	for name := range used {
		usedNames = append(usedNames, name)
	}
	sort.Strings(usedNames)
	var standardLines []string
	var otherLines []string
	for _, name := range usedNames {
		if present[name] {
			continue
		}
		lines := map[string]bool{}
		standard := false
		for _, source := range pool {
			spec := source.importNames[name]
			if spec == nil {
				continue
			}
			line := spec.Path.Value
			if spec.Name != nil {
				line = spec.Name.Name + " " + line
			}
			lines[line] = true
			standard = isStandardImport(importPath(spec))
		}
		if len(lines) > 1 {
			return nil, fmt.Errorf("the moved code's files import %q differently", name)
		}
		for line := range lines {
			if standard {
				standardLines = append(standardLines, line)
			} else {
				otherLines = append(otherLines, line)
			}
		}
	}

	var edits []edit
	var lastGroup *ast.GenDecl
	for _, declaration := range syntax.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.IMPORT {
			continue
		}
		gone := 0
		for _, spec := range general.Specs {
			if importSpec, isImport := spec.(*ast.ImportSpec); isImport && removed[importSpec] {
				gone++
			}
		}
		if gone == len(general.Specs) {
			span := file.lineRange(docStart(general.Doc, general.Pos()), general.End())
			edits = append(edits, edit{start: span.start, end: span.end})
			continue
		}
		// A block this trims to one import, with nothing to add and no comment inside, becomes the one-line
		// form the house writes a single import in.
		adding := len(standardLines)+len(otherLines) > 0
		if gone > 0 && !adding && general.Lparen != token.NoPos && len(general.Specs)-gone == 1 &&
			!holdsComment(syntax, general) {
			for _, spec := range general.Specs {
				if importSpec, isImport := spec.(*ast.ImportSpec); isImport && !removed[importSpec] {
					span := file.lineRange(general.Pos(), general.End())
					edits = append(
						edits,
						edit{start: span.start, end: span.end, text: "import " + importLine(importSpec) + "\n"},
					)
				}
			}
			continue
		}
		for _, spec := range general.Specs {
			importSpec, isImport := spec.(*ast.ImportSpec)
			if !isImport {
				continue
			}
			if removed[importSpec] {
				span := file.lineRange(docStart(importSpec.Doc, importSpec.Pos()), importSpec.End())
				edits = append(edits, edit{start: span.start, end: span.end})
			}
		}
		if general.Lparen != token.NoPos {
			lastGroup = general
		}
	}

	if len(standardLines)+len(otherLines) > 0 {
		sort.Strings(standardLines)
		sort.Strings(otherLines)
		if lastGroup != nil {
			edits = append(edits, groupedInsertions(file, lastGroup, removed, standardLines, otherLines)...)
		} else {
			// A file with only single-line imports gets one block holding them and the additions, so it never
			// carries two import declarations.
			for _, declaration := range syntax.Decls {
				general, ok := declaration.(*ast.GenDecl)
				if !ok || general.Tok != token.IMPORT {
					continue
				}
				importSpec, isImport := general.Specs[0].(*ast.ImportSpec)
				if !isImport || removed[importSpec] {
					continue
				}
				line := importLine(importSpec)
				if isStandardImport(importPath(importSpec)) {
					standardLines = append(standardLines, line)
				} else {
					otherLines = append(otherLines, line)
				}
				span := file.lineRange(docStart(general.Doc, general.Pos()), general.End())
				edits = append(edits, edit{start: span.start, end: span.end})
			}
			sort.Strings(standardLines)
			sort.Strings(otherLines)
			var builder strings.Builder
			if lines := append(append([]string(nil), standardLines...), otherLines...); len(lines) == 1 {
				builder.WriteString("\nimport " + lines[0] + "\n")
				span := file.lineRange(syntax.Package, syntax.Name.End())
				edits = append(edits, edit{start: span.end, end: span.end, text: builder.String()})
				return applyEdits(text, edits), nil
			}
			builder.WriteString("\nimport (\n")
			for _, line := range standardLines {
				builder.WriteString("\t" + line + "\n")
			}
			if len(standardLines) > 0 && len(otherLines) > 0 {
				builder.WriteString("\n")
			}
			for _, line := range otherLines {
				builder.WriteString("\t" + line + "\n")
			}
			builder.WriteString(")\n")
			span := file.lineRange(syntax.Package, syntax.Name.End())
			edits = append(edits, edit{start: span.end, end: span.end, text: builder.String()})
		}
	}

	return applyEdits(text, edits), nil
}

// applyEdits applies edits to text, the latest first so each offset still points into the original. An insertion
// and a removal at one offset apply in the order they were made.
func applyEdits(text []byte, edits []edit) []byte {
	sort.SliceStable(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	result := append([]byte(nil), text...)
	for _, change := range edits {
		result = append(result[:change.start:change.start], append([]byte(change.text), result[change.end:]...)...)
	}
	return result
}

// importLine is an import spec as a line of an import declaration: its name, if it has one, and its path.
func importLine(spec *ast.ImportSpec) string {
	if spec.Name != nil {
		return spec.Name.Name + " " + spec.Path.Value
	}
	return spec.Path.Value
}

// holdsComment is whether a comment of syntax sits inside declaration.
func holdsComment(syntax *ast.File, declaration *ast.GenDecl) bool {
	for _, group := range syntax.Comments {
		if group.Pos() >= declaration.Pos() && group.End() <= declaration.End() {
			return true
		}
	}
	return false
}

// isStandardImport is whether path is the standard library's: its first element has no dot.
func isStandardImport(path string) bool {
	return !strings.Contains(strings.SplitN(path, "/", 2)[0], ".")
}

// importKind sorts an import path into the groups a file may keep apart: the standard library, the repository's
// own module, and everything else.
func importKind(path string) string {
	switch {
	case isStandardImport(path):
		return "standard"
	case strings.HasPrefix(path, "github.com/system-inc/cohere"):
		return "own"
	default:
		return "external"
	}
}

// groupedInsertions places each added import line in block's group of its kind: after the last import of the
// same kind, else after the last import of any kind but the standard library's for one that is not, else in a
// group of its own, the standard library's first and any other last. gofmt then sorts each group.
func groupedInsertions(file *sourceFile, block *ast.GenDecl, removed map[*ast.ImportSpec]bool,
	standardLines []string, otherLines []string) []edit {
	lastOfKind := map[string]*ast.ImportSpec{}
	var lastNonStandard *ast.ImportSpec
	for _, spec := range block.Specs {
		importSpec, isImport := spec.(*ast.ImportSpec)
		if !isImport || removed[importSpec] {
			continue
		}
		kind := importKind(importPath(importSpec))
		lastOfKind[kind] = importSpec
		if kind != "standard" {
			lastNonStandard = importSpec
		}
	}
	var edits []edit
	insertAfter := map[*ast.ImportSpec][]string{}
	var newStandard []string
	var newOther []string
	for _, line := range standardLines {
		if anchor := lastOfKind["standard"]; anchor != nil {
			insertAfter[anchor] = append(insertAfter[anchor], line)
		} else {
			newStandard = append(newStandard, line)
		}
	}
	for _, line := range otherLines {
		path := strings.Trim(line[strings.Index(line, "\""):], "\"")
		anchor := lastOfKind[importKind(path)]
		if anchor == nil {
			anchor = lastNonStandard
		}
		if anchor != nil {
			insertAfter[anchor] = append(insertAfter[anchor], line)
		} else {
			newOther = append(newOther, line)
		}
	}
	for anchor, lines := range insertAfter {
		end := file.lineRange(anchor.Pos(), anchor.End()).end
		var builder strings.Builder
		for _, line := range lines {
			builder.WriteString("\t" + line + "\n")
		}
		edits = append(edits, edit{start: end, end: end, text: builder.String()})
	}
	if len(newStandard) > 0 {
		start := file.lineRange(block.Lparen, block.Lparen).end
		var builder strings.Builder
		for _, line := range newStandard {
			builder.WriteString("\t" + line + "\n")
		}
		builder.WriteString("\n")
		edits = append(edits, edit{start: start, end: start, text: builder.String()})
	}
	if len(newOther) > 0 {
		offset := file.offset(block.Rparen)
		var builder strings.Builder
		builder.WriteString("\n")
		for _, line := range newOther {
			builder.WriteString("\t" + line + "\n")
		}
		edits = append(edits, edit{start: offset, end: offset, text: builder.String()})
	}
	return edits
}
