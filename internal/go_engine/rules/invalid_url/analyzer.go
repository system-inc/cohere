// Package invalid_url is staticcheck's SA1007, a URL given as a constant to net/url.Parse that does not parse,
// ported onto staticcheck_call, the house's translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1007/sa1007.go, whose rule it keeps and
// whose messages, the URL and the url package's own error, it reports word for word over the same range, the URL
// argument's expression. It finds the URL's position in staticcheck's knowledge package, a table of standard library
// argument positions that the house ports in a later wave. The original keeps no state from one function to the next,
// nor does the port. The registry runs the port in the original's place, under its name, and the original stays only as
// the oracle: the parity tests beside this file run both over staticcheck's own fixture and the house's. The two differ
// nowhere: staticcheck_call departs from callcheck only on calls url.Parse is never part of, as
// testdata/src/differences records.
package invalid_url

import (
	"fmt"
	"go/constant"
	"net/url"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to net/url.Parse.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"net/url.Parse": checkUrl,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1007",
	"SA1007",
	"Flags a URL given as a constant to net/url.Parse that does not parse: staticcheck's SA1007, ported onto "+
		"staticcheck's intermediate representation.",
	&Listener,
)

// checkUrl marks the call's URL with the error parsing it gives, when the URL is a constant string.
func checkUrl(call *staticcheck_call.Call) {
	address := call.Arguments[knowledge.Arg("net/url.Parse.rawurl")]
	extracted := staticcheck_call.ExtractConstantOfKind(address.Value, constant.String)
	if extracted == nil {
		return
	}
	value := constant.StringVal(extracted.Value)
	if _, parseError := url.Parse(value); parseError != nil {
		address.Invalid(fmt.Sprintf("%q is not a valid URL: %s", value, parseError))
	}
}
