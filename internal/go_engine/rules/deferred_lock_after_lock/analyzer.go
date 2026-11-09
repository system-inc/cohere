// Package deferred_lock_after_lock is staticcheck's SA2003, a mutex locked and then, in the very next instruction,
// locked again by a deferred call where unlocking it was meant, ported onto the engine's own intermediate
// representation (#tf4cb8g).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa2003/sa2003.go, whose logic it keeps and
// whose messages it reports word for word, from the start to the end of the defer statement, where staticcheck's
// report puts it. The original walks every source function of a package in one run, but keeps nothing from one
// function to the next, nor from one block to the next: it judges each pair of neighbouring instructions alone. So the
// port judges one function at a time and finds what the original finds. The registry runs the port in the original's
// place, under its name, and the original stays only as the oracle: the parity tests beside this file run both over the
// same packages, and they differ nowhere. The original names the method in its message by the short name of the first
// call, looked up again after its match, where the port reads both names from the full name it matched, one of only
// two, so the words are the same. The one place the port could differ is a defer with no syntax, where staticcheck's
// report panics and the port reports nothing; staticcheck builds every defer from its statement, so no such defer
// reaches either.
package deferred_lock_after_lock

import (
	"fmt"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA2003",
	"SA2003",
	"Flags a deferred call to Lock right after locking, where Unlock was likely meant: staticcheck's SA2003, "+
		"ported onto the engine's own intermediate representation.",
	&Listener,
)

// lockMethod is one locking method the check knows, by the two names its message reads.
type lockMethod struct {
	// name is the method's own name.
	name string
	// release is the name of the method that releases what it takes.
	release string
}

// lockMethods is the locking methods the check knows, by the full name CallName, irutil's translated, gives a static
// call to each. The original's list is these two alone, so a write lock of a sync.RWMutex deferred after taking it is
// not found.
var lockMethods = map[string]lockMethod{
	"(*sync.Mutex).Lock":    {name: "Lock", release: "Unlock"},
	"(*sync.RWMutex).RLock": {name: "RLock", release: "RUnlock"},
}

// Check judges one function: each static call to a locking method followed, as the next instruction of its block, by
// a defer of a locking method on the very same receiver, the one value both calls read, reported at the defer.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		// The last instruction of a block has no next one to be the defer.
		for index := 0; index+1 < len(block.Instructions); index++ {
			call, isCall := block.Instructions[index].(*intermediate_representation.Call)
			if !isCall {
				continue
			}
			locked, isLock := lockMethods[intermediate_representation.CallName(call.Common())]
			if !isLock {
				continue
			}
			deferred, isDefer := block.Instructions[index+1].(*intermediate_representation.Defer)
			if !isDefer {
				continue
			}
			if _, isDeferredLock := lockMethods[intermediate_representation.CallName(&deferred.Call)]; !isDeferredLock {
				continue
			}
			// A static call to a method takes its receiver first. The same value is the same mutex: a global, a local
			// or a parameter. A field read twice is two values, so a mutex in a struct is never found, as in the
			// original.
			if call.Call.Arguments[0] != deferred.Call.Arguments[0] {
				continue
			}
			source := deferred.Source()
			if source == nil {
				// A defer with no syntax has no place to report; staticcheck's report panics on one.
				continue
			}
			context.Report(analysis.Diagnostic{
				Pos: source.Pos(),
				End: source.End(),
				Message: fmt.Sprintf("deferring %s right after having locked already; did you mean to defer %s?",
					locked.name, locked.release),
			})
		}
	}
}
