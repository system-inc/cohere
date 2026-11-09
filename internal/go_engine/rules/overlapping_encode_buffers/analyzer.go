// Package overlapping_encode_buffers is staticcheck's SA1031, an encoder of encoding/ascii85, base32, base64 or hex
// given a destination and a source that overlap, which the encoder overwrites as it reads, ported onto
// staticcheck_call, the house's translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1031/sa1031.go, whose rules it keeps
// and whose message it reports word for word over the same range, the destination argument's expression. It reads
// the positions of each encoder's two buffers from staticcheck's knowledge package, the table of argument positions
// the original reads, which this wave imports as data and a later wave ports. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over staticcheck's own fixtures and the house's. The two differ nowhere: staticcheck_call departs from callcheck
// only where it would panic, on calls the encoders are never part of, as testdata/src/differences records.
//
// It keeps one false positive the original makes, for parity until the port takes the original's place: two lower
// bounds that are each a phi of differing values both flatten to nil, so the two are judged one bound and the call
// is marked though the slices never meet. testdata/src/parity holds the case.
package overlapping_encode_buffers

import (
	"go/constant"
	"go/token"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to an encoder that writes a source's encoding into a destination.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"encoding/ascii85.Encode": checkBuffers(
		knowledge.Arg("encoding/ascii85.Encode.dst"),
		knowledge.Arg("encoding/ascii85.Encode.src"),
	),
	"(*encoding/base32.Encoding).Encode": checkBuffers(
		knowledge.Arg("(*encoding/base32.Encoding).Encode.dst"),
		knowledge.Arg("(*encoding/base32.Encoding).Encode.src"),
	),
	"(*encoding/base64.Encoding).Encode": checkBuffers(
		knowledge.Arg("(*encoding/base64.Encoding).Encode.dst"),
		knowledge.Arg("(*encoding/base64.Encoding).Encode.src"),
	),
	"encoding/hex.Encode": checkBuffers(
		knowledge.Arg("encoding/hex.Encode.dst"),
		knowledge.Arg("encoding/hex.Encode.src"),
	),
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1031",
	"SA1031",
	"Flags an encoder of encoding/ascii85, base32, base64 or hex given a destination and a source that overlap: "+
		"staticcheck's SA1031, ported onto the engine's own intermediate representation.",
	&Listener,
)

// checkBuffers is the rule for an encoder whose destination and source are the arguments at destinationIndex and
// sourceIndex. It marks the destination when the two are one value, or slices of one array from one lower bound,
// each seen through phis whose edges are all that one value. Two slices of one array from bounds that differ are not
// judged, though they may overlap too.
func checkBuffers(destinationIndex int, sourceIndex int) staticcheck_call.Rule {
	return func(call *staticcheck_call.Call) {
		destination := call.Arguments[destinationIndex]
		source := call.Arguments[sourceIndex]
		_, isDestinationConstant := intermediate_representation.Flatten(
			destination.Value,
		).(*intermediate_representation.Constant)
		_, isSourceConstant := intermediate_representation.Flatten(source.Value).(*intermediate_representation.Constant)
		if isDestinationConstant || isSourceConstant {
			// A constant slice is nil, which overlaps nothing.
			return
		}
		if destination.Value == source.Value {
			destination.Invalid("overlapping dst and src")
			return
		}
		destinationSlice, isDestinationSlice := intermediate_representation.Flatten(
			destination.Value,
		).(*intermediate_representation.Slice)
		if !isDestinationSlice {
			return
		}
		sourceSlice, isSourceSlice := intermediate_representation.Flatten(
			source.Value,
		).(*intermediate_representation.Slice)
		if !isSourceSlice {
			return
		}
		// Flatten is nil for a phi whose edges differ, so two such arrays, or two such bounds below, are judged one
		// and the same, as the original judges them.
		destinationArray := intermediate_representation.Flatten(destinationSlice.X)
		if destinationArray != intermediate_representation.Flatten(sourceSlice.X) {
			return
		}
		// A slice written without a lower bound has none, nil, so two of them are from one bound, the start.
		destinationLow := intermediate_representation.Flatten(destinationSlice.Low)
		sourceLow := intermediate_representation.Flatten(sourceSlice.Low)
		destinationConstant, isDestinationLowConstant := destinationLow.(*intermediate_representation.Constant)
		sourceConstant, isSourceLowConstant := sourceLow.(*intermediate_representation.Constant)
		if destinationLow == sourceLow || (isDestinationLowConstant && isSourceLowConstant &&
			constant.Compare(destinationConstant.Value, token.EQL, sourceConstant.Value)) {
			destination.Invalid("overlapping dst and src")
		}
	}
}
