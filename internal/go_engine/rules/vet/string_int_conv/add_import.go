package string_int_conv

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// addImport is the prefix a reference at position to member of the package at packagePath is written with, `name.` or
// empty for a dot import, and the edits that import the package where file does not already, under preferredName or,
// where something at position already holds that name, the first fresh name after it. An existing import serves when
// position is within its scope: a named one when nothing nearer shadows its name, a dot one when member resolves in
// the file's scope. A preferredName of `_` asks for a blank import, any existing import of the package serving.
// x/tools' refactor.AddImport.
func addImport(
	information *types.Info, file *ast.File, preferredName string, packagePath string, member string,
	position token.Pos,
) (string, []analysis.TextEdit) {
	scope := information.Scopes[file].Innermost(position)
	if scope == nil {
		// Invariant: position is within file, whose scope holds every position in it; the original panics here too.
		panic("string_int_conv: no enclosing lexical block")
	}
	for _, specification := range file.Imports {
		imported := information.PkgNameOf(specification)
		if imported == nil || imported.Imported().Path() != packagePath {
			continue
		}
		// Kept for fidelity, and this pass never reaches it: it asks for fmt by name, never for a blank import.
		if preferredName == "_" {
			return "", nil
		}
		if imported.Name() == "." {
			// A dot import serves when member resolves in the file's scope, where the dot import declares it, and
			// not in a nearer one that shadows it.
			if resolvedScope, _ := scope.LookupParent(member, position); resolvedScope == information.Scopes[file] {
				return "", nil
			}
			continue
		}
		if _, resolved := scope.LookupParent(imported.Name(), position); resolved == imported {
			return imported.Name() + ".", nil
		}
	}

	// The package must be imported anew, under a name nothing at position holds.
	newName := preferredName
	prefix := ""
	if preferredName != "_" {
		newName = freshName(scope, position, preferredName)
		prefix = newName + "."
	}
	// The import is named only where the name chosen is not the preferred one, or not the last element of the path.
	if newName == preferredName && newName == path.Base(packagePath) {
		newName = ""
	}
	return prefix, addImportEdits(file, newName, packagePath)
}

// freshName is preferred, or where something at position already holds that name, the first of preferred followed by
// 0, 1, 2 and on that nothing holds. x/tools' refactor.FreshName.
func freshName(scope *types.Scope, position token.Pos, preferred string) string {
	candidate := preferred
	for index := 0; ; index++ {
		if _, declared := scope.LookupParent(candidate, position); declared == nil {
			return candidate
		}
		candidate = fmt.Sprintf("%s%d", preferred, index)
	}
}

// addImportEdits is the one edit that imports packagePath into file, named name where that is not empty: a new spec in
// the file's first declaration when that is a grouped import, a standard package's before its first spec, followed by
// a blank line where that spec is not a standard package's, and any other at the group's end; otherwise a new import
// declaration before the first declaration, its doc comment included, or at the file's end when it has none.
// x/tools' refactor.AddImportEdits.
func addImportEdits(file *ast.File, name string, packagePath string) []analysis.TextEdit {
	newText := strconv.Quote(packagePath)
	if name != "" {
		newText = name + " " + newText
	}
	var before token.Pos
	var firstDeclaration ast.Decl
	if len(file.Decls) > 0 {
		firstDeclaration = file.Decls[0]
		before = firstDeclaration.Pos()
		switch declaration := firstDeclaration.(type) {
		case *ast.GenDecl:
			if declaration.Doc != nil {
				before = declaration.Doc.Pos()
			}
		case *ast.FuncDecl:
			if declaration.Doc != nil {
				before = declaration.Doc.Pos()
			}
		}
	} else {
		before = file.FileEnd
	}
	var position token.Pos
	group, isGeneral := firstDeclaration.(*ast.GenDecl)
	switch {
	case isGeneral && group.Tok == token.IMPORT && group.Rparen.IsValid() &&
		maybeStandardPackage(packagePath) && len(group.Specs) > 0:
		first, isImport := group.Specs[0].(*ast.ImportSpec)
		if !isImport {
			// Invariant: the parser puts only import specs in an import declaration.
			panic("string_int_conv: an import declaration holds a spec that is not an import")
		}
		position = first.Pos()
		// The first spec's path is read with its quotes, as the original reads it, which answers as the bare path
		// does for every path but `testdata`, standard only when quoted.
		if !maybeStandardPackage(first.Path.Value) {
			newText += "\n"
		}
		newText += "\n\t"
	case isGeneral && group.Tok == token.IMPORT && group.Rparen.IsValid():
		position = group.Rparen
		newText = "\t" + newText + "\n"
	default:
		// gofmt merges the import declarations this may leave side by side.
		position = before
		newText = "import " + newText + "\n\n"
	}
	return []analysis.TextEdit{{Pos: position, End: position, NewText: []byte(newText)}}
}

// maybeStandardPackage is whether packagePath may be a standard library package's: one whose first element holds no
// dot, other than `testdata`. x/tools' packagepath.MaybeStdPackage.
func maybeStandardPackage(packagePath string) bool {
	slash := strings.IndexByte(packagePath, '/')
	if slash < 0 {
		slash = len(packagePath)
	}
	return !strings.Contains(packagePath[:slash], ".") && packagePath != "testdata"
}
