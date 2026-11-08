// Package unsupported_marshal_type is staticcheck's SA1026, a value given to encoding/json or encoding/xml to marshal
// whose type the encoder cannot marshal, a channel, a function, a map xml cannot hold or a cyclic type, ported onto
// staticcheck_call, the house's translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1026/sa1026.go, whose rules it keeps
// and whose messages it reports word for word over the same range, the value argument's expression. Whether a type
// marshals is asked of staticcheck's fakejson and fakexml, its forks of the encoders' type walks onto go/types, which
// this wave imports as data and wave 5 ports. A type is named relative to the context's package, where the original
// names it relative to the package of the function the call is in, the same package for every function the check is
// handed. The original stays the oracle: the parity tests beside this file run both over staticcheck's own fixture
// and the house's. The two differ nowhere: staticcheck_call departs from callcheck only on calls whose findings land
// in the same place, as testdata/src/differences records.
package unsupported_marshal_type

import (
	"fmt"
	"go/types"

	"honnef.co/go/tools/staticcheck/fakejson"
	"honnef.co/go/tools/staticcheck/fakexml"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to an encoding/json or encoding/xml function that marshals its first
// argument.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"encoding/json.Marshal":           checkJson,
	"encoding/json.MarshalIndent":     checkJson,
	"encoding/xml.Marshal":            checkXml,
	"encoding/xml.MarshalIndent":      checkXml,
	"(*encoding/json.Encoder).Encode": checkJson,
	"(*encoding/xml.Encoder).Encode":  checkXml,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1026",
	"SA1026",
	"Flags a value given to encoding/json or encoding/xml to marshal whose type cannot be marshaled, such as a "+
		"channel or a function: staticcheck's SA1026, ported onto staticcheck's intermediate representation.",
	&Listener,
)

// checkJson marks the call's value, its first argument, when encoding/json cannot marshal its type, naming the type
// it stops at and the path to it.
func checkJson(call *staticcheck_call.Call) {
	marshaled := call.Arguments[0]
	if unsupported := fakejson.Marshal(marshaled.Value.Type()); unsupported != nil {
		markMarshaled(call, marshaled, "unsupported", unsupported.Type, unsupported.Path)
	}
}

// checkXml marks the call's value, its first argument, when encoding/xml cannot marshal its type or the type refers
// to itself through pointers alone, naming the type it stops at and the path to it.
func checkXml(call *staticcheck_call.Call) {
	marshaled := call.Arguments[0]
	switch marshalError := fakexml.Marshal(marshaled.Value.Type()).(type) {
	case *fakexml.UnsupportedTypeError:
		markMarshaled(call, marshaled, "unsupported", marshalError.Type, marshalError.Path)
	case *fakexml.CyclicTypeError:
		markMarshaled(call, marshaled, "cyclic", marshalError.Type, marshalError.Path)
	}
	// Every other error is left unmarked, as the original leaves it: a conflict between struct tags vet reports at
	// the tags themselves, and a malformed field SA5008 reports at the field, each better placed than the call.
}

// markMarshaled marks the argument marshaled with the original's message for a type of the kind named, unsupported
// or cyclic, found at path from it. The path is "x" when the argument's own type is the one, and then it goes
// unsaid.
func markMarshaled(
	call *staticcheck_call.Call,
	marshaled *staticcheck_call.Argument,
	kind string,
	found types.Type,
	path string,
) {
	typeName := types.TypeString(found, types.RelativeTo(call.Context.Package))
	if path == "x" {
		marshaled.Invalid(fmt.Sprintf("trying to marshal %s type %s", kind, typeName))
		return
	}
	marshaled.Invalid(fmt.Sprintf("trying to marshal %s type %s, via %s", kind, typeName, path))
}
