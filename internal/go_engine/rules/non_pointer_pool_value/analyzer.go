// Package non_pointer_pool_value is staticcheck's SA6002, a value given to sync.Pool's Put that is not like a
// pointer, or is a slice, so converting it to the interface Put takes allocates, ported onto staticcheck_call, the
// house's translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa6002/sa6002.go, whose rule it keeps and
// whose message it reports word for word over the same range, the value's expression. It reads the position of Put's
// value from staticcheck's knowledge package, the table of argument positions the original reads, which this wave
// imports as data and a later wave ports, and judges the value's type with staticcheck's typeutil, as the original
// does. The original stays the oracle: the parity tests beside this file run both over staticcheck's own fixture and
// the house's. The two differ nowhere: staticcheck_call departs from callcheck only where it would panic, on calls
// Put is never part of, as testdata/src/differences records.
package non_pointer_pool_value

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"
	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Check judges every call in a function to sync.Pool's Put.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"(*sync.Pool).Put": checkValue,
})

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA6002",
	"Flags a value given to sync.Pool's Put that is not like a pointer, or is a slice, so putting it allocates: "+
		"staticcheck's SA6002, ported onto staticcheck's intermediate representation.",
	Check,
)

// checkValue marks the value Put is given, as the kit hands it, the value before its conversion to Put's interface,
// when its type may not be like a pointer, or is a slice, whose header is three words and so is copied to the heap
// as surely as a struct. A value already of an interface type, or of a type parameter whose type set may hold a
// pointer, is not marked.
func checkValue(call *staticcheck_call.Call) {
	pooled := call.Arguments[knowledge.Arg("(*sync.Pool).Put.x")]
	pooledType := pooled.Value.Type()
	_, isSlice := pooledType.Underlying().(*types.Slice)
	if !typeutil.MaybePointerLike(pooledType) || isSlice {
		pooled.Invalid("argument should be pointer-like to avoid allocations")
	}
}
