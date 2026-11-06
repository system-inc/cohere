package load

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// cgoReach says, of one type-checked package outside the workspace with cgo files, which of its syntax reaches what
// the faked package "C" left invalid: an identifier the type checker resolved to package "C" itself, or to an
// object declared in one of the package's cgo files, the files the cgo tool would translate. It decides from the
// syntax and TypesInfo.Uses alone, never from an error's text, which the type checker is free to reword.
type cgoReach struct {
	fileSet *token.FileSet
	uses    map[*ast.Ident]types.Object

	// cgoFiles are the package's cgo files, absolute.
	cgoFiles map[string]bool
}

// newCgoReach is the reach of loaded, a package with the cgo files listed.
func newCgoReach(fileSet *token.FileSet, loaded *packages.Package, listed *listedPackage) *cgoReach {
	cgoFiles := map[string]bool{}
	for _, name := range listed.CgoFiles {
		cgoFiles[filepath.Join(listed.Directory, name)] = true
	}
	return &cgoReach{fileSet: fileSet, uses: loaded.TypesInfo.Uses, cgoFiles: cgoFiles}
}

// reaches reports whether any identifier in node uses package "C" or an object declared in a cgo file.
func (reach *cgoReach) reaches(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(child ast.Node) bool {
		if found {
			return false
		}
		identifier, isIdentifier := child.(*ast.Ident)
		if !isIdentifier {
			return true
		}
		used := reach.uses[identifier]
		if used == nil {
			return true
		}
		if packageName, isPackageName := used.(*types.PkgName); isPackageName && packageName.Imported().Path() == "C" {
			found = true
			return false
		}
		if declared := reach.fileSet.File(used.Pos()); declared != nil && reach.cgoFiles[declared.Name()] {
			found = true
		}
		return !found
	})
	return found
}

// errorReaches reports whether the expression at position, in one of syntax, reaches cgo: the widest expression
// enclosing the position before a statement or declaration does, which is the expression the error is about and
// no more, so an error elsewhere in the same statement is judged by its own expression. A position in no
// expression, or in no file of syntax, reaches nothing.
func (reach *cgoReach) errorReaches(syntax []*ast.File, position token.Pos) bool {
	for _, file := range syntax {
		if position < file.FileStart || position > file.FileEnd {
			continue
		}
		path, _ := astutil.PathEnclosingInterval(file, position, position)
		var widest ast.Expr
		for _, enclosing := range path {
			expression, isExpression := enclosing.(ast.Expr)
			if !isExpression {
				break
			}
			widest = expression
		}
		return widest != nil && reach.reaches(widest)
	}
	return false
}

// withholdCgoBodies takes its body out of every function of a package outside the workspace whose body reaches cgo,
// and returns how many functions it took the body out of, each once though a package and its test variants share
// the syntax. The faked package "C" leaves its names untyped, and the analyzers that walk a body's every expression
// (buildssa, staticcheck's buildir, printf, SA1019) panic on one, so such a function reaches them as an assembly
// function does, its signature without a body. It runs once every package is type-checked, so it changes only what
// the analyzers are given: the type information already holds the bodies, and the rules that read only syntax
// still see everything outside those functions.
//
// Mutates: reachable the functions its packages declare that reach cgo lose their bodies
func withholdCgoBodies(fileSet *token.FileSet, reachable []*node) int {
	withheld := map[*ast.FuncDecl]bool{}
	for _, current := range reachable {
		if !current.withholds || current.loaded.TypesInfo == nil {
			continue
		}
		reach := newCgoReach(fileSet, current.loaded, current.listed)
		for _, file := range current.loaded.Syntax {
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction || function.Body == nil || !reach.reaches(function.Body) {
					continue
				}
				function.Body = nil
				withheld[function] = true
			}
		}
	}
	return len(withheld)
}
