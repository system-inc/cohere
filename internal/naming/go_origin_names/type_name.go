package go_origin_names

import "go/types"

// TypeName is a named type by package path and name.
type TypeName struct {
	PackagePath string
	Name        string
}

// NamedTypeOf is the named type `resolvedType` is or points to, or the zero TypeName.
func NamedTypeOf(resolvedType types.Type) TypeName {
	dereferenced := resolvedType
	if pointer, isPointer := types.Unalias(resolvedType).(*types.Pointer); isPointer {
		dereferenced = pointer.Elem()
	}
	if named, isNamed := types.Unalias(dereferenced).(*types.Named); isNamed && named.Obj().Pkg() != nil {
		return TypeName{PackagePath: named.Obj().Pkg().Path(), Name: named.Obj().Name()}
	}
	return TypeName{}
}
