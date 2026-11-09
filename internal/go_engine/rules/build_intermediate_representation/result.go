package build_intermediate_representation

import (
	"fmt"
	"iter"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
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
type Result struct {
	// irPackage is the pass's package, every function in it built.
	irPackage *intermediate_representation.Package
}

// Package is the pass's package, every function in it built.
func (result *Result) Package() *intermediate_representation.Package {
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
func (result *Result) SourceFunctions() iter.Seq[*intermediate_representation.Function] {
	return func(yield func(*intermediate_representation.Function) bool) {
		for _, function := range result.irPackage.Functions {
			if !yield(function) {
				return
			}
		}
		for _, function := range result.irPackage.Functions {
			if !yieldAnonymous(function, yield) {
				return
			}
		}
	}
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
