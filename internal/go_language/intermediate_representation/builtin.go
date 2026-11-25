package intermediate_representation

import (
	"go/token"
	"go/types"
)

// A Builtin represents a specific use of a built-in function, e.g. len.
//
// Builtins are immutable values.  Builtins do not have addresses.
// Builtins can only appear in CallCommon.Value.
//
// Name() indicates the function: one of the built-in functions from the
// Go spec (excluding "make" and "new") or one of these ir-defined
// intrinsics:
//
//	// wrapnilchk returns ptr if non-nil, panics otherwise.
//	// (For use in indirection wrappers.)
//	func ir:wrapnilchk(ptr *T, recvType, methodName string) *T
//
//	// noreturnWasPanic returns true if the previously called
//	// function panicked, false if it exited the process.
//	func ir:noreturnWasPanic() bool
//
// Object() returns a *types.Builtin for built-ins defined by the spec,
// nil for others.
//
// Type() returns a *types.Signature representing the effective
// signature of the built-in for this call.
type Builtin struct {
	node

	name string
	sig  *types.Signature
}

func (builtin *Builtin) Type() types.Type { return builtin.sig }

func (builtin *Builtin) Name() string { return builtin.name }

func (*Builtin) Referrers() *[]Instruction { return nil }

func (builtin *Builtin) Pos() token.Pos { return token.NoPos }

func (builtin *Builtin) Object() types.Object { return types.Universe.Lookup(builtin.name) }

func (builtin *Builtin) Parent() *Function { return nil }

// Non-Instruction Values:
func (builtin *Builtin) Operands(rands []*Value) []*Value { return rands }
