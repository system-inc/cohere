// Package unobserved_append is staticcheck's SA4010, a call to append whose result nothing reads but other appends,
// extending a slice no one else can see, ported onto the engine's own intermediate representation.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4010/sa4010.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the call, where staticcheck's report puts it.
// The original walks every source function of a package in one run, but keeps nothing from one function to the next:
// its walk over an append's readers and its two judgments of the slice the append extends each start afresh for
// every append. So the port judges one function at a time and finds what the original finds. The original ranges over
// a map of the values its first judgment reached, in an order Go leaves to chance; the port ranges over them in the
// order reached, and the outcome never depended on the order, since every reader is judged the first time any walk
// reaches it and any reader judged wrong ends the judgment. The registry runs the port in the original's place, under
// its name, and the original stays only as the oracle: the parity tests beside this file run both over the same
// packages, and they differ nowhere. The one place the port could differ is a call with no syntax, where staticcheck's
// report panics and the port reports nothing; staticcheck builds every append from its call expression, so no such call
// reaches either.
package unobserved_append

import (
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher over the engine's own intermediate
// representation: every function, each judged on its own.
var Listener = rule.Listener{
	FileLocal: true, IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check}}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4010",
	"SA4010",
	"Flags a call to append whose result is never observed anywhere: staticcheck's SA4010, ported onto the "+
		"engine's own intermediate representation.",
	&Listener,
)

// message is the original's, word for word.
const message = "this result of append is never used, except maybe in other appends"

// Check judges one function: each call to append whose result nothing reads but phis and other appends, reported when
// the slice it extends is one the function made itself and nothing else reads, so no other slice can share the
// backing array the append writes into.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			call, isCall := instruction.(*intermediate_representation.Call)
			if !isCall || !isAppend(call) || isObserved(call) {
				continue
			}
			origins, isMadeHere := originsOf(call.Call.Arguments[0])
			if !isMadeHere || !isUnshared(call, origins) {
				continue
			}
			source := call.Source()
			if source == nil {
				// A call with no syntax has no place to report; staticcheck's report panics on one.
				continue
			}
			context.Report(analysis.Diagnostic{Pos: source.Pos(), End: source.End(), Message: message})
		}
	}
}

// isAppend is whether value is a call of the builtin append.
func isAppend(value intermediate_representation.Value) bool {
	call, isCall := value.(*intermediate_representation.Call)
	if !isCall || call.Call.IsInvoke() {
		return false
	}
	builtin, isBuiltin := call.Call.Value.(*intermediate_representation.Builtin)
	return isBuiltin && builtin.Name() == "append"
}

// isObserved is whether anything reads the result of call but a phi or another append, followed through the phis and
// appends that read it.
func isObserved(call *intermediate_representation.Call) bool {
	visited := map[intermediate_representation.Instruction]bool{}
	var readersObserve func(value intermediate_representation.Value) bool
	readersObserve = func(value intermediate_representation.Value) bool {
		referrers := value.Referrers()
		if referrers == nil {
			return false
		}
		for _, referrer := range *referrers {
			if visited[referrer] {
				continue
			}
			visited[referrer] = true
			switch referrer := referrer.(type) {
			case *intermediate_representation.Phi:
				if readersObserve(referrer) {
					return true
				}
			case intermediate_representation.Value:
				if !isAppend(referrer) || readersObserve(referrer) {
					return true
				}
			default:
				// An instruction that is no value, a store, a send, a return, a `_ =`, uses the result.
				return true
			}
		}
		return false
	}
	return readersObserve(call)
}

// originsOf is the values the slice argument of an append was built from, in the order reached, and whether the
// function made every one of them itself: a nil constant, a make, an allocation, or a phi, a slicing or another append
// of such a slice. Anything else, a parameter, a load, the result of a call, may share its backing array with a slice
// the function cannot see, and the append's write into it may be observed there.
func originsOf(argument intermediate_representation.Value) ([]intermediate_representation.Value, bool) {
	reached := map[intermediate_representation.Value]bool{}
	origins := []intermediate_representation.Value{}
	var madeHere func(value intermediate_representation.Value) bool
	madeHere = func(value intermediate_representation.Value) bool {
		if reached[value] {
			// A phi in a loop reaches itself, and is judged where it was first reached.
			return true
		}
		reached[value] = true
		origins = append(origins, value)
		switch value := value.(type) {
		case *intermediate_representation.Phi:
			for _, edge := range value.Edges {
				if !madeHere(edge) {
					return false
				}
			}
			return true
		case *intermediate_representation.Slice:
			return madeHere(value.X)
		case *intermediate_representation.Constant,
			*intermediate_representation.MakeSlice,
			*intermediate_representation.Allocation:
			return true
		case *intermediate_representation.Call:
			return isAppend(value) && madeHere(value.Call.Arguments[0])
		}
		return false
	}
	isMadeHere := madeHere(argument)
	return origins, isMadeHere
}

// isUnshared is whether nothing reads the origins of call's slice but call, the origins themselves, and phis, slicings,
// makes and allocations read the same way. A reader of any other kind, a call, a store, a load through an allocation,
// could keep or read the backing array the append writes into. Another append is such a reader too, unless it is
// among the origins.
func isUnshared(call *intermediate_representation.Call, origins []intermediate_representation.Value) bool {
	judged := map[intermediate_representation.Instruction]bool{call: true}
	for _, origin := range origins {
		// A constant is no instruction and has no readers to judge.
		if instruction, isInstruction := origin.(intermediate_representation.Instruction); isInstruction {
			judged[instruction] = true
		}
	}
	var readersUnshared func(value intermediate_representation.Value) bool
	readersUnshared = func(value intermediate_representation.Value) bool {
		referrers := value.Referrers()
		if referrers == nil {
			return true
		}
		for _, referrer := range *referrers {
			if judged[referrer] {
				continue
			}
			judged[referrer] = true
			switch referrer.(type) {
			case *intermediate_representation.Phi,
				*intermediate_representation.Slice,
				*intermediate_representation.MakeSlice,
				*intermediate_representation.Allocation:
			default:
				return false
			}
			// Each kind the switch lets through is a value, whose own readers are judged the same way.
			readerValue, isValue := referrer.(intermediate_representation.Value)
			if isValue && !readersUnshared(readerValue) {
				return false
			}
		}
		return true
	}
	for _, origin := range origins {
		if !readersUnshared(origin) {
			return false
		}
	}
	return true
}
