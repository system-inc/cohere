package dispatch

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// listeners is what every dispatcher keeps of the rules registered on it, whichever walk it makes: the dispatcher's
// analyzer, and each rule with its listener and the kinds it hears. IntermediateRepresentation and Syntax each hold one
// and walk over what it starts on a package, so registration, which rules a pass calls, a rule's analyzer, its context
// and its outcome are built one way for both.
//
// Every rule is registered before the first run, since the checker reads the analyzer's requirements as a run starts,
// and a run reads the registered rules without a lock.
type listeners struct {
	// analyzer is the dispatcher's own analyzer, which every rule analyzer built on the dispatcher requires. It
	// requires what the walk reads, staticcheck's generated pass, which every context's IsGenerated reads, and every
	// registered listener's Requires after them, and its result is a Result. The engine's checker keeps a listener's
	// requirement on a package only where the run holds a rule requiring it there (see ListenerRequirement).
	analyzer *analysis.Analyzer
	// walkRequires is what the walk itself reads, the analyzer's requirements before any listener's joined them. One
	// of them failing on a package fails the dispatcher there, since there is no walk without it.
	walkRequires []*analysis.Analyzer
	// registered is each rule registered on the dispatcher, in the order it was registered.
	registered []registeredRule
	// shardBytes is how much source one shard of a split package holds (see shardsOf), defaultShardBytes but in a
	// test that splits a small fixture.
	shardBytes int
}

// registeredRule is one rule a dispatcher calls.
type registeredRule struct {
	// name is the rule's name, which Report stamps and Result is keyed by.
	name string
	// analyzer is the rule's own analyzer, whose name a crash names and whose flags are the rule's options.
	analyzer *analysis.Analyzer
	listener *rule.Listener
	// kinds is the node or instruction kinds the listener lists, each once.
	kinds []reflect.Type
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
	walked := append(slices.Clone(walkRequires), generated.Analyzer)
	return &listeners{
		analyzer: &analysis.Analyzer{
			Name:             name,
			Doc:              doc,
			Requires:         slices.Clone(walked),
			RunDespiteErrors: runDespiteErrors,
			ResultType:       reflect.TypeFor[Result](),
			Run:              run,
		},
		walkRequires: walked,
		shardBytes:   defaultShardBytes,
	}
}

// IsDispatcher is whether analyzer is a dispatcher's own, which the engine's checker lets run past a failed
// requirement on its package (see rule.Failed). Every dispatcher's analyzer, and no other, returns a Result, so this
// reads it off the analyzer itself, a test's fresh dispatcher included, with no registry of dispatchers to keep.
func IsDispatcher(analyzer *analysis.Analyzer) bool {
	return analyzer.ResultType == reflect.TypeFor[Result]()
}

// ListenerRequirement reports whether analyzer is a dispatcher's and requires required for its listeners alone,
// beyond what its walk reads, for the engine's checker under checker.Options.ForDependents. The checker keeps such a
// requirement on a package only where an analyzer requiring the dispatcher there requires it too, and every rule
// analyzer requires what its listener does (see ruleAnalyzer), so an analyzer that only the listeners of rules the run
// left out require never runs: staticcheck's purity on every dependency, with every rule reading it turned off.
//
// It reads the analyzer itself, as IsDispatcher does. The walk's requirements are a dispatcher's first, through
// staticcheck's generated pass, which newListeners puts last among them, and register only appends after them, never
// adding generated again, so what follows generated is exactly the listeners'. A dispatcher without generated, which
// newListeners never makes, keeps every requirement, as a driver without the option does.
func ListenerRequirement(analyzer *analysis.Analyzer, required *analysis.Analyzer) bool {
	if !IsDispatcher(analyzer) {
		return false
	}
	walked := slices.Index(analyzer.Requires, generated.Analyzer)
	return walked >= 0 && slices.Contains(analyzer.Requires[walked+1:], required)
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
//
// It requires what the listener requires too, beyond what the walk reads, though only the dispatcher reads those
// results. Where one of them fails on a package, the dispatcher runs past it (see start), and the checker fails the
// rule's own action there itself, so the rule's crash is the checker's own sentence, naming each failed requirement
// as the checker names an action and with its cause, word for word what it was when the rule walked by itself. A
// crash the dispatcher wrote could do neither: a pass names its package by import path, where the checker names a
// test variant `p [p.test]`, and the engine's crash sentence finds the cause by walking the failed action's failed
// prerequisites, which a crash its run returned has none of.
func (listeners *listeners) ruleAnalyzer(
	ruleName string,
	analyzerName string,
	doc string,
	listener *rule.Listener,
	kinds []reflect.Type,
) *analysis.Analyzer {
	dispatcherAnalyzer := listeners.analyzer
	requires := []*analysis.Analyzer{dispatcherAnalyzer}
	for _, required := range listener.Requires {
		// A requirement the walk reads too fails the dispatcher, which already fails the rule, as it did before the
		// rule required it.
		if !slices.Contains(listeners.walkRequires, required) && !slices.Contains(requires, required) {
			requires = append(requires, required)
		}
	}
	analyzer := &analysis.Analyzer{
		Name:     analyzerName,
		Doc:      doc,
		Requires: requires,
		Run: func(pass *analysis.Pass) (any, error) {
			result, isResult := pass.ResultOf[dispatcherAnalyzer].(Result)
			if !isResult {
				return nil, fmt.Errorf("reading the dispatcher's result for %s: it is %T, where a dispatch.Result "+
					"was expected", ruleName, pass.ResultOf[dispatcherAnalyzer])
			}
			outcome, found := result[ruleName]
			if !found {
				return nil, fmt.Errorf("the dispatcher holds no outcome for %s, so its listener was never "+
					"registered on the dispatcher the rule requires, or that dispatcher's pass did not count the "+
					"rule among its dependents", ruleName)
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
// listener requires to what the dispatcher requires, after what is there, which ListenerRequirement reads. A name
// registered twice panics: the outcome is keyed by name, so the two rules' findings would land under one, and
// registration runs as the registry is built, where a panic stops every test before any rule runs.
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
	for _, required := range listener.Requires {
		if !slices.Contains(listeners.analyzer.Requires, required) {
			listeners.analyzer.Requires = append(listeners.analyzer.Requires, required)
		}
	}
	// A kind listed twice is the registry's to refuse; here it is heard once all the same.
	heard := []reflect.Type{}
	for _, kind := range kinds {
		if kind != nil && !slices.Contains(heard, kind) {
			heard = append(heard, kind)
		}
	}
	listeners.registered = append(listeners.registered, registeredRule{
		name:     name,
		analyzer: analyzer,
		listener: listener,
		kinds:    heard,
	})
}

// startedRuns is the listeners a dispatcher calls over one package, each ready with a context of its own.
type startedRuns struct {
	// runs is each started listener, in the order its rule was registered.
	runs []*listenerRun
	// byKind is, for each kind a started listener lists, the started listeners that list it, in runs' order, so a
	// node or an instruction reaches only the listeners declaring its kind, at the cost of one lookup.
	byKind map[reflect.Type][]*listenerRun
	// notStarted is the outcome of each rule of the run whose listener was not started because something it
	// requires failed on the package.
	notStarted Result
	// timing is the run's collector under `--timing`, which the checker hands the pass under rule.Timed, nil
	// otherwise, and dispatcher is the name of the dispatcher's own analyzer, whose row finish takes the listeners'
	// calls out of.
	timing     *timing.Collector
	dispatcher string
}

// start is the listeners to call over pass's package, each ready with a context of its own, all judging the package's
// generated files by one reading of staticcheck's generated pass. They are those of the rules whose analyzers require
// the dispatcher on this package in this run, the set the engine's checker hands the pass under rule.Dependents, so a
// rule the run left out, by its settings, a filter, or by being registered from a package the process merely
// imports, costs no call and no context: its analyzer is not in the graph, and a dispatcher's outcome is read only by
// the analyzers that require it.
//
// A pass without the set comes from a driver that is not the engine's, such as analysistest's or a test calling
// checker.Analyze with no Options.Dependents, and there every registered listener starts, as before the set existed.
// That costs only calls, never output: an outcome no analyzer of the graph requires is never read.
//
// Under rule.Failed the pass runs past what failed on the package. A listener any of whose own Requires failed is not
// started, since it would read a result that is not there, and every other listener starts. A failure in what the
// walk itself reads ends the pass, since there is no walk without it.
//
// shards above 1 splits the package (see shardsOf): whole holds every started listener that is not marked FileLocal,
// with every rule not started, and each of parts holds a run of every started FileLocal listener, with a file-local
// context, to hear one shard's files. At 1, whole holds every started listener and parts is empty.
func (listeners *listeners) start(pass *analysis.Pass, shards int) (*startedRuns, []*startedRuns, error) {
	// Absent under a driver that runs nothing past a failure, where the pass runs only once everything it requires
	// finished, and the nil map reads as nothing failed.
	failed, _ := pass.ResultOf[rule.Failed].(map[*analysis.Analyzer]error)
	if walkError := listeners.walkFailed(pass, failed); walkError != nil {
		return nil, nil, walkError
	}
	generatedFiles, isGeneratedFiles := pass.ResultOf[generated.Analyzer].(map[string]generated.Generator)
	if !isGeneratedFiles {
		return nil, nil, fmt.Errorf("reading %s's result: it is %T, where staticcheck's map of generated files "+
			"was expected", generated.Analyzer.Name, pass.ResultOf[generated.Analyzer])
	}
	isGenerated := func(filename string) bool {
		// A plain lookup by the name the file was parsed from, which is how staticcheck's pass keys it. A caller asking
		// by display name, as report.FilterGenerated does, misses under a `//line` directive naming another file,
		// exactly as staticcheck's own does.
		_, found := generatedFiles[filename]
		return found
	}
	dependents, knowsDependents := pass.ResultOf[rule.Dependents].(map[*analysis.Analyzer]bool)
	// Absent on a run that did not ask for --timing, where the nil collector measures nothing.
	collector, _ := pass.ResultOf[rule.Timed].(*timing.Collector)
	newStarted := func() *startedRuns {
		return &startedRuns{byKind: map[reflect.Type][]*listenerRun{}, notStarted: Result{}, timing: collector,
			dispatcher: listeners.analyzer.Name}
	}
	whole := newStarted()
	var parts []*startedRuns
	if shards > 1 {
		parts = make([]*startedRuns, shards)
		for index := range parts {
			parts[index] = newStarted()
		}
	}
	for index := range listeners.registered {
		registered := &listeners.registered[index]
		if knowsDependents && !dependents[registered.analyzer] {
			continue
		}
		if crash := requirementsFailed(registered, pass, failed); crash != nil {
			whole.notStarted[registered.name] = Outcome{Crash: crash}
			continue
		}
		if len(parts) > 0 && registered.listener.FileLocal {
			for _, part := range parts {
				part.add(newListenerRun(registered, pass, isGenerated, collector, true))
			}
			continue
		}
		whole.add(newListenerRun(registered, pass, isGenerated, collector, false))
	}
	return whole, parts, nil
}

// add makes run one of the listeners started, heard for every kind its listener lists.
func (started *startedRuns) add(run *listenerRun) {
	started.runs = append(started.runs, run)
	for _, kind := range run.registered.kinds {
		started.byKind[kind] = append(started.byKind[kind], run)
	}
}

// walkFailed is the error that ends a pass for which something the walk itself reads failed on the package, among
// failed, or nil when none of it did.
func (listeners *listeners) walkFailed(pass *analysis.Pass, failed map[*analysis.Analyzer]error) error {
	causes := failedAmong(listeners.walkRequires, failed)
	if len(causes) == 0 {
		return nil
	}
	return fmt.Errorf("walking %s for %s: what the walk reads failed there, %s", pass.Pkg.Path(),
		listeners.analyzer.Name, strings.Join(causes, "; "))
}

// requirementsFailed is the crash of registered's listener on pass's package when any of its own Requires is among
// failed, or nil when none is. The engine never reports it: the rule's own analyzer requires what the listener does
// (see ruleAnalyzer), so the checker fails the rule's action on the package before its run reads this outcome. It is
// here so a result never lacks a rule the run counted, and says what happened all the same.
func requirementsFailed(registered *registeredRule, pass *analysis.Pass, failed map[*analysis.Analyzer]error) error {
	causes := failedAmong(registered.listener.Requires, failed)
	if len(causes) == 0 {
		return nil
	}
	return fmt.Errorf("the listener of %s was not started on %s, since what it requires failed there, %s",
		registered.name, pass.Pkg.Path(), strings.Join(causes, "; "))
}

// failedAmong is each of required that failed, as its name and its error, sorted.
func failedAmong(required []*analysis.Analyzer, failed map[*analysis.Analyzer]error) []string {
	var causes []string
	for _, analyzer := range required {
		if requiredError, didFail := failed[analyzer]; didFail {
			causes = append(causes, fmt.Sprintf("%s: %v", analyzer.Name, requiredError))
		}
	}
	slices.Sort(causes)
	return causes
}

// finish calls every started PackageExit once the walk is over and returns each started rule's outcome, with the
// outcome of each rule whose listener was not started, which is every outcome a rule analyzer of the run reads. Under
// `--timing` it bills each listener's calls over the package to its rule, and takes them out of the dispatcher's own
// row, so that row is the walk alone.
func finish(started *startedRuns) Result {
	for _, run := range started.runs {
		run.packageExit()
	}
	if started.timing != nil {
		var listened time.Duration
		var calls int64
		for _, run := range started.runs {
			started.timing.Listener(run.registered.name, run.cpu, run.calls)
			listened += run.cpu
			calls += run.calls
		}
		started.timing.Nested(started.dispatcher, listened, calls)
	}
	result := Result{}
	for name, outcome := range started.notStarted {
		result[name] = outcome
	}
	for _, run := range started.runs {
		result[run.registered.name] = run.outcome()
	}
	return result
}

// finishShards is finish over a package split into whole and parts: each file-local rule's outcome is its runs' over
// every shard, its findings gathered in shard order, or the first shard's crash in that order, since a run cut short on
// one shard could have reported anything there, as a run cut short on the whole package could.
func finishShards(whole *startedRuns, parts []*startedRuns) Result {
	result := finish(whole)
	crashed := map[string]bool{}
	for _, part := range parts {
		for name, outcome := range finish(part) {
			if crashed[name] {
				continue
			}
			if outcome.Crash != nil {
				result[name] = outcome
				crashed[name] = true
				continue
			}
			gathered := result[name]
			gathered.Diagnostics = append(gathered.Diagnostics, outcome.Diagnostics...)
			result[name] = gathered
		}
	}
	return result
}
