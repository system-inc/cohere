package build_intermediate_representation

import (
	"fmt"
	"iter"
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// Result is one package in our intermediate representation for the readers that walk it, created by Analyzer and
// built whole on the first read of it, through Package or SourceFunctions, as buildir built its package before any
// check ran. A reader assumes that: one that follows StaticCallee into a function of the same package declared after
// the one it is reading finds that function's blocks, where a package built one function at a time, as each is
// read, would hand it a callee nobody had read yet, with none (staticcheck's SA2002 missed 3 of 21 findings that
// way, #dz6qbwm). Inside the workspace building per function bought nothing, since the readers walk every function.
// A callee in another package is created from types alone and has no blocks, as buildir left it. The fact passes
// read through this result too, on every package, as staticcheck's did through buildir. The run's fact resolver,
// which computes a fact one function at a time, only where asked (#w7zmvat), builds a dependency's functions in a
// program of its own, and a package this result is made for in this result's own (see Sharer), so a root is built
// once in a run, by whichever of the two reaches each function first (#wt5vaft).
type Result struct {
	// irPackage is the pass's package, unbuilt, or built in part by the resolver's questions, until the first read of
	// the result builds all of it.
	irPackage *intermediate_representation.Package
	// buildOnce builds irPackage on the first read. Package.Build is safe from many goroutines and waits on each
	// function's own build, so a reader on another goroutine finds every function built once it returns.
	buildOnce sync.Once
}

// Package is the pass's package, every function in it built.
func (result *Result) Package() *intermediate_representation.Package {
	result.buildOnce.Do(result.irPackage.Build)
	return result.irPackage
}

// ResultOf is Analyzer's result over pass's package, for an analyzer that requires Analyzer, or an error naming what
// pass held instead, never a nil that reads as a package with no functions.
func ResultOf(pass *analysis.Pass) (*Result, error) {
	result, isResult := pass.ResultOf[Analyzer].(*Result)
	if !isResult || result == nil {
		return nil, fmt.Errorf("reading %s's result: it is %T, where the engine's own intermediate representation "+
			"was expected", Analyzer.Name, pass.ResultOf[Analyzer])
	}
	return result, nil
}

// SourceFunctions is every source function of the package, built, in the order buildir's SrcFuncs lists them: the
// package's functions and methods as declared, the synthetic package initializer first, then each one's anonymous
// functions, depth first, in the order they were declared in it.
//
// Under `--timing`, collector is the run's and within is the analyzer whose action reads the functions. When this read
// is the one that builds the package, the build is measured, billed to Analyzer's row, since it is this pass's build
// done late, and taken out of within's, so a dispatcher's row is its walk alone (#3apgjer). A build another reader
// reached first is inside that reader's own measurement and billed with it, so each build is billed once. collector
// is nil otherwise, and the package is built as Package builds it.
func (result *Result) SourceFunctions(
	collector *timing.Collector,
	within string,
) iter.Seq[*intermediate_representation.Function] {
	return func(yield func(*intermediate_representation.Function) bool) {
		if collector != nil {
			result.buildOnce.Do(func() {
				cpu := collector.Measure(result.irPackage.Build)
				collector.Shared(Analyzer.Name, cpu)
				collector.Nested(within, cpu, 1)
			})
		}
		built := result.Package()
		for _, function := range built.Functions {
			if !yield(function) {
				return
			}
		}
		for _, function := range built.Functions {
			if !yieldAnonymous(function, yield) {
				return
			}
		}
	}
}

// SourceFunctionsReadBy is SourceFunctions read by pass's own analyzer, one that is no dispatcher, with the run's
// collector taken from pass under `--timing`, so the build it does is billed to Analyzer's row as a dispatcher's is.
func (result *Result) SourceFunctionsReadBy(pass *analysis.Pass) iter.Seq[*intermediate_representation.Function] {
	// Absent on a run that did not ask for --timing, where the nil collector measures nothing.
	collector, _ := pass.ResultOf[rule.Timed].(*timing.Collector)
	return result.SourceFunctions(collector, pass.Analyzer.Name)
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
