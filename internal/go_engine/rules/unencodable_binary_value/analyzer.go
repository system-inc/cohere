// Package unencodable_binary_value is staticcheck's SA1003, a value given to encoding/binary.Write of a type the
// package cannot encode, one with no fixed size or none it knows, ported onto staticcheck_call, the house's
// translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1003/sa1003.go, whose rule it keeps and
// whose message it reports word for word over the same range, the data argument's expression. It finds the data's
// position in staticcheck's knowledge package, a table of standard library argument positions that the house ports
// in a later wave, and reads the standard library's version, before which binary.Write could not encode a bool,
// through staticcheck_report.StandardLibraryVersion. The original stays the oracle: the parity tests beside this file
// run both over staticcheck's own fixtures and the house's, each a module of its own Go version. staticcheck_call
// departs from callcheck only on calls binary.Write is never part of, as testdata/differences records, and the two
// differ in one place. The original reads the version of the file the call's function is in, and the package
// initializer, where a package-level variable's initializer runs, is in no file, so the original panics there on a
// value whose type holds a bool; the port reads the version of the file the call is in, which is the same file for
// every other function, and judges the call, as testdata/package_initializers records.
package unencodable_binary_value

import (
	"fmt"
	"go/types"
	"go/version"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Check judges every call in a function to encoding/binary.Write.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"encoding/binary.Write": checkData,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1003",
	"SA1003",
	"Flags a value given to encoding/binary.Write of a type the package cannot encode: staticcheck's SA1003, "+
		"ported onto staticcheck's intermediate representation.",
	&Listener,
)

// checkData marks the call's data with its type when binary.Write cannot encode a value of that type.
func checkData(call *staticcheck_call.Call) {
	data := call.Arguments[knowledge.Arg("encoding/binary.Write.data")]
	if !encodable(call, data.Value.Type()) {
		data.Invalid(fmt.Sprintf("value of type %s cannot be used with binary.Write", data.Value.Type()))
	}
}

// encodable is whether binary.Write can encode data of dataType given to call: what it encodes is the value a
// pointer points at, and the elements of a slice, an array, a map or a channel, so it is their type that is judged.
// A map's or a channel's elements are judged as the original judges them, though binary.Write refuses both.
func encodable(call *staticcheck_call.Call, dataType types.Type) bool {
	judged := dataType.Underlying()
	if pointer, isPointer := judged.(*types.Pointer); isPointer {
		judged = pointer.Elem().Underlying()
	}
	// A pointer to a pointer is judged as the pointer it points at, which is never encodable.
	if container, isContainer := judged.(interface{ Elem() types.Type }); isContainer {
		if _, isPointer := container.(*types.Pointer); !isPointer {
			judged = container.Elem()
		}
	}
	return encodableType(call, judged)
}

// encodableType is whether binary.Write can encode a value of judged, given to call: a number of fixed size, a bool
// from Go 1.8's standard library, and an array or a struct of encodable values. An interface could hold anything, so
// it is taken as encodable, and so is a type that did not type-check.
func encodableType(call *staticcheck_call.Call, judged types.Type) bool {
	switch underlying := judged.Underlying().(type) {
	case *types.Basic:
		switch underlying.Kind() {
		case types.Uint8, types.Uint16, types.Uint32, types.Uint64,
			types.Int8, types.Int16, types.Int32, types.Int64,
			types.Float32, types.Float64, types.Complex64, types.Complex128, types.Invalid:
			return true
		case types.Bool:
			// The original asks the version of the file of the function the call is in, and the package
			// initializer is in none; the call itself is always in the file whose version it is judged by.
			return version.Compare(
				staticcheck_report.StandardLibraryVersion(call.Context, call.Instruction), "go1.8",
			) >= 0
		}
		return false
	case *types.Struct:
		for index := 0; index < underlying.NumFields(); index++ {
			if !encodableType(call, underlying.Field(index).Type()) {
				return false
			}
		}
		return true
	case *types.Array:
		return encodableType(call, underlying.Elem())
	case *types.Interface:
		return true
	}
	return false
}
