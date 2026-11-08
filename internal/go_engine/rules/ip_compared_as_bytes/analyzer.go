// Package ip_compared_as_bytes is staticcheck's SA1021, two net.IP values compared with bytes.Equal, which tells an
// IPv4 address held in four bytes from the same address held in sixteen, ported onto staticcheck_call, the house's
// translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1021/sa1021.go, whose rule it keeps
// and whose message it reports word for word over the same range, the call. It reads the positions of bytes.Equal's
// two arguments from staticcheck's knowledge package, the table of argument positions the original reads, which this
// wave imports as data and a later wave ports. The registry runs the port in the original's place, under its name, and
// the original stays only as the oracle: the parity tests beside this file run both over staticcheck's own fixture and
// the house's. The two differ nowhere: staticcheck_call departs from callcheck only where it would panic, on calls
// bytes.Equal is never part of, as testdata/src/differences records.
package ip_compared_as_bytes

import (
	"go/types"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to bytes.Equal.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"bytes.Equal": checkComparison,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1021",
	"SA1021",
	"Flags two net.IP values compared with bytes.Equal, which tells the four and sixteen byte forms of one IPv4 "+
		"address apart: staticcheck's SA1021, ported onto staticcheck's intermediate representation.",
	&Listener,
)

// checkComparison marks the call when both of its arguments are net.IP values, each changed to the []byte
// bytes.Equal takes. One net.IP compared with plain bytes is not judged.
func checkComparison(call *staticcheck_call.Call) {
	if isChangedFrom(call.Arguments[knowledge.Arg("bytes.Equal.a")].Value, "net.IP") &&
		isChangedFrom(call.Arguments[knowledge.Arg("bytes.Equal.b")].Value, "net.IP") {
		call.Invalid("use net.IP.Equal to compare net.IPs, not bytes.Equal")
	}
}

// isChangedFrom is whether value is a change of type, the conversion between two types of one underlying type,
// from the type named typeName, its alias seen through. A type defined from net.IP is named for itself, so it is not
// net.IP here.
func isChangedFrom(value ir.Value, typeName string) bool {
	change, isChange := value.(*ir.ChangeType)
	return isChange && types.TypeString(types.Unalias(change.X.Type()), nil) == typeName
}
