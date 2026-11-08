// Package invalid_listen_address is staticcheck's SA1020, a constant address given to net/http.ListenAndServe or
// ListenAndServeTLS whose port is neither a number a port can be nor a service name, ported onto staticcheck_call,
// the house's translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1020/sa1020.go, whose rules it keeps,
// one for each of the two functions, and whose message it reports word for word over the same range, the address
// argument's expression. The original keeps no state from one function to the next, nor does the port. Like the
// original it judges the port alone and never the host, and it reads a service name more strictly than the network
// stack does: a name with a hyphen anywhere, which IANA allows inside a name, is not one. That is the original's
// false positive, kept for parity, and parity's serviceNames shows it. The original stays the oracle: the parity
// tests beside this file run both over staticcheck's own fixture and the house's. The two differ nowhere:
// staticcheck_call departs from callcheck only on calls the two functions are never part of, as
// testdata/src/differences records.
package invalid_listen_address

import (
	"go/constant"
	"net"
	"strconv"
	"strings"

	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to net/http.ListenAndServe or ListenAndServeTLS.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"net/http.ListenAndServe":    checkAddress(0),
	"net/http.ListenAndServeTLS": checkAddress(0),
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1020",
	"SA1020",
	"Flags a constant address given to net/http.ListenAndServe whose port is neither a port number nor a service "+
		"name: staticcheck's SA1020, ported onto staticcheck's intermediate representation.",
	&Listener,
)

// checkAddress is the rule that marks the argument at index, the address the call listens on, when it is a constant
// whose port is invalid.
func checkAddress(index int) staticcheck_call.Rule {
	return func(call *staticcheck_call.Call) {
		address := call.Arguments[index]
		if !isValidAddress(address.Value) {
			address.Invalid("invalid port or service name in host:port pair")
		}
	}
}

// isValidAddress is false only for a constant string that is not empty, which the server reads as :http, and that
// either does not split into a host and a port or splits into a port that is invalid. Any other value may be valid.
func isValidAddress(value ir.Value) bool {
	extracted := staticcheck_call.ExtractConstantOfKind(value, constant.String)
	if extracted == nil {
		return true
	}
	address := constant.StringVal(extracted.Value)
	if address == "" {
		return true
	}
	_, port, splitError := net.SplitHostPort(address)
	if splitError != nil {
		return false
	}
	return isValidPort(port)
}

// isValidPort is whether port is a decimal number from 0 to 65535, or, when it is not a number at all, a service name.
func isValidPort(port string) bool {
	number, parseError := strconv.ParseInt(port, 10, 64)
	if parseError != nil {
		return isValidServiceName(port)
	}
	return number >= 0 && number <= 65535
}

// isValidServiceName is whether name is from 1 to 15 characters, letters and digits only, with a letter among them.
// The checks for a hyphen at either end and for two in a row are the original's, though a name that passes them is
// still refused below for any hyphen it holds.
func isValidServiceName(name string) bool {
	if len(name) < 1 || len(name) > 15 {
		return false
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	if strings.Contains(name, "--") {
		return false
	}
	hasLetter := false
	for _, character := range name {
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') {
			hasLetter = true
			continue
		}
		if character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return hasLetter
}
