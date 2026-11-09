package intermediate_representation

import (
	"go/token"
	"go/types"
)

// A Type is a Member of a Package representing a package-level named type.
type Type struct {
	object    *types.TypeName
	irPackage *Package
}

func (namedType *Type) Name() string { return namedType.object.Name() }

func (namedType *Type) Pos() token.Pos { return namedType.object.Pos() }

func (namedType *Type) Type() types.Type { return namedType.object.Type() }

func (namedType *Type) Token() token.Token { return token.TYPE }

func (namedType *Type) Object() types.Object { return namedType.object }

func (namedType *Type) String() string { return namedType.RelativeString(nil) }

func (namedType *Type) Package() *Package { return namedType.irPackage }

func (namedType *Type) RelativeString(from *types.Package) string {
	return relativeString(namedType, namedType.irPackage, from)
}
