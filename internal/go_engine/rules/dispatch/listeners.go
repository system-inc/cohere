package dispatch

import (
	"fmt"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// listeners is what every dispatcher keeps of the rules registered on it, whichever walk it makes: the dispatcher's
// analyzer, each rule with its listener, and which listeners each kind reaches. Ir and Syntax each hold one and walk
// over it, so registration, a rule's analyzer, its context and its outcome are built one way for both.
//
// Every rule is registered before the first run, since the checker reads the analyzer's requirements as a run starts,
// and a run reads the registered rules without a lock.
type listeners struct {
	// analyzer is the dispatcher's own analyzer, which every rule analyzer built on the dispatcher requires. It
	// requires what the walk reads, staticcheck's generated pass, which every context's IsGenerated reads, and every
	// registered listener's Requires, and its result is a Result.
	analyzer *analysis.Analyzer
	// registered is each rule registered on the dispatcher, in the order it was registered.
	registered []registeredRule
	// byKind is, for each kind any listener lists, the indexes in registered of the listeners that list it, so a node
	// or an instruction reaches only the listeners declaring its kind, at the cost of one lookup.
	byKind map[reflect.Type][]int
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

// newListeners is the registry of a dispatcher whose analyzer is named name, documented by doc, runs run, and requires
// walkRequires, beside staticcheck's generated pass.
func newListeners(
	name string,
	doc string,
	walkRequires []*analysis.Analyzer,
	runDespiteErrors bool,
	run func(pass *analysis.Pass) (any, error),
) *listeners {
	return &listeners{
		analyzer: &analysis.Analyzer{
			Name:             name,
			Doc:              doc,
			Requires:         append(slices.Clone(walkRequires), generated.Analyzer),
			RunDespiteErrors: runDespiteErrors,
			ResultType:       reflect.TypeFor[Result](),
			Run:              run,
		},
		byKind: map[reflect.Type][]int{},
	}
}

// Analyzer is the dispatcher's own analyzer, which every rule analyzer built on the dispatcher requires.
func (listeners *listeners) Analyzer() *analysis.Analyzer {
	return listeners.analyzer
}

// Holds is whether listener is registered on the dispatcher.
func (listeners *listeners) Holds(listener *rule.Listener) bool {
	for _, registered := range listeners.registered {
		if registered.listener == listener {
			return true
		}
	}
	return false
}

// ruleAnalyzer registers listener as the rule named ruleName, hearing the kinds listed, and returns the rule's own
// analyzer, named analyzerName and documented by doc, which requires the dispatcher. Its run reports each finding the
// listener made on the package, or returns the listener's crash there, so a finding and a crash are the rule's own,
// as they were when the rule walked the package itself. Its options are flags the caller adds to the analyzer, which
// the listener's context reads.
func (listeners *listeners) ruleAnalyzer(
	ruleName string,
	analyzerName string,
	doc string,
	listener *rule.Listener,
	kinds []reflect.Type,
) *analysis.Analyzer {
	dispatcherAnalyzer := listeners.analyzer
	analyzer := &analysis.Analyzer{
		Name:     analyzerName,
		Doc:      doc,
		Requires: []*analysis.Analyzer{dispatcherAnalyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			result, isResult := pass.ResultOf[dispatcherAnalyzer].(Result)
			if !isResult {
				return nil, fmt.Errorf("reading the dispatcher's result for %s: it is %T, where a dispatch.Result "+
					"was expected", ruleName, pass.ResultOf[dispatcherAnalyzer])
			}
			outcome, found := result[ruleName]
			if !found {
				return nil, fmt.Errorf("the dispatcher holds no outcome for %s, so its listener was never "+
					"registered on the dispatcher the rule requires", ruleName)
			}
			if outcome.Crash != nil {
				return nil, outcome.Crash
			}
			for _, diagnostic := range outcome.Diagnostics {
				pass.Report(diagnostic)
			}
			return nil, nil
		},
	}
	listeners.register(ruleName, analyzer, listener, kinds)
	return analyzer
}

// register adds the rule named name, run by analyzer, to the rules the dispatcher calls, hearing kinds, and what its
// listener requires to what the dispatcher requires. A name registered twice panics: the outcome is keyed by name,
// so the two rules' findings would land under one, and registration runs as the registry is built, where a panic
// stops every test before any rule runs.
func (listeners *listeners) register(
	name string,
	analyzer *analysis.Analyzer,
	listener *rule.Listener,
	kinds []reflect.Type,
) {
	for _, registered := range listeners.registered {
		if registered.name == name {
			// Invariant: the registry names each rule once, which TestEveryRegisteredRuleValidatesAndHasItsOwnName
			// holds, so a second registration of a name is a mistake in building the registry, never in a package.
			panic(fmt.Sprintf("rule %s is registered on the dispatcher twice, and a dispatcher's outcomes are "+
				"keyed by rule name", name))
		}
	}
	index := len(listeners.registered)
	listeners.registered = append(listeners.registered, registeredRule{
		name:     name,
		analyzer: analyzer,
		listener: listener,
	})
	for _, required := range listener.Requires {
		if !slices.Contains(listeners.analyzer.Requires, required) {
			listeners.analyzer.Requires = append(listeners.analyzer.Requires, required)
		}
	}
	// A kind listed twice is the registry's to refuse; here it is heard once all the same.
	listed := map[reflect.Type]bool{}
	for _, kind := range kinds {
		if kind == nil || listed[kind] {
			continue
		}
		listed[kind] = true
		listeners.byKind[kind] = append(listeners.byKind[kind], index)
	}
}

// start is every registered listener ready to hear pass's package, each with a context of its own, all judging the
// package's generated files by one reading of staticcheck's generated pass.
func (listeners *listeners) start(pass *analysis.Pass) ([]*listenerRun, error) {
	generatedFiles, isGeneratedFiles := pass.ResultOf[generated.Analyzer].(map[string]generated.Generator)
	if !isGeneratedFiles {
		return nil, fmt.Errorf("reading %s's result: it is %T, where staticcheck's map of generated files was "+
			"expected", generated.Analyzer.Name, pass.ResultOf[generated.Analyzer])
	}
	isGenerated := func(filename string) bool {
		// A plain lookup by the name the file was parsed from, which is how staticcheck's pass keys it. A caller asking
		// by display name, as report.FilterGenerated does, misses under a `//line` directive naming another file,
		// exactly as staticcheck's own does.
		_, found := generatedFiles[filename]
		return found
	}
	runs := make([]*listenerRun, len(listeners.registered))
	for index := range listeners.registered {
		runs[index] = newListenerRun(&listeners.registered[index], pass, isGenerated)
	}
	return runs, nil
}

// finish calls every PackageExit once the walk is over and returns each rule's outcome.
func finish(runs []*listenerRun) Result {
	for _, run := range runs {
		run.packageExit()
	}
	result := Result{}
	for _, run := range runs {
		result[run.registered.name] = run.outcome()
	}
	return result
}
