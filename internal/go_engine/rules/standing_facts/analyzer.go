// Package standing_facts settles, before any rule runs on a package in an extra build, which of the package's function
// bodies the build may leave to the host's findings once facts are counted (#5vbs1np, stage 4 of #9paaen9). The reach
// at body grain says which bodies a difference in types reaches, and the build lints those; a body it leaves standing
// keeps the host's findings only if every fact and answer its rules read is equal in the build. So this analyzer
// compares what the host held with the build's own, the no-return fact of each function a body's findings can turn
// on and each answer the host read or recorded, and takes out of the standing set every body that uses an object
// whose fact or answer differs, to a fixed point (fact_reach.Settle).
//
// The host's side comes from the checker, under rule.FactHost, and every name in it is the host's: this package finds
// the build's own object for each (functionFinder) and never names one of its own to the host. Where anything cannot
// be found, asked or trusted, the object is taken as differing, which only lints a body the host's findings might
// have served.
package standing_facts

import (
	"fmt"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// analyzerName is the analyzer's name, and so its row under `--timing`, which its questions are taken out of
// (fact_resolver.Question). It is a constant of its own because run, which Analyzer holds, cannot name Analyzer.
const analyzerName = "standing_facts"

// Why an object was named for its function's no-return fact: the build's fact is another than the host's, or the
// build has no function the host's identity names, so its fact there cannot be read at all.
const (
	noReturnReason        fact_reach.Reason = "no-return"
	unknownNoReturnReason fact_reach.Reason = "unknown no-return"
)

// Analyzer hands the package's rules the bodies that stand in this build, which each asks of one body at a time
// (Result.Stands). It requires ctrlflow for the build's no-return facts, and because the resolver learns a package as
// ctrlflow finishes there, so every package a question can reach is held before one is asked.
var Analyzer = &analysis.Analyzer{
	Name:       analyzerName,
	Doc:        "settles which function bodies an extra build leaves to the host's findings once facts are compared",
	Run:        run,
	Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
	ResultType: reflect.TypeFor[*Result](),
}

// run is the package's standing set in this build, settled: nothing for a package the checker handed no host's side
// of, or that the build lints whole, and else what the host's set keeps once every differing fact and answer has
// taken its users out.
func run(pass *analysis.Pass) (any, error) {
	host, held := pass.ResultOf[rule.FactHost].(fact_reach.Host)
	if !held {
		return &Result{}, nil
	}
	standing, partial := host.Standing()
	if !partial {
		// The build lints the package whole, so nothing is compared: no body's findings are the host's.
		return &Result{}, nil
	}
	resolver, resolverError := fact_resolver.Of(pass)
	if resolverError != nil {
		return nil, resolverError
	}
	reader := resolver.Package(pass.Pkg)
	if reader == nil {
		return nil, fmt.Errorf("settling the standing bodies of %s: the fact resolver never learned it, though "+
			"ctrlflow, which it learns a package from, ran on it first", pass.Pkg.Path())
	}
	graphs, isGraphs := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	if !isGraphs {
		return nil, fmt.Errorf("settling the standing bodies of %s: ctrlflow's result is a %T", pass.Pkg.Path(),
			pass.ResultOf[ctrlflow.Analyzer])
	}
	result := &Result{Partial: true}
	functions := &functionFinder{pass: pass}
	// A copy, since the host's own list is not this pass's to append to.
	named := slices.Clone(host.OrderBound())

	// The no-return fact of each function a body's findings can turn on, the host's beside ctrlflow's here. For a
	// function of this package CFGs.NoReturn is the fact itself. For another package's it is the fact only where
	// this package calls the function as a statement, and reads false anywhere else, so a host's fact the package
	// never reads that way can name an object whose fact is in truth equal. That is the safe side: it only lints a
	// body the host's findings would have served.
	for _, function := range host.Functions() {
		result.FunctionsCompared++
		own, found := functions.function(function.Identity)
		switch {
		case !found:
			named = append(named, fact_reach.Named{Object: function.Object, Reason: unknownNoReturnReason})
		case graphs.NoReturn(own) != function.NoReturn:
			named = append(named, fact_reach.Named{Object: function.Object, Reason: noReturnReason})
		default:
			continue
		}
		result.FunctionsNamed++
	}

	// The answers the host's analysis read from other packages, each asked before any body leaves: no body of this
	// package records them, so the walk would never meet one, and one that differs names its object with the reason
	// the walk gives an answer it asked.
	asked := &questions{host: host, resolver: resolver, reader: reader, functions: functions,
		verdicts: map[string]fact_reach.Comparison{}}
	for _, question := range host.Imported() {
		result.ImportedAsked++
		switch asked.compare(question) {
		case fact_reach.Equal:
			continue
		case fact_reach.Differs:
			named = append(named, fact_reach.Named{Object: question.About,
				Reason: fact_reach.DiffersReason(question.Kind)})
		default:
			named = append(named, fact_reach.Named{Object: question.About,
				Reason: fact_reach.UnknownReason(question.Kind)})
		}
		result.ImportedNamed++
	}

	// The members of each cycle of calls in the build that touches a body it lints, whose answers may hang on the order
	// the build's walk enters the cycle in, where the host's order bound saw no such cycle.
	ordered := buildOrderNamed(pass, host, standing, functions)
	result.BuildOrderNamed = len(ordered)
	named = append(named, ordered...)

	// The bodies the build lints already, whose recorded answers the walk asks first. The host's set is only read,
	// here and by the walk: what still stands is the settlement's own map.
	var reached []fact_reach.Place
	for _, place := range host.Places() {
		if !standing[place] {
			reached = append(reached, place)
		}
	}
	// Every kind the build can ask again, and every kind the host answered about the package, which a body that leaves
	// reads unknown for where it holds no record of it (fact_reach.Settle). A kind the build cannot ask is among them
	// wherever the host answered it, since its judgments read callees' answers from its state as any kind's may.
	kinds := slices.Concat(fact_resolver.RegisteredNames(), host.Kinds())
	slices.Sort(kinds)
	result.Settlement = fact_reach.Settle(standing, reached, named, slices.Compact(kinds), host, asked.compare)
	return result, nil
}
