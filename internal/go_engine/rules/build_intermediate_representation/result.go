package build_intermediate_representation

import (
	"fmt"
	"iter"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// Result is one package in our intermediate representation, created and not built.
type Result struct {
	// Package is the pass's package. Its functions are built when first read, by Function.Build or through
	// SourceFunctions, never all at once.
	Package *intermediate_representation.Package
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

// SourceFunctions is every source function of the package, in the order buildir's SrcFuncs lists them: the
// package's functions and methods as declared, the synthetic package initializer first, then each one's anonymous
// functions, depth first, in the order they were declared in it. Each package function is built as it is read, which
// builds the anonymous functions it holds with it, so a reader that stops early leaves the rest unbuilt, and the
// anonymous functions, which only a build finds, come once every package function has been read.
//
// Under `--timing`, collector is the run's, and each build is measured, billed to Analyzer's row, since it is this
// pass's build done late, and taken out of the row of within, the analyzer whose action reads the functions, so a
// dispatcher's row is its walk alone (#3apgjer). A build a reader's own call reaches first, Function.Build or Origin
// called on a function read, is inside that call and billed with it. collector is nil otherwise, and each function is
// built as it always was.
func (result *Result) SourceFunctions(
	collector *timing.Collector,
	within string,
) iter.Seq[*intermediate_representation.Function] {
	return func(yield func(*intermediate_representation.Function) bool) {
		for _, function := range result.Package.Functions {
			if collector != nil {
				cpu := collector.Measure(function.Build)
				collector.Shared(Analyzer.Name, cpu)
				collector.Nested(within, cpu, 1)
			} else {
				function.Build()
			}
			if !yield(function) {
				return
			}
		}
		for _, function := range result.Package.Functions {
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
