// Package test_failure_in_goroutine is staticcheck's SA2002, a goroutine that calls a method of testing's T, B or F
// that stops the test, FailNow, SkipNow or one of the methods built on them, which must be called in the test's own
// goroutine, ported onto the engine's own intermediate representation (#tf4cb8g).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa2002/sa2002.go, whose logic it keeps and
// whose messages it reports word for word, from the start to the end of the go statement, where staticcheck's report
// puts it, with one related entry at the call that stops the test. The original walks every source function of a
// package in one run, but keeps nothing from one function to the next: for each go statement it reads the body of the
// function the goroutine starts, one level deep, afresh. That body is another function of the same intermediate
// representation, read the same from any function that starts it, so the port judges one function at a time and finds
// what the original finds. The registry runs the port in the original's place, under its name, and the original stays
// only as the oracle: the parity tests beside this file run both over the same packages, related entries included, and
// they differ nowhere. The original reports through staticcheck's report with its Related option; the port sets the one
// related entry itself until the shared report kit (#wtvf02h) carries it. A call with no syntax does reach both: a
// goroutine started from a method value runs the wrapper staticcheck builds for it, whose call of the method has none,
// and both report that finding without its related entry. The one place the port could differ is a go statement with no
// syntax, where staticcheck's report panics and the port reports nothing; staticcheck builds every go statement from
// its statement, so no such one reaches either.
package test_failure_in_goroutine

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	FileLocal:                  true,
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA2002",
	"SA2002",
	"Flags a goroutine that calls testing.T.FailNow, SkipNow or a method built on them, which must be called in the "+
		"test's own goroutine: staticcheck's SA2002, ported onto the engine's own intermediate representation.",
	&Listener,
)

// stoppingMethods is the names of the methods of testing's common type, which T, B and F embed, that stop the test's
// goroutine, the original's list word for word.
var stoppingMethods = map[string]bool{
	"FailNow": true, "Fatal": true, "Fatalf": true, "SkipNow": true, "Skip": true, "Skipf": true,
}

// Check judges one function: each go statement in it that starts a function with a body, named or literal, whose own
// instructions call a method that stops the test, reported once per such call, at the go statement, with the call as
// its related entry. Calls in functions that body calls in turn are not followed, as in the original.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			goStatement, isGo := instruction.(*intermediate_representation.Go)
			if !isGo {
				continue
			}
			started := startedFunction(goStatement)
			if started == nil || started.Blocks == nil {
				// A function called through an interface or a value has no body to read here, nor has one declared
				// in another package, whose body staticcheck does not build.
				continue
			}
			for _, startedBlock := range started.Blocks {
				for _, startedInstruction := range startedBlock.Instructions {
					call, isCall := startedInstruction.(*intermediate_representation.Call)
					if !isCall {
						continue
					}
					name, isStopping := stoppingMethodName(call)
					if !isStopping {
						continue
					}
					report(context, goStatement, call, name)
				}
			}
		}
	}
}

// startedFunction is the function goStatement starts, when it is a function or a closure of one, and nil otherwise.
func startedFunction(goStatement *intermediate_representation.Go) *intermediate_representation.Function {
	switch value := goStatement.Call.Value.(type) {
	case *intermediate_representation.Function:
		return value
	case *intermediate_representation.MakeClosure:
		// A closure's function is always a *Function; the original asserts as much.
		closed, isFunction := value.Function.(*intermediate_representation.Function)
		if !isFunction {
			return nil
		}
		return closed
	}
	return nil
}

// stoppingMethodName is the name of the method call statically calls, and whether it is one of testing's common
// type that stops the test.
func stoppingMethodName(call *intermediate_representation.Call) (string, bool) {
	if call.Call.IsInvoke() {
		return "", false
	}
	callee := call.Call.StaticCallee()
	if callee == nil {
		return "", false
	}
	receiver := callee.Signature.Recv()
	if receiver == nil || !typeutil.IsPointerToTypeWithName(receiver.Type(), "testing.common") {
		return "", false
	}
	method, isMethod := callee.Object().(*types.Func)
	if !isMethod || !stoppingMethods[method.Name()] {
		return "", false
	}
	return method.Name(), true
}

// report reports goStatement, whose goroutine stops the test by call to the method name, with call as its related
// entry, as staticcheck's report with its Related option does.
func report(
	context *rule.Context,
	goStatement *intermediate_representation.Go,
	call *intermediate_representation.Call,
	name string,
) {
	source := goStatement.Source()
	if source == nil {
		// A go statement with no syntax has no place to report; staticcheck's report panics on one.
		return
	}
	diagnostic := analysis.Diagnostic{
		Pos: source.Pos(),
		End: source.End(),
		Message: fmt.Sprintf("the goroutine calls T.%s, which must be called in the same goroutine as the test",
			name),
	}
	if callSource := call.Source(); callSource != nil {
		// Staticcheck's Related option leaves out an entry with no syntax and reports the finding without it.
		diagnostic.Related = []analysis.RelatedInformation{{
			Pos:     callSource.Pos(),
			End:     callSource.End(),
			Message: fmt.Sprintf("call to T.%s", name),
		}}
	}
	context.Report(diagnostic)
}
