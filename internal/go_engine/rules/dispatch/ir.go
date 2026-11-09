// Package dispatch walks each package once for every dispatched rule, where each rule's analyzer would otherwise walk
// it on its own. A rule registers a rule.Listener through RuleAnalyzer, which returns an analyzer of the rule's own
// whose run reports what the dispatcher found under the rule's name, or returns the rule's crash. So the engine's
// handling of a rule, its settings, suppression, crash accounting, findings cache and rulesRun, reads a dispatched
// rule as any other.
//
// There are three dispatchers, one per walk: Ir over staticcheck's intermediate representation,
// IntermediateRepresentation over the engine's own, and Syntax over the syntax tree. A listener hears one of them,
// never two, and the process shares one of each, so every rule's analyzer requires the one dispatcher that holds its
// listener.
package dispatch

import (
	"reflect"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Ir calls the listeners registered on it that a package's pass starts (see start) over one walk of the package's
// functions in staticcheck's intermediate representation: for each function every listener's Function, then each
// instruction, in block order, to every listener whose Instructions list its kind, then every FunctionExit; and once
// the functions are walked, every PackageExit. Listeners are called in the order they were registered.
type Ir struct {
	*listeners
}

// NewIr is a dispatcher over the intermediate representation with no rule registered on it. Its analyzer requires
// buildir, and runs only where buildir does, on a package without errors.
func NewIr() *Ir {
	dispatcher := &Ir{}
	dispatcher.listeners = newListeners(
		"dispatch_ir",
		"walks each package's functions in staticcheck's intermediate representation once, calling every listener "+
			"registered on it, for the rule analyzers that report what each found",
		[]*analysis.Analyzer{staticcheck_ir.Analyzer},
		false,
		dispatcher.run,
	)
	return dispatcher
}

// RuleAnalyzer registers listener, which must hear the intermediate representation's walk, as the rule named
// ruleName, and returns the rule's own analyzer, named analyzerName and documented by doc, which requires this
// dispatcher.
func (dispatcher *Ir) RuleAnalyzer(
	ruleName string,
	analyzerName string,
	doc string,
	listener *rule.Listener,
) *analysis.Analyzer {
	if listener.IsSyntax() || listener.IsIntermediateRepresentation() {
		// Invariant: RuleAnalyzer hands a syntax listener to Syntax and one of the engine's own intermediate
		// representation to IntermediateRepresentation, and Validate refuses one that declares two walks, so either
		// here is a mistake in building the registry, never in a package.
		panic("rule " + ruleName + " registers a listener of another walk on the dispatcher over staticcheck's " +
			"intermediate representation, which would never call it")
	}
	kinds := []reflect.Type{}
	for _, kind := range listener.Instructions {
		kinds = append(kinds, reflect.TypeOf(kind))
	}
	return dispatcher.ruleAnalyzer(ruleName, analyzerName, doc, listener, kinds)
}

// run walks the package's functions once, calling every started listener, each with its own context and its own
// recover, and returns each started rule's outcome.
func (dispatcher *Ir) run(pass *analysis.Pass) (any, error) {
	// Started first, which refuses a pass whose intermediate representation failed, so that failure is named as one.
	started, startError := dispatcher.start(pass)
	if startError != nil {
		return nil, startError
	}
	functions, readError := staticcheck_ir.Functions(pass)
	if readError != nil {
		return nil, readError
	}
	for _, function := range functions {
		for _, run := range started.runs {
			run.function(function)
		}
		// With no started listener hearing an instruction, the blocks are not walked at all.
		if len(started.byKind) > 0 {
			for _, block := range function.Blocks {
				for _, instruction := range block.Instrs {
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
