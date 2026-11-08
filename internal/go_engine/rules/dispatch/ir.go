// Package dispatch walks each package once for every dispatched rule, where each rule's analyzer would otherwise walk
// it on its own. A rule registers a rule.Listener on a dispatcher and keeps an analyzer of its own, built by
// RuleAnalyzer, whose run reports what the dispatcher found under the rule's name, or returns the rule's crash. So
// the engine's handling of a rule, its settings, suppression, crash accounting, findings cache and rulesRun, reads a
// dispatched rule as any other.
//
// Ir is the dispatcher over staticcheck's intermediate representation. There is no syntax dispatcher yet, so a
// listener's Nodes, Node, FileEnter and FileExit are never called, and the registry refuses a listener that sets them,
// and one with PackageExit alone, which no walk gathers for.
package dispatch

import (
	"fmt"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Ir calls the listeners registered on it over one walk of each package's functions in staticcheck's intermediate
// representation: for each function every listener's Function, then each instruction, in block order, to every
// listener whose Instructions list its kind, then every FunctionExit; and once the functions are walked, every
// PackageExit. Listeners are called in the order they were registered.
//
// Every rule is registered before the first run, since the checker reads Analyzer's requirements as a run starts,
// and a run reads the registered rules without a lock.
type Ir struct {
	// Analyzer is the dispatcher's own analyzer, which every rule analyzer built on this dispatcher requires. It
	// requires staticcheck's buildir, staticcheck's generated pass, which every context's IsGenerated reads, and
	// every registered listener's Requires, and its result is a Result.
	Analyzer *analysis.Analyzer

	// registered is each rule registered on the dispatcher, in the order it was registered.
	registered []registeredRule
	// byInstructionKind is, for each instruction kind any listener lists, the indexes in registered of the listeners
	// that list it, so an instruction reaches only the listeners declaring its kind, at the cost of one lookup.
	byInstructionKind map[reflect.Type][]int
}

// registeredRule is one rule a dispatcher calls.
type registeredRule struct {
	// name is the rule's name, which Report stamps and Result is keyed by.
	name string
	// analyzer is the rule's own analyzer, whose name a crash names and whose flags are the rule's options.
	analyzer *analysis.Analyzer
	listener *rule.Listener
}

// Result is what a dispatcher found over one package, each rule's outcome under the rule's name.
type Result map[string]Outcome

// Outcome is what one rule's listener found over one package: its findings, or the crash that ended its run there,
// in which case it has no findings, since a run cut short could have reported anything.
type Outcome struct {
	Diagnostics []analysis.Diagnostic
	Crash       error
}

// NewIr is a dispatcher with no rule registered on it.
func NewIr() *Ir {
	dispatcher := &Ir{byInstructionKind: map[reflect.Type][]int{}}
	dispatcher.Analyzer = &analysis.Analyzer{
		Name: "dispatch_ir",
		Doc: "walks each package's functions in staticcheck's intermediate representation once, calling every " +
			"listener registered on it, for the rule analyzers that report what each found",
		Requires:   []*analysis.Analyzer{staticcheck_ir.Analyzer, generated.Analyzer},
		ResultType: reflect.TypeFor[Result](),
		Run:        dispatcher.run,
	}
	return dispatcher
}

// Holds is whether listener is registered on the dispatcher.
func (dispatcher *Ir) Holds(listener *rule.Listener) bool {
	for _, registered := range dispatcher.registered {
		if registered.listener == listener {
			return true
		}
	}
	return false
}

// register adds the rule named name, run by analyzer, to the rules the dispatcher calls, and what its listener
// requires to what the dispatcher requires. A name registered twice panics: the outcome is keyed by name, so the two
// rules' findings would land under one, and registration runs as the registry is built, where a panic stops every
// test before any rule runs.
func (dispatcher *Ir) register(name string, analyzer *analysis.Analyzer, listener *rule.Listener) {
	for _, registered := range dispatcher.registered {
		if registered.name == name {
			// Invariant: the registry names each rule once, which TestEveryRegisteredRuleValidatesAndHasItsOwnName
			// holds, so a second registration of a name is a mistake in building the registry, never in a package.
			panic(fmt.Sprintf("rule %s is registered on the dispatcher twice, and a dispatcher's outcomes are "+
				"keyed by rule name", name))
		}
	}
	index := len(dispatcher.registered)
	dispatcher.registered = append(dispatcher.registered, registeredRule{
		name:     name,
		analyzer: analyzer,
		listener: listener,
	})
	for _, required := range listener.Requires {
		if !slices.Contains(dispatcher.Analyzer.Requires, required) {
			dispatcher.Analyzer.Requires = append(dispatcher.Analyzer.Requires, required)
		}
	}
	// A kind listed twice is the registry's to refuse; here it is heard once all the same.
	listed := map[reflect.Type]bool{}
	for _, kind := range listener.Instructions {
		kindType := reflect.TypeOf(kind)
		if kindType == nil || listed[kindType] {
			continue
		}
		listed[kindType] = true
		dispatcher.byInstructionKind[kindType] = append(dispatcher.byInstructionKind[kindType], index)
	}
}

// run walks the package's functions once, calling every registered listener, each with its own context and its own
// recover, and returns each rule's outcome.
func (dispatcher *Ir) run(pass *analysis.Pass) (any, error) {
	functions, readError := staticcheck_ir.Functions(pass)
	if readError != nil {
		return nil, readError
	}
	generatedFiles, isGeneratedFiles := pass.ResultOf[generated.Analyzer].(map[string]generated.Generator)
	if !isGeneratedFiles {
		return nil, fmt.Errorf("reading %s's result: it is %T, where staticcheck's map of generated files was "+
			"expected", generated.Analyzer.Name, pass.ResultOf[generated.Analyzer])
	}
	// One judgment of the package's files, shared by every rule's context.
	isGenerated := func(filename string) bool {
		// staticcheck's pass keys a file by the name it was parsed from, and a check asks by the name a finding is
		// displayed in, the two differing only under a line directive naming another Go file, where staticcheck's
		// own lookup misses too.
		_, found := generatedFiles[filename]
		return found
	}
	runs := make([]*listenerRun, len(dispatcher.registered))
	for index := range dispatcher.registered {
		runs[index] = newListenerRun(&dispatcher.registered[index], pass, isGenerated)
	}
	for _, function := range functions {
		for _, run := range runs {
			run.function(function)
		}
		// With no listener hearing an instruction, the blocks are not walked at all.
		if len(dispatcher.byInstructionKind) > 0 {
			for _, block := range function.Blocks {
				for _, instruction := range block.Instrs {
					for _, index := range dispatcher.byInstructionKind[reflect.TypeOf(instruction)] {
						runs[index].instruction(instruction)
					}
				}
			}
		}
		for _, run := range runs {
			run.functionExit(function)
		}
	}
	for _, run := range runs {
		run.packageExit()
	}
	result := Result{}
	for _, run := range runs {
		result[run.registered.name] = run.outcome()
	}
	return result, nil
}
