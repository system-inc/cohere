package build_intermediate_representation

import (
	"fmt"
	"iter"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
)

// Result is one package in our intermediate representation for the readers that walk it, created and built whole by
// Analyzer before any reader starts, as buildir built its package before any check ran. A reader assumes that: one
// that follows StaticCallee into a function of the same package declared after the one it is reading finds that
// function's blocks, where a package built one function at a time, as each is read, would hand it a callee nobody had
// read yet, with none (staticcheck's SA2002 missed 3 of 21 findings that way, #dz6qbwm). Inside the workspace building
// per function bought nothing, since the readers walk every function. A callee in another package is created from
// types alone and has no blocks, as buildir left it. The fact passes read through this result too, on every package,
// as staticcheck's did through buildir. The run's fact resolver, which computes a fact one function at a time, only
// where asked (#w7zmvat), builds a dependency's functions in a program of its own, and a package this result is made
// for in this result's own (see Sharer), so a root is built once in a run, by whichever of the two reaches each
// function first (#wt5vaft).
//
// A package in which a function stands, an extra build's (#e4rd5dw), Analyzer creates and leaves unbuilt (see
// builtUnlessStanding). Package and SourceFunctions build it whole before they answer, so what a reader assumes above
// holds for every reader of either, and SourceFunctionsBut builds it less the functions its reader hears nothing of.
type Result struct {
	// irPackage is the pass's package, every function in it built, or, where a function of it stands, as built as
	// its readers have asked so far.
	irPackage *intermediate_representation.Package
}

// Package is the pass's package, every function in it built, which it builds first where Analyzer left that to the
// package's readers. Package.Build builds once, so on a package built already this costs reading that it is.
func (result *Result) Package() *intermediate_representation.Package {
	result.irPackage.Build()
	return result.irPackage
}

// ResultOf is Analyzer's result over pass's package, for an analyzer that requires Analyzer, or an error naming what
// pass held instead, never a nil that reads as a package with no functions.
func ResultOf(pass *analysis.Pass) (*Result, error) {
	result, isResult := pass.ResultOf[Analyzer].(*Result)
	if !isResult || result == nil {
		return nil, fmt.Errorf("reading %s's result: it is %T, where cohere's own intermediate representation "+
			"was expected", Analyzer.Name, pass.ResultOf[Analyzer])
	}
	return result, nil
}

// SourceFunctions is every source function of the package, built, in the order buildir's SrcFuncs lists them: the
// package's functions and methods as declared, the synthetic package initializer first, then each one's anonymous
// functions, depth first, in the order they were declared in it. Where Analyzer left the building to the package's
// readers it builds the package whole first, as Package does.
func (result *Result) SourceFunctions() iter.Seq[*intermediate_representation.Function] {
	result.irPackage.Build()
	return sourceFunctions(result.irPackage.Functions)
}

// SourceFunctionsBut is SourceFunctions less each declared function left holds for and the anonymous functions
// declared in one, in the same order, and it builds only what it lists: on a package Analyzer left unbuilt, a function
// left is never built by this, nor is an anonymous function inside one so much as created. It is for the reader that
// hears nothing of those functions, the dispatcher, where they stand for every listener it started (#e4rd5dw). The
// synthetic package initializer is a declared function to left as any other, and a reader that leaves it leaves every
// function literal of a package-level initializer with it.
//
// A function left may be built all the same, by the run's fact resolver or by a reader of the whole package beside
// this one, and then or later, so this reads nothing of one, its anonymous functions included.
func (result *Result) SourceFunctionsBut(
	left func(declared *intermediate_representation.Function) bool,
) iter.Seq[*intermediate_representation.Function] {
	kept := make([]*intermediate_representation.Function, 0, len(result.irPackage.Functions))
	for _, function := range result.irPackage.Functions {
		if !left(function) {
			kept = append(kept, function)
		}
	}
	buildEach(kept)
	return sourceFunctions(kept)
}

// sourceFunctions is declared, functions already built, then each one's anonymous functions, depth first, in the
// order they were declared in it.
func sourceFunctions(
	declared []*intermediate_representation.Function,
) iter.Seq[*intermediate_representation.Function] {
	return func(yield func(*intermediate_representation.Function) bool) {
		for _, function := range declared {
			if !yield(function) {
				return
			}
		}
		for _, function := range declared {
			if !yieldAnonymous(function, yield) {
				return
			}
		}
	}
}

// partBuildThreshold and partBuildWorkers are the representation's own parallelBuildThreshold and
// parallelBuildWorkers, which it measured for Package.Build (#s2z79xj) and does not export: how many functions a build
// needs before it spreads over several workers, and the most workers it takes. A package built in part is built on
// the same terms, so the package a cold run ends on builds no more serially for having a function that stands.
const (
	partBuildThreshold = 128
	partBuildWorkers   = 8
)

// buildEach builds each of functions, declared functions of one package, and returns once every one is built, as
// Package.Build builds a package's: one after another when they are few, and otherwise shared out among several
// workers as each frees up, so a long function holds up only the worker that took it. Function.Build builds a
// function once whoever asks, and may be called from many goroutines at once, so one the fact resolver or a build of
// the whole package reaches first is waited for, never built twice.
func buildEach(functions []*intermediate_representation.Function) {
	workers := min(runtime.GOMAXPROCS(0), partBuildWorkers, len(functions)/(partBuildThreshold/2))
	if len(functions) < partBuildThreshold || workers < 2 {
		for _, function := range functions {
			function.Build()
		}
		return
	}
	var next atomic.Int64
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for {
				index := int(next.Add(1)) - 1
				if index >= len(functions) {
					return
				}
				functions[index].Build()
			}
		})
	}
	group.Wait()
}

// yieldAnonymous yields function's anonymous functions, each followed by its own, depth first, and reports whether
// the reader wants more.
func yieldAnonymous(
	function *intermediate_representation.Function,
	yield func(*intermediate_representation.Function) bool,
) bool {
	for _, anonymous := range function.AnonymousFunctions {
		if !yield(anonymous) || !yieldAnonymous(anonymous, yield) {
			return false
		}
	}
	return true
}
