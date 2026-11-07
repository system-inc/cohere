package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// planSyntacticFiles adds the edits of every Go file under the root that no type-checked load read: a fixture
// under testdata, or a file whose build constraint no platform satisfies. Without types, a qualifier is a
// selector's left side the parser could not resolve to anything in the file, spelled as the import's name, and a
// file that declares the new name anywhere is refused rather than guessed at.
func (plan *plan) planSyntacticFiles(root string, mapping *mapping, sources fileSource, typed map[string]bool) error {
	return filepath.WalkDir(root, func(filename string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		relative, relativeError := filepath.Rel(root, filename)
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative == "TypeScript" ||
				relative == ".git" ||
				strings.HasPrefix(entry.Name(), ".") && relative != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(filename, ".go") || typed[filename] {
			return nil
		}
		contents, readError := sources.read(filename)
		if readError != nil {
			return readError
		}
		fileSet := token.NewFileSet()
		file, parseError := parser.ParseFile(fileSet, filename, contents, parser.ParseComments)
		if parseError != nil {
			// A fixture may be broken on purpose; one that imports nothing moved needs nothing.
			if strings.Contains(string(contents), mapping.modulePath+"/") {
				plan.notes = append(
					plan.notes,
					fmt.Sprintf("does not parse, so not read: %s: %v", relative, parseError),
				)
			}
			return nil
		}
		before := len(plan.edits)
		plan.planSyntacticFile(mapping, filename, path.Dir(relative), fileSet, file, contents)
		if len(plan.edits) > before {
			plan.counts.SyntacticFiles++
			plan.notes = append(plan.notes, "read without types: "+relative)
		}
		return nil
	})
}

// planSyntacticFile is planTypedFile for a file read without types.
func (plan *plan) planSyntacticFile(
	mapping *mapping,
	filename string,
	relativeFolder string,
	fileSet *token.FileSet,
	file *ast.File,
	contents []byte,
) {
	offset := func(position token.Pos) int { return fileSet.PositionFor(position, false).Offset }
	plan.planClause(mapping, relativeFolder, filename, file, offset)
	for _, spec := range file.Imports {
		importPath, unquoteError := strconv.Unquote(spec.Path.Value)
		if unquoteError != nil {
			continue
		}
		newPath := mapping.NewImportPath(importPath)
		if newPath == importPath {
			continue
		}
		plan.add(filename, offset(spec.Path.Pos()), spec.Path.Value, strconv.Quote(newPath))
		plan.counts.ImportPaths++
		// Without types the old name is the folder's, which every moved folder's clause is checked to match.
		oldName := path.Base(importPath)
		newName := mapping.ImportedPackageName(importPath, oldName)
		if newName == oldName {
			continue
		}
		written := oldName
		if spec.Name != nil {
			if spec.Name.Name != oldName {
				if spec.Name.Name != "_" && spec.Name.Name != "." {
					plan.counts.AliasesKept++
				}
				continue
			}
			plan.add(
				filename,
				offset(spec.Name.Pos()),
				string(contents[offset(spec.Name.Pos()):offset(spec.Path.Pos())]),
				"",
			)
			plan.counts.AliasesDropped++
			written = spec.Name.Name
		}
		standing := false
		ast.Inspect(file, func(node ast.Node) bool {
			if identifier, isIdentifier := node.(*ast.Ident); isIdentifier &&
				identifier.Name == newName &&
				identifier.Obj != nil {
				standing = true
			}
			return true
		})
		if standing {
			plan.refusals = append(
				plan.refusals,
				fmt.Sprintf("%s: declares %s, the import's new name, and is read without types", filename, newName),
			)
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, isSelector := node.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			qualifier, isIdentifier := selector.X.(*ast.Ident)
			if isIdentifier && qualifier.Name == written && qualifier.Obj == nil {
				plan.add(filename, offset(qualifier.Pos()), qualifier.Name, newName)
				plan.counts.Qualifiers++
			}
			return true
		})
	}
}
