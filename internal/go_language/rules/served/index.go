package served

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// Index knows which types of one package count, so each file is judged against every method in the package.
type Index struct {
	pass   *analysis.Pass
	counts map[*types.TypeName]bool
}

// Types is every type file serves, in source order: each counted type it declares, placed at its declaration, and
// each counted type declared elsewhere that it declares a method of, placed at the receiver of the first.
func (index *Index) Types(file *ast.File) []Type {
	served := []Type{}
	isServed := map[*types.TypeName]bool{}
	// Declarations are read before receivers, so a type this file declares is placed at its declaration wherever its
	// methods here sit. An alias declares no type of its own, so its object never has methods and never counts.
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
			if !isTypeSpec {
				continue
			}
			typeName, isTypeName := index.pass.TypesInfo.Defs[typeSpec.Name].(*types.TypeName)
			if !isTypeName || !index.counts[typeName] || isServed[typeName] {
				continue
			}
			served = append(served, Type{TypeName: typeName, Node: typeSpec.Name, Declared: true})
			isServed[typeName] = true
		}
	}
	// A marker type's receivers are skipped like its declaration, so a variant's marker method beside its interface
	// serves nothing.
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction {
			continue
		}
		typeName := receiverTypeName(index.pass, function)
		if typeName == nil || !index.counts[typeName] || isServed[typeName] {
			continue
		}
		served = append(served, Type{TypeName: typeName, Node: function.Recv.List[0].Type})
		isServed[typeName] = true
	}
	slices.SortFunc(served, func(left, right Type) int {
		return cmp.Compare(left.Node.Pos(), right.Node.Pos())
	})
	return served
}
