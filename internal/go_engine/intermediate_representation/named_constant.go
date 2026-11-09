package intermediate_representation

import (
	"go/token"
	"go/types"
)

// A NamedConstant is a Member of a Package representing a package-level
// named constant.
//
// Pos() returns the position of the declaring ast.ValueSpec.Names[*]
// identifier.
//
// NB: a NamedConstant is not a Value; it contains a constant Value, which
// it augments with the name and position of its 'const' declaration.
type NamedConstant struct {
	object    *types.Const
	Value     *Constant
	irPackage *Package
}

func (namedConstant *NamedConstant) Name() string { return namedConstant.object.Name() }

func (namedConstant *NamedConstant) Pos() token.Pos { return namedConstant.object.Pos() }

func (namedConstant *NamedConstant) String() string { return namedConstant.RelativeString(nil) }

func (namedConstant *NamedConstant) Type() types.Type { return namedConstant.object.Type() }

func (namedConstant *NamedConstant) Token() token.Token { return token.CONST }

func (namedConstant *NamedConstant) Object() types.Object { return namedConstant.object }

func (namedConstant *NamedConstant) Package() *Package { return namedConstant.irPackage }

func (namedConstant *NamedConstant) RelativeString(from *types.Package) string {
	return relativeString(namedConstant, namedConstant.irPackage, from)
}
