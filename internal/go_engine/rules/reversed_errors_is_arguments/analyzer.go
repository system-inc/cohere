// Package reversed_errors_is_arguments is staticcheck's SA1032, errors.Is given its target first, an error another
// package declares as a global, and the error it judges second, ported onto staticcheck_call, the house's
// translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1032/sa1032.go, whose rule it keeps
// and whose message it reports word for word over the same range, the call. The original stays the oracle: the
// parity tests beside this file run both over staticcheck's own fixture and the house's. The two differ nowhere:
// staticcheck_call departs from callcheck only where it would panic, on calls errors.Is is never part of, as
// testdata/src/differences records.
package reversed_errors_is_arguments

import (
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to errors.Is.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"errors.Is": checkOrder,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1032",
	"SA1032",
	"Flags errors.Is given a global error of another package first and the error it judges second: "+
		"staticcheck's SA1032, ported onto staticcheck's intermediate representation.",
	&Listener,
)

// checkOrder marks the call when its first argument, the error judged, is a global another package declares, the
// shape of a target. When the second argument is such a global too, neither is the error judged, since a package's
// external tests can ask whether one package's error is another's, so the call is not marked.
func checkOrder(call *staticcheck_call.Call) {
	// errors.Is takes two arguments and the kit hands every one, so this never returns; the original checks all the
	// same, and the translation keeps its logic so the two read alike.
	if len(call.Arguments) != 2 {
		return
	}
	judged := loadedGlobal(call.Arguments[0])
	if !isOfAnotherPackage(call, judged) {
		return
	}
	if target := loadedGlobal(call.Arguments[1]); isOfAnotherPackage(call, target) {
		return
	}
	call.Invalid("arguments have the wrong order")
}

// loadedGlobal is the global argument is read from, when its value is a load of a global, and nil otherwise. A
// global kept in a local before the call is a load all the same, but one that meets another value in a phi is not.
func loadedGlobal(argument *staticcheck_call.Argument) *ir.Global {
	load, isLoad := argument.Value.(*ir.Load)
	if !isLoad {
		return nil
	}
	global, _ := load.X.(*ir.Global)
	return global
}

// isOfAnotherPackage is whether global is a global of a package other than the one call is in. A global of no
// package, if the builder ever makes one, is not.
func isOfAnotherPackage(call *staticcheck_call.Call, global *ir.Global) bool {
	if global == nil {
		return false
	}
	globalPackage := global.Package().Pkg
	return globalPackage != nil && globalPackage.Path() != call.Context.Package.Path()
}
