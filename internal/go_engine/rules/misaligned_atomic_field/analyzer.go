// Package misaligned_atomic_field is staticcheck's SA1027, the address of a struct field that is not 64-bit aligned
// given to one of sync/atomic's 64-bit functions on a 32-bit platform, ported onto staticcheck_call, the house's
// translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1027/sa1027.go, whose rules it keeps
// and whose message it reports word for word over the same range, the address argument's expression. The platform is
// the one the package was checked for, read from the context's TypesSizes where the original reads its pass's. The
// original asserts the shape of the field's struct and panics should the representation hand it another; the port
// asserts with a check and judges nothing there, a shape the representation never builds. The registry runs the port in
// the original's place, under its name, and the original stays only as the oracle: the parity tests beside this file
// run both over staticcheck's own fixture and the house's, on 32-bit and 64-bit platforms. staticcheck_call departs
// from callcheck only on calls sync/atomic's functions are never part of, as testdata/src/differences records, and the
// two differ in one place. In a generic function's own body, a field placed after one sized by a type parameter has no
// offset, and go/types panics when the original asks for one: its Offsetsof asks Alignof of the type parameter, which
// fails the assertion at go/types/gcsizes.go:54 (Go 1.27, "assertion failed"), and the panic takes the original down
// with every other finding of the package. The port judges nothing there, as testdata/src/type_parameters records.
package misaligned_atomic_field

import (
	"fmt"
	"go/types"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to one of sync/atomic's functions on a 64-bit word.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"sync/atomic.AddInt64":             checkAlignment,
	"sync/atomic.AddUint64":            checkAlignment,
	"sync/atomic.CompareAndSwapInt64":  checkAlignment,
	"sync/atomic.CompareAndSwapUint64": checkAlignment,
	"sync/atomic.LoadInt64":            checkAlignment,
	"sync/atomic.LoadUint64":           checkAlignment,
	"sync/atomic.StoreInt64":           checkAlignment,
	"sync/atomic.StoreUint64":          checkAlignment,
	"sync/atomic.SwapInt64":            checkAlignment,
	"sync/atomic.SwapUint64":           checkAlignment,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1027",
	"SA1027",
	"Flags the address of a struct field that is not 64-bit aligned given to one of sync/atomic's 64-bit "+
		"functions on a 32-bit platform: staticcheck's SA1027, ported onto staticcheck's intermediate representation.",
	&Listener,
)

// checkAlignment marks the call when its first argument, the address of the word, is the address of a struct field
// whose offset in its struct is not a multiple of 8, on a platform whose pointers are 4 bytes. Only a field's
// address is judged, never an element of an array or a slice, as the original judges it. The offset is the field's
// in the struct it is named in, so a field of a struct nested in another is judged by where it sits in the inner one.
func checkAlignment(call *staticcheck_call.Call) {
	// Only a 32-bit platform leaves a 64-bit word's alignment to the caller.
	if call.Context.TypesSizes.Sizeof(types.Typ[types.Uintptr]) != 4 {
		return
	}
	field, isField := irutil.Flatten(call.Arguments[0].Value).(*ir.FieldAddr)
	if !isField {
		return
	}
	// A field's address is taken through a pointer to its struct; the original asserts both without a check.
	pointer, isPointer := field.X.Type().Underlying().(*types.Pointer)
	if !isPointer {
		return
	}
	structure, isStructure := pointer.Elem().Underlying().(*types.Struct)
	if !isStructure {
		return
	}
	// The fields up to and including the one judged are all Offsetsof needs to place it.
	fields := make([]*types.Var, 0, field.Field+1)
	for index := 0; index < structure.NumFields() && index <= field.Field; index++ {
		// A field sized by a type parameter, in a generic function's own body, has no offset until the function is
		// instantiated, and go/types panics when asked for one, as it does under the original's Offsetsof; the port
		// judges nothing there.
		if sizedByTypeParameter(structure.Field(index).Type()) {
			return
		}
		fields = append(fields, structure.Field(index))
	}
	if call.Context.TypesSizes.Offsetsof(fields)[field.Field]%8 != 0 {
		call.Invalid(fmt.Sprintf("address of non 64-bit aligned field %s passed to %s",
			structure.Field(field.Field).Name(), irutil.CallName(call.Instruction.Common())))
	}
}

// sizedByTypeParameter is whether the size or alignment of a value of fieldType depends on a type parameter:
// fieldType is one, or an array or a struct holding one in place. A pointer, slice, map, channel, function or
// interface is the same size whatever it refers to.
func sizedByTypeParameter(fieldType types.Type) bool {
	if _, isTypeParameter := types.Unalias(fieldType).(*types.TypeParam); isTypeParameter {
		return true
	}
	switch underlying := fieldType.Underlying().(type) {
	case *types.Array:
		return sizedByTypeParameter(underlying.Elem())
	case *types.Struct:
		for index := 0; index < underlying.NumFields(); index++ {
			if sizedByTypeParameter(underlying.Field(index).Type()) {
				return true
			}
		}
	}
	return false
}
