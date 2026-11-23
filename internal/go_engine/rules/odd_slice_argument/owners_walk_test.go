package odd_slice_argument

import (
	"go/ast"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// ownersByWalk is where each named parameter of home's functions and function literals is declared, read from all of
// its syntax at once: the oracle ownerOf, which reads only the declaration a parameter is in, is held to. A receiver is
// left out, since no call passes a slice to one as a parameter.
func ownersByWalk(home *fact_resolver.Package) map[*types.Var]parameterOwner {
	owners := map[*types.Var]parameterOwner{}
	add := func(fields *ast.FieldList, owner parameterOwner) {
		for _, field := range fields.List {
			for _, name := range field.Names {
				if variable, isVariable := home.Info().Defs[name].(*types.Var); isVariable {
					owners[variable] = owner
				}
			}
		}
	}
	addLiterals := func(node ast.Node, declared *types.Func) {
		ast.Inspect(node, func(inspected ast.Node) bool {
			if literal, isLiteral := inspected.(*ast.FuncLit); isLiteral {
				add(literal.Type.Params, parameterOwner{declared: declared, literal: literal})
			}
			return true
		})
	}
	for _, file := range home.Files() {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				addLiterals(declaration, nil)
				continue
			}
			declared, isDeclared := home.Info().Defs[function.Name].(*types.Func)
			if !isDeclared {
				continue
			}
			add(function.Type.Params, parameterOwner{declared: declared})
			if function.Body != nil {
				addLiterals(function.Body, declared)
			}
		}
	}
	return owners
}
