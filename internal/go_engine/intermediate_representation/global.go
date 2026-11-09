package intermediate_representation

import (
	"go/token"
	"go/types"
)

// A Global is a named Value holding the address of a package-level
// variable.
//
// Pos() returns the position of the ast.ValueSpec.Names[*]
// identifier.
type Global struct {
	node

	name   string
	object types.Object // a *types.Var; may be nil for synthetics e.g. init$guard
	typ    types.Type

	Package *Package
}

func (global *Global) Type() types.Type { return global.typ }

func (global *Global) Name() string { return global.name }

func (global *Global) Parent() *Function { return nil }

func (global *Global) Referrers() *[]Instruction { return nil }

func (global *Global) Token() token.Token { return token.VAR }

func (global *Global) Object() types.Object { return global.object }

func (global *Global) String() string { return global.RelativeString(nil) }

func (global *Global) RelativeString(from *types.Package) string {
	return relativeString(global, global.Package, from)
}

func (global *Global) Operands(rands []*Value) []*Value { return rands }
