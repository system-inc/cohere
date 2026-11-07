package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/types"
	"sort"
	"strings"

	"github.com/system-inc/cohere/tools/go_codemods/rename"

	"golang.org/x/tools/go/packages"
)

// problemLine is a line of a file the per-package check found wrong; line 0 means the whole file.
type problemLine struct {
	filename string
	line     int
	reason   string
}

// checkPackage rewrites pkg's files with edits in memory, type-checks the package again against the imports
// it was loaded with, and returns every line where something is wrong: a type error, an identifier whose name
// or resolution is not what the edits and retargets mean. It is the per-package rehearsal of
// workspace.ApplyVerifiedRetargeted, run before it so one wrong decision can be dropped instead of refusing
// the whole pass. A line a longer name pushes past the house width is the line wrap's, run after the pass.
func checkPackage(
	goPackage *packages.Package,
	edits []rename.Edit,
	retargets map[rename.EditKey]int,
	insertions []rename.Insertion,
) ([]problemLine, error) {
	editsByFile := rename.GroupByFile(edits)
	insertedLines := map[string][]int{}
	seenOffsets := map[rename.EditKey]bool{}
	var problems []problemLine
	for _, insertion := range insertions {
		key := rename.EditKey{Filename: insertion.Filename, Offset: insertion.Offset}
		if seenOffsets[key] {
			problems = append(
				problems,
				problemLine{filename: insertion.Filename, line: lineAt(goPackage, insertion.Filename, insertion.Offset),
					reason: "two declarations inserted before one line"},
			)
		}
		seenOffsets[key] = true
		insertedLines[insertion.Filename] = append(
			insertedLines[insertion.Filename],
			lineAt(goPackage, insertion.Filename, insertion.Offset),
		)
	}
	if len(problems) > 0 {
		return problems, nil
	}
	// originalLine is the line a rewritten line was, which every inserted line before it moved down by one.
	originalLine := func(filename string, rewritten int) int {
		lines := append([]int(nil), insertedLines[filename]...)
		sort.Ints(lines)
		for index, line := range lines {
			if line+index > rewritten {
				return rewritten - index
			}
			if line+index == rewritten {
				return line
			}
		}
		return rewritten - len(lines)
	}
	files := make([]*ast.File, len(goPackage.Syntax))
	edited := map[string]bool{}
	for index, file := range goPackage.Syntax {
		filename := goPackage.Fset.PositionFor(file.Pos(), false).Filename
		files[index] = file
		fileEdits, hasEdits := editsByFile[filename]
		if !hasEdits {
			continue
		}
		edited[filename] = true
		original, rewritten, rewriteError := rename.RewriteFile(filename, fileEdits)
		if rewriteError != nil {
			problems = append(problems, problemLine{filename: filename, reason: rewriteError.Error()})
			continue
		}
		// Problems are reported by line, which holds only while every line but the inserted ones stays.
		if strings.Count(string(original), "\n")+
			len(insertedLines[filename]) != strings.Count(string(rewritten), "\n") {
			problems = append(problems, problemLine{filename: filename, reason: "the rewrite changes the line count"})
			continue
		}
		parsed, parseError := parser.ParseFile(
			goPackage.Fset,
			filename,
			rewritten,
			parser.ParseComments|parser.SkipObjectResolution,
		)
		if parseError != nil {
			problems = append(problems, problemLine{filename: filename, reason: parseError.Error()})
			continue
		}
		files[index] = parsed
	}
	if len(problems) > 0 {
		return problems, nil
	}

	var typeErrors []types.Error
	configuration := types.Config{
		Importer: importerFunction(func(path string) (*types.Package, error) {
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			imported, found := goPackage.Imports[path]
			if !found || imported.Types == nil {
				return nil, fmt.Errorf("%s is not among the loaded imports", path)
			}
			return imported.Types, nil
		}),
		Error: func(checkError error) {
			var typeError types.Error
			if errors.As(checkError, &typeError) {
				typeErrors = append(typeErrors, typeError)
			}
		},
	}
	info := &types.Info{
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Scopes:     map[ast.Node]*types.Scope{},
		Types:      map[ast.Expr]types.TypeAndValue{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	// Ignored: Check returns the first of the type errors, and the Error callback above has kept every one.
	checked, _ := configuration.Check(goPackage.Types.Path(), goPackage.Fset, files, info)
	for _, typeError := range typeErrors {
		position := typeError.Fset.PositionFor(typeError.Pos, false)
		if !edited[position.Filename] {
			return nil, fmt.Errorf("%s: an unedited file does not type-check here: %w", goPackage.ID, typeError)
		}
		problems = append(problems, problemLine{
			filename: position.Filename,
			line:     originalLine(position.Filename, position.Line),
			reason:   typeError.Msg,
		})
	}
	if len(problems) > 0 {
		return problems, nil
	}

	newNames := map[rename.EditKey]string{}
	for _, edit := range edits {
		newNames[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}] = edit.NewText
	}
	offsets := rename.IdentifierOffsets(goPackage.Fset, goPackage.Syntax)
	expected := rename.Fingerprint(goPackage.Fset, goPackage.Types, goPackage.TypesInfo, goPackage.Syntax)
	rename.Expect(expected, offsets, newNames, retargets, insertions)
	actual := rename.Fingerprint(goPackage.Fset, checked, info, files)
	for filename, expectedRecords := range expected {
		actualRecords := actual[filename]
		if len(expectedRecords) != len(actualRecords) {
			problems = append(problems, problemLine{filename: filename, reason: "the identifier count changed"})
			continue
		}
		// The original offset of each expected record; an inserted record has its insertion's.
		recordOffsets := append([]int(nil), offsets[filename]...)
		var inserted []int
		for _, insertion := range insertions {
			if insertion.Filename == filename {
				inserted = append(inserted, insertion.Offset)
			}
		}
		sort.Ints(inserted)
		for which := len(inserted) - 1; which >= 0; which-- {
			at := sort.SearchInts(offsets[filename], inserted[which])
			recordOffsets = append(
				recordOffsets[:at],
				append([]int{inserted[which], inserted[which]}, recordOffsets[at:]...)...,
			)
		}
		for index := range expectedRecords {
			if expectedRecords[index] != actualRecords[index] {
				problems = append(
					problems,
					problemLine{filename: filename, line: lineAt(goPackage, filename, recordOffsets[index]),
						reason: fmt.Sprintf("expected %+v, got %+v", expectedRecords[index], actualRecords[index])},
				)
			}
		}
	}
	return problems, nil
}

// lineAt is the line of offset in one of pkg's files.
func lineAt(goPackage *packages.Package, filename string, offset int) int {
	for _, file := range goPackage.Syntax {
		tokenFile := goPackage.Fset.File(file.Pos())
		if tokenFile.Name() == filename {
			return tokenFile.Line(tokenFile.Pos(offset))
		}
	}
	return 0
}
