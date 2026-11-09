// Package inappropriate_context_key is staticcheck's SA1029, a key given to context.WithValue that another package
// could collide with, a built in type or an empty anonymous struct, or one that cannot be compared, ported onto
// staticcheck_call, the house's translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1029/sa1029.go, whose rule it keeps
// and whose messages it reports word for word over the same range, the key argument's expression. The registry runs the
// port in the original's place, under its name, and the original stays only as the oracle: the parity tests beside this
// file run both over staticcheck's own fixture and the house's. The two differ nowhere: staticcheck_call departs from
// callcheck only where it would panic, on calls context.WithValue is never part of, as testdata/src/differences
// records.
package inappropriate_context_key

import (
	"fmt"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to context.WithValue.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"context.WithValue": checkKey,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	FileLocal:                  true,
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1029",
	"SA1029",
	"Flags a key given to context.WithValue that is of a built in type, an empty anonymous struct, or a type that "+
		"cannot be compared: staticcheck's SA1029, ported onto the engine's own intermediate representation.",
	&Listener,
)

// checkKey marks the call's key, its second argument, as the kit hands it, the value before its conversion to the
// parameter's interface, so the key's own type is judged. A key of a built in type is marked, naming the alias it was
// written through, if any, and so is a key of an empty anonymous struct, or else of a type that cannot be compared.
func checkKey(call *staticcheck_call.Call) {
	key := call.Arguments[1]
	keyType := key.Value.Type()
	if basic, isBasic := types.Unalias(keyType).(*types.Basic); isBasic {
		if _, isAlias := keyType.(*types.Alias); isAlias {
			key.Invalid(fmt.Sprintf("should not use built-in type %s (via alias %s) as key for value; define your own "+
				"type to avoid collisions", basic, types.TypeString(keyType, types.RelativeTo(call.Context.Package))))
		} else {
			key.Invalid(fmt.Sprintf("should not use built-in type %s as key for value; define your own type to avoid "+
				"collisions", basic))
		}
	}
	// The struct is read without seeing through an alias, as the original reads it: an alias of struct{} is judged
	// as comparable, and so not marked.
	if structure, isStruct := keyType.(*types.Struct); isStruct && structure.NumFields() == 0 {
		key.Invalid("should not use empty anonymous struct as key for value; define your own type to avoid collisions")
	} else if !types.Comparable(keyType) {
		key.Invalid(fmt.Sprintf("keys used with context.WithValue must be comparable, but type %s is not comparable",
			keyType))
	}
}
