package load

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// cgoReach says, of one type-checked package outside the workspace with cgo files, which of its syntax reaches what
// the faked package "C" left invalid. It decides from the syntax and the type information alone, never from an
// error's text, which the type checker is free to reword.
type cgoReach struct {
	fileSet *token.FileSet
	info    *types.Info

	// cgoFiles are the package's cgo files, absolute.
	cgoFiles map[string]bool
}

// newCgoReach is the reach of loaded, a package with the cgo files listed.
func newCgoReach(fileSet *token.FileSet, loaded *packages.Package, listed *listedPackage) *cgoReach {
	cgoFiles := map[string]bool{}
	for _, name := range listed.CgoFiles {
		cgoFiles[filepath.Join(listed.Directory, name)] = true
	}
	return &cgoReach{fileSet: fileSet, info: loaded.TypesInfo, cgoFiles: cgoFiles}
}

// reaches reports whether any identifier in node is one TypesInfo.Uses resolves to package "C" itself, or to an
// object declared in one of the package's cgo files, the files the cgo tool would translate.
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
		used := reach.info.Uses[identifier]
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

// holdsInvalid reports whether node holds an expression the type checker recorded no valid type for, as it records
// none for an expression whose operand the faked package "C" made invalid, though the expression names nothing of
// the cgo files: a comparison of what a `_C_` wrapper returned, say. An identifier is judged by the type of the
// object it uses instead, since the type checker records no defining identifier as an expression, and a key of a
// composite literal and a type switch's guard are left out, since it records neither.
func (reach *cgoReach) holdsInvalid(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(child ast.Node) bool {
		expression, isExpression := child.(ast.Expr)
		if found || !isExpression {
			return !found
		}
		switch expressed := expression.(type) {
		case *ast.Ident:
			// Only a value or a type has a type to judge: a package name, a label and a builtin carry the invalid
			// type by design.
			switch used := reach.info.Uses[expressed].(type) {
			case *types.Var, *types.Const, *types.Func, *types.TypeName:
				found = invalidType(used.Type())
			}
			return !found
		case *ast.KeyValueExpr:
			return true
		case *ast.TypeAssertExpr:
			// A type switch's `x.(type)` has no type of its own, so it is judged by what it asserts on.
			if expressed.Type == nil {
				return true
			}
		}
		recorded, isRecorded := reach.info.Types[expression]
		found = !isRecorded || recorded.Type == nil || recorded.Type == types.Typ[types.Invalid]
		return !found
	})
	return found
}

// invalidType reports whether typed is invalid, or is a pointer, slice, array, channel or map of what is, as the
// faked package "C" leaves the type of every C name and of what is declared from one.
func invalidType(typed types.Type) bool {
	switch underlying := types.Unalias(typed).(type) {
	case nil:
		return false
	case *types.Basic:
		return underlying.Kind() == types.Invalid
	case *types.Pointer:
		return invalidType(underlying.Elem())
	case *types.Slice:
		return invalidType(underlying.Elem())
	case *types.Array:
		return invalidType(underlying.Elem())
	case *types.Chan:
		return invalidType(underlying.Elem())
	case *types.Map:
		return invalidType(underlying.Key()) || invalidType(underlying.Elem())
	default:
		return false
	}
}

// withholds reports whether node is to reach the analyzers without the syntax it holds: it reaches cgo, or holds
// what the faked package "C" left invalid.
func (reach *cgoReach) withholds(node ast.Node) bool {
	return reach.reaches(node) || reach.holdsInvalid(node)
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

// withholdCgoDeclarations gives the analyzers, of every package outside the workspace with cgo files, each function
// whose body withholds (see cgoReach.withholds) without its body, and each package-level variable whose initial
// value withholds without that value, and returns how many declarations it withheld from, each once though a
// package and its test variants share the syntax. The faked package "C" leaves its names, and what they reach,
// without types, and the analyzers that read every expression (buildssa, staticcheck's buildir, printf, SA1019)
// panic on one. A function then reaches them as an assembly function does, its signature without a body, and a
// variable as one declared without a value: its initializer leaves the syntax and TypesInfo.InitOrder, which
// buildssa and buildir build the package's initialization from, so dropping it from the syntax alone still has
// them build it. Constants are left as they are: their values are folded, and no analyzer stopped on one in a load
// of net and os/user for linux, though net's cgoAddrInfoFlags is untyped. It runs once every package is
// type-checked, so it changes only what the analyzers are given, and the rules that read only syntax still see
// everything else.
//
// Mutates: reachable the declarations of its packages that withhold lose their bodies and initial values, and
// those packages' InitOrder loses the initializers
func withholdCgoDeclarations(fileSet *token.FileSet, reachable []*node) int {
	withheld := map[ast.Node]bool{}
	for _, current := range reachable {
		if !current.withholds || current.loaded.TypesInfo == nil {
			continue
		}
		reach := newCgoReach(fileSet, current.loaded, current.listed)
		current.loaded.TypesInfo.InitOrder = slices.DeleteFunc(slices.Clone(current.loaded.TypesInfo.InitOrder),
			func(initializer *types.Initializer) bool { return reach.withholds(initializer.Rhs) })
		for _, file := range current.loaded.Syntax {
			for _, declaration := range file.Decls {
				switch declared := declaration.(type) {
				case *ast.FuncDecl:
					if declared.Body != nil && reach.withholds(declared.Body) {
						declared.Body = nil
						withheld[declared] = true
					}
				case *ast.GenDecl:
					if declared.Tok != token.VAR {
						continue
					}
					for _, specification := range declared.Specs {
						value, isValue := specification.(*ast.ValueSpec)
						if isValue && len(value.Values) > 0 && reach.withholds(value) {
							value.Values = nil
							withheld[value] = true
						}
					}
				}
			}
		}
	}
	return len(withheld)
}
