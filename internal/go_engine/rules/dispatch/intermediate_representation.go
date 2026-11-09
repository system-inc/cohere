package dispatch

import (
	"reflect"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// IntermediateRepresentation is the walk over the engine's own intermediate representation, which every reader that
// walked staticcheck's has moved onto (#g0rwdw1, #kk4864m). It calls the listeners registered on it that a package's
// pass starts (see start) over one walk of the package's source functions, in the order buildir's SrcFuncs lists
// them: for each function every listener's Function, then each instruction, in block order, to every listener whose
// Instructions list its kind, then every FunctionExit; and once the functions are walked, every PackageExit.
// Listeners are called in the order they were registered.
//
// The package is built whole as the walk starts, and a pass that starts no listener reads none of it, so builds none.
type IntermediateRepresentation struct {
	*listeners
}

// NewIntermediateRepresentation is a dispatcher over the engine's own intermediate representation with no rule
// registered on it. Its analyzer requires build_intermediate_representation, and runs only where that does, on a
// package without errors.
func NewIntermediateRepresentation() *IntermediateRepresentation {
	dispatcher := &IntermediateRepresentation{}
	dispatcher.listeners = newListeners(
		"dispatch_intermediate_representation",
		"walks each package's functions in the engine's own intermediate representation once, calling every "+
			"listener registered on it, for the rule analyzers that report what each found",
		[]*analysis.Analyzer{build_intermediate_representation.Analyzer},
		false,
		dispatcher.run,
	)
	return dispatcher
}

// RuleAnalyzer registers listener, which must hear the engine's own intermediate representation's walk, as the rule
// named ruleName, and returns the rule's own analyzer, named analyzerName and documented by doc, which requires this
// dispatcher. A listener that reads Function.ValueForExpression has its rule's analyzer require the representation
// and its ExpressionReader mark as well, so the package records the expression map wherever the rule runs (see
// build_intermediate_representation.ExpressionReader).
func (dispatcher *IntermediateRepresentation) RuleAnalyzer(
	ruleName string,
	analyzerName string,
	doc string,
	listener *rule.Listener,
) *analysis.Analyzer {
	if !listener.IsIntermediateRepresentation() {
		// Invariant: the shared RuleAnalyzer hands every listener that is not syntax here, and a listener of neither
		// walk is one Validate refuses, so any here is a mistake in building the registry, never in a package.
		panic("rule " + ruleName + " registers a listener that does not hear the engine's own intermediate " +
			"representation on the dispatcher over it, which would never call it")
	}
	kinds := []reflect.Type{}
	for _, kind := range listener.IntermediateRepresentation.Instructions {
		kinds = append(kinds, reflect.TypeOf(kind))
	}
	analyzer := dispatcher.ruleAnalyzer(ruleName, analyzerName, doc, listener, kinds)
	if listener.IntermediateRepresentation.ReadsValueForExpression {
		analyzer.Requires = append(analyzer.Requires, build_intermediate_representation.Analyzer,
			build_intermediate_representation.ExpressionReader)
	}
	return analyzer
}

// run walks the package's source functions once, calling every started listener, each with its own context and its
// own recover, and returns each started rule's outcome.
func (dispatcher *IntermediateRepresentation) run(pass *analysis.Pass) (any, error) {
	// Started first, which refuses a pass whose representation failed, so that failure is named as one.
	started, startError := dispatcher.start(pass)
	if startError != nil {
		return nil, startError
	}
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	// With no started listener the functions are not read, so the package is not built.
	if len(started.runs) == 0 {
		return finish(started), nil
	}
	// Under --timing each function's build is billed to the representation's build, not to this walk.
	for function := range built.SourceFunctions(started.timing, started.dispatcher) {
		for _, run := range started.runs {
			run.function(function)
		}
		// With no started listener hearing an instruction, the blocks are not walked at all.
		if len(started.byKind) > 0 {
			for _, block := range function.Blocks {
				for _, instruction := range block.Instructions {
					for _, run := range started.byKind[reflect.TypeOf(instruction)] {
						run.instruction(instruction)
					}
				}
			}
		}
		for _, run := range started.runs {
			run.functionExit(function)
		}
	}
	return finish(started), nil
}
