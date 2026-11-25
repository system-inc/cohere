package testing_goroutine

import (
	"go/ast"
	"go/types"
	"slices"
)

// localFunctionDeclarations is a lookup of the declaration in files of each function and method they declare, built on
// the first lookup of a function, and nil for one declared elsewhere. x/tools' localFunctionDecls.
func localFunctionDeclarations(information *types.Info, files []*ast.File) func(*types.Func) *ast.FuncDecl {
	var declarations map[*types.Func]*ast.FuncDecl // Built lazily, as the original builds it.
	return func(function *types.Func) *ast.FuncDecl {
		if function != nil && declarations == nil {
			declarations = map[*types.Func]*ast.FuncDecl{}
			for _, file := range files {
				for _, declared := range file.Decls {
					declaration, isFunction := declared.(*ast.FuncDecl)
					if !isFunction {
						continue
					}
					if defined, isDefined := information.Defs[declaration.Name].(*types.Func); isDefined {
						declarations[defined] = declaration
					}
				}
			}
		}
		return declarations[function]
	}
}

// isMethodNamed reports whether function is a method declared in the package at packagePath, with one of names,
// whatever its receiver's type. x/tools' testinggoroutine isMethodNamed, which unlike analysis.IsMethodNamed ignores
// the receiver's type name.
func isMethodNamed(function *types.Func, packagePath string, names ...string) bool {
	if function == nil {
		return false
	}
	if function.Pkg() == nil || function.Pkg().Path() != packagePath {
		return false
	}
	if function.Signature().Recv() == nil {
		return false
	}
	return slices.Contains(names, function.Name())
}

// functionLiteralInScope is the function literal identifier is first assigned, by the declaration the parser resolved
// it to in its own file, a `:=` or a `var`, and nil when it is first assigned anything else, or declared in another
// file or not as a variable. A later `=` does not count, so what the variable holds by then is not followed. x/tools'
// funcLitInScope.
func functionLiteralInScope(identifier *ast.Ident) *ast.FuncLit {
	if identifier.Obj == nil {
		return nil
	}
	var assigned ast.Expr
	switch declaration := identifier.Obj.Decl.(type) {
	case *ast.AssignStmt:
		for index, left := range declaration.Lhs {
			if name, isIdentifier := left.(*ast.Ident); isIdentifier && name.Name == identifier.Name &&
				index < len(declaration.Rhs) {
				assigned = declaration.Rhs[index]
			}
		}
	case *ast.ValueSpec:
		for index, name := range declaration.Names {
			if name.Name == identifier.Name && index < len(declaration.Values) {
				assigned = declaration.Values[index]
			}
		}
	}
	literal, isLiteral := assigned.(*ast.FuncLit)
	if !isLiteral {
		return nil
	}
	return literal
}

// usedIdentifier is the identifier whose use expression denotes, looking through parentheses and an instantiation of
// a generic function, `function[int]`, and nil when expression is neither an identifier nor a selector. x/tools'
// typesinternal.UsedIdent, less its panic when information lacks Types or Uses, which a pass always fills.
func usedIdentifier(information *types.Info, expression ast.Expr) *ast.Ident {
	// uninstantiated is expression, or, when it instantiates a generic function, that function without its type
	// arguments.
	uninstantiated := expression
	switch instantiated := ast.Unparen(expression).(type) {
	case *ast.IndexExpr:
		// `values[index]` is an index into a value, not an instantiation, and is judged as written.
		if information.Types[instantiated.Index].IsType() {
			uninstantiated = instantiated.X
		}
	case *ast.IndexListExpr:
		uninstantiated = instantiated.X
	}
	switch used := ast.Unparen(uninstantiated).(type) {
	// Uses records the object a selector names under its Sel, for a qualified identifier, a method value and a
	// method expression alike, so Selections is never needed.
	case *ast.Ident:
		return used
	case *ast.SelectorExpr:
		return used.Sel
	}
	return nil
}

// imports reports whether importer imports the package at path directly. x/tools' typesinternal.Imports.
func imports(importer *types.Package, path string) bool {
	return slices.ContainsFunc(importer.Imports(), func(dependency *types.Package) bool {
		return dependency.Path() == path
	})
}
