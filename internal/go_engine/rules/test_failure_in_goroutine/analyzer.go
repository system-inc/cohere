// Package test_failure_in_goroutine is staticcheck's SA2002, a goroutine that calls a method of testing's T, B or F
// that stops the test, FailNow, SkipNow or one of the methods built on them, which must be called in the test's own
// goroutine, ported onto the engine's own intermediate representation (#tf4cb8g).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa2002/sa2002.go, whose logic it keeps and
// whose messages it reports word for word, from the start to the end of the go statement, where staticcheck's report
// puts it, with one related entry at the call that stops the test. The original walks every source function of a
// package in one run, but keeps nothing from one function to the next: for each go statement it reads the body of the
// function the goroutine starts, one level deep, afresh. The port judges one function at a time and finds what the
// original finds, reading a started function by where it is declared (#h0k644s). A function literal or a closure of
// the function judged is part of that function's declaration and built inside its build. A wrapper its body makes, as
// a method value's, belongs to no declaration, and is built by whichever builder ran that body, in a later turn, or by
// the first reader that calls Build on it. The port reads the blocks of both, and calls Build on each first, so a
// wrapper is built by the time it is read whoever built the function judged. A function or method the package declares
// is another declaration, so the port asks the run's fact resolver which of its calls stop the test
// (StoppingCallsAnalyzer, in result.go), which builds that function when first asked and keeps the answer. Judging a
// function so needs no other declaration built before it, and what a finding rests on in another declaration's body is
// an answer the engine holds. A function another package declares is neither read nor asked about, since the original
// builds no body for it.
//
// The registry runs the port in the original's place, under its name, and the original stays only as the oracle: the
// parity tests beside this file run both over the same packages, related entries included, and they differ nowhere.
// The original reports through staticcheck's report with its Related option; the port sets the one related entry
// itself until the shared report kit (#wtvf02h) carries it. A call with no syntax does reach both: a goroutine started
// from a method value runs the wrapper staticcheck builds for it, whose call of the method has none, and both report
// that finding without its related entry. The one place the port could differ is a go statement with no syntax, where
// staticcheck's report panics and the port reports nothing; staticcheck builds every go statement from its statement,
// so no such one reaches either.
package test_failure_in_goroutine

import (
	"fmt"
	"go/token"
	"go/types"
	"iter"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own. It
// requires StoppingCallsAnalyzer, whose result it asks of the functions the package declares.
var Listener = rule.Listener{
	FileLocal:                  true,
	Requires:                   []*analysis.Analyzer{StoppingCallsAnalyzer},
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
//
// It reads the blocks of function, of the literals and closures of its declaration, and of the wrappers its body makes,
// each of those built by its own Build before it is read, and no other declaration's: the stopping calls of a function
// or method the package declares are the fact resolver's answer about it.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			goStatement, isGo := instruction.(*intermediate_representation.Go)
			if !isGo {
				continue
			}
			started := startedFunction(goStatement)
			if started == nil {
				// A function called through an interface or a value has no body to read here.
				continue
			}
			if started.Parent() != nil || started.Package == nil {
				// A function literal or a closure of function's declaration, or a function of no package that
				// function's body makes, as the wrapper of a method value is. Whoever reads a function's blocks
				// calls Build on it first: a literal's Build finds its enclosing function built and returns, and a
				// wrapper's builds it or waits for the builder building it, since a Build of function can return
				// to a second caller before the first builder has built the wrappers function's body queued.
				//
				// It cannot deadlock. Check runs outside every build and holds no lock a build takes. Build waits
				// for the buildOnce of the function it builds, held by a goroutine running that function's body,
				// and a body never waits for another function's buildOnce. Then it makes the builder's closing wait
				// for shared functions, after marking its own task done, the wait every lazy Build makes.
				started.Build()
				for call, name := range stoppingCalls(started) {
					report(context, goStatement, name, relatedAtCall(call, name))
				}
				continue
			}
			if started.Package != function.Package {
				// A function declared in another package, whose body staticcheck does not build: never read, and
				// never asked about.
				continue
			}
			declared, isDeclared := started.Object().(*types.Func)
			if !isDeclared {
				// The package's synthetic initializer, which has no object and which no go statement can name.
				continue
			}
			for _, stopping := range askedBy(context, function).StoppingCalls(declared) {
				report(context, goStatement, stopping.Method, relatedAtPlace(context, stopping))
			}
		}
	}
}

// askedBy is the package's stopping calls as context's rule asks them while it checks function,
// StoppingCallsAnalyzer's result.
func askedBy(context *rule.Context, function *intermediate_representation.Function) *Result {
	result, isResult := context.ResultOf(StoppingCallsAnalyzer).(*Result)
	if !isResult || result == nil {
		// Invariant: the listener requires StoppingCallsAnalyzer, and the dispatcher hands over its result, of the
		// type it declares.
		panic("test_failure_in_goroutine: the stopping calls' result is missing")
	}
	return result.Asking(context.RuleName()).For(function)
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

// stoppingCalls is each call among function's own instructions to a method that stops the test, with the method's
// name, in instruction order. A function with no blocks, declared without a body, has none.
func stoppingCalls(
	function *intermediate_representation.Function,
) iter.Seq2[*intermediate_representation.Call, string] {
	return func(yield func(*intermediate_representation.Call, string) bool) {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instructions {
				call, isCall := instruction.(*intermediate_representation.Call)
				if !isCall {
					continue
				}
				name, isStopping := stoppingMethodName(call)
				if !isStopping {
					continue
				}
				if !yield(call, name) {
					return
				}
			}
		}
	}
}

// relatedAtCall is the related entry of a finding at call, a call to the stopping method name read from its blocks:
// none for a call with no syntax, which staticcheck's Related option leaves out, reporting the finding without it.
func relatedAtCall(call *intermediate_representation.Call, name string) []analysis.RelatedInformation {
	source := call.Source()
	if source == nil {
		return nil
	}
	return relatedEntry(name, source.Pos(), source.End())
}

// relatedAtPlace is the related entry of a finding at stopping, a call the fact resolver answered with, placed in
// the file of context's package it names: none for a call with no syntax, as relatedAtCall has none.
func relatedAtPlace(context *rule.Context, stopping StoppingCall) []analysis.RelatedInformation {
	if !stopping.HasSyntax {
		return nil
	}
	for _, file := range context.Files {
		tokenFile := context.FileSet.File(file.FileStart)
		if tokenFile != nil && tokenFile.Name() == stopping.File {
			return relatedEntry(stopping.Method, tokenFile.Pos(stopping.Start), tokenFile.Pos(stopping.End))
		}
	}
	// Invariant: the answer is about a function the package declares, whose syntax is in one of the package's files,
	// and an answer a store gives back was kept under a key holding each file's path, so a file the package does not
	// hold is a mistake in whoever held the answer, never a finding to report without its entry.
	panic(fmt.Sprintf("test_failure_in_goroutine: the call to T.%s is placed in %s, which is no file of %s",
		stopping.Method, stopping.File, context.Package.Path()))
}

// relatedEntry is the one related entry of a finding, from start to end of the call to the stopping method name.
func relatedEntry(name string, start token.Pos, end token.Pos) []analysis.RelatedInformation {
	return []analysis.RelatedInformation{{Pos: start, End: end, Message: fmt.Sprintf("call to T.%s", name)}}
}

// report reports goStatement, whose goroutine stops the test by a call to the method name, with related, the entry at
// that call or none, as staticcheck's report with its Related option does.
func report(
	context *rule.Context,
	goStatement *intermediate_representation.Go,
	name string,
	related []analysis.RelatedInformation,
) {
	source := goStatement.Source()
	if source == nil {
		// A go statement with no syntax has no place to report; staticcheck's report panics on one.
		return
	}
	context.Report(analysis.Diagnostic{
		Pos: source.Pos(),
		End: source.End(),
		Message: fmt.Sprintf("the goroutine calls T.%s, which must be called in the same goroutine as the test",
			name),
		Related: related,
	})
}
