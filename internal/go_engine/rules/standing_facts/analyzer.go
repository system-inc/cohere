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
	"maps"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_presence"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
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
// ctrlflow finishes there, so every package a question can reach is held before one is asked; and printf and the
// deprecated facts analyzer for their facts, wrapper facts and deprecations, which the checker pushes too (#wxx4ar6),
// and which it compares. It is rooted only beside a census build's rules, which a run measuring the reach takes, so a
// project whose rules need neither runs them there alone, and their facts reach no entry or key (check's fact reach
// settle tests).
var Analyzer = &analysis.Analyzer{
	Name:       analyzerName,
	Doc:        "settles which function bodies an extra build leaves to the host's findings once facts are compared",
	Run:        run,
	Requires:   []*analysis.Analyzer{ctrlflow.Analyzer, printf.Analyzer, deprecated.Analyzer},
	ResultType: reflect.TypeFor[*Result](),
}

// run is the package's standing set in this build, settled: nothing for a package the checker handed no host's side
// of, or that the host's side has the build settle nothing of, and else what the host's set keeps, which may be
// nothing as the walk starts, once every differing fact and answer has taken its users out.
func run(pass *analysis.Pass) (any, error) {
	host, held := pass.ResultOf[rule.FactHost].(fact_reach.Host)
	if !held {
		return &Result{}, nil
	}
	standing, partial := host.Standing()
	if !partial {
		// The build lints the package whole and reads no settlement of it, so nothing is compared.
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
	wrappers, isWrappers := pass.ResultOf[printf.Analyzer].(*printf.Wrappers)
	if !isWrappers {
		return nil, fmt.Errorf("settling the standing bodies of %s: printf's result is a %T", pass.Pkg.Path(),
			pass.ResultOf[printf.Analyzer])
	}
	deprecations, isDeprecations := pass.ResultOf[deprecated.Analyzer].(deprecated.Result)
	if !isDeprecations {
		return nil, fmt.Errorf("settling the standing bodies of %s: the deprecated facts' result is a %T",
			pass.Pkg.Path(), pass.ResultOf[deprecated.Analyzer])
	}
	// Ignored: a run that records no reads hands none, and a recorded question then records nothing.
	reads, _ := pass.ResultOf[rule.Recorded].(*build_reads.Reads)
	pushed := pushedFacts{wrappers: wrappers, deprecations: deprecations, reads: reads, home: pass.Pkg}
	result := &Result{Partial: true}
	functions := &functionFinder{pass: pass}
	// A copy, since the host's own list is not this pass's to append to.
	named := slices.Clone(host.OrderBound())
	// The package's own functions named for their deprecation, by key.
	ownDeprecations := map[fact_reach.Object]bool{}

	// The no-return fact of each function a body's findings can turn on, the host's beside ctrlflow's here. For a
	// function of this package CFGs.NoReturn is the fact itself. For another package's it is the fact only where
	// this package calls the function as a statement, and reads false anywhere else, so a host's fact the package
	// never reads that way can name an object whose fact is in truth equal. That is the safe side: it only lints a
	// body the host's findings would have served. Then each other fact the checker pushes that the host held of the
	// function and a build compares, its wrapper fact, where the host ran printf on the package, one the build's printf
	// never looked up reading unknown, and its deprecation, where the host ran the deprecated facts analyzer there
	// (pushedFacts).
	for _, function := range host.Functions() {
		result.FunctionsCompared++
		own, found := functions.function(function.Identity)
		switch {
		case !found:
			named = append(named, fact_reach.Named{Object: function.Object, Reason: unknownNoReturnReason})
			result.FunctionsNamed++
			continue
		case graphs.NoReturn(own) != function.NoReturn:
			named = append(named, fact_reach.Named{Object: function.Object, Reason: noReturnReason})
			result.FunctionsNamed++
		}
		compared, differing := pushed.differing(function.Object, function.Pushed, own)
		result.PushedCompared += compared
		result.PushedNamed += len(differing)
		named = append(named, differing...)
		for _, each := range differing {
			if each.Reason == pushedReasons[deprecated.Analyzer.Name] && own.Pkg() == pass.Pkg {
				ownDeprecations[function.Object] = true
			}
		}
	}
	// The wrapper fact of each interface method and variable a body uses whose shape lets it hold one, the host's
	// beside the build's printf's, as a function's is compared above (#zqx7gke): printf finds an interface method a
	// wrapper where an implementation assigned to it anywhere in the package is one, and a variable one where the
	// function literal assigned to it forwards to one, so either's fact can differ with its text and file the same in
	// both builds. Neither is a function, and neither has a no-return fact. One the build has no own object of reads
	// unknown. A variable or a field of another package is named after its facts whatever they read: printf may hold a
	// kind of it found here alone, which neither build's facts carry (fact_reach.PushedObject.Local). Its facts are
	// still compared first, so one that differs names it for that, as any other object's would.
	for _, object := range host.PushedObjects() {
		own, found := functions.pushedObject(object.Identity)
		if !found {
			own = nil
		}
		compared, differing := pushed.differing(object.Object, object.Pushed, own)
		result.PushedCompared += compared
		result.PushedNamed += len(differing)
		named = append(named, differing...)
		if object.Local {
			result.PushedCompared++
			result.PushedNamed++
			named = append(named, fact_reach.Named{Object: object.Object, Reason: localWrapperReason})
		}
	}
	// Each function of the package named for its own deprecation is a user of it at the place that declares it
	// (ownDeprecationUsers).
	source := ownDeprecationUsers{Host: host, declaring: map[fact_reach.Object][]fact_reach.Place{}}
	for _, place := range host.Places() {
		for _, object := range host.Declared(place) {
			if ownDeprecations[object] {
				source.declaring[object] = append(source.declaring[object], place)
			}
		}
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

	// The bodies a package-wide rule reads along a row's edge, which must be present in the build: the objects whose
	// users leave, and each body that leaves by itself, taken out of the pass's own copy here, before the cycles are
	// read, so a cycle that touches it is one the build lints, and among the bodies reached below, whose recorded
	// answers the walk asks.
	presenceNamed, leaving := standing_presence.Present(pass, host, standing, functions.find)
	named = append(named, presenceNamed...)
	result.PresenceNamed = map[standing_presence.Row]int{}
	result.PresenceRemoved = map[standing_presence.Row]int{}
	for _, object := range presenceNamed {
		if row, isRow := standing_presence.RowOf(object.Reason); isRow {
			result.PresenceNamed[row]++
		}
	}
	for _, left := range result.keepLeaving(standing, leaving) {
		delete(standing, left)
	}

	// The members of each cycle of calls in the build that touches a body it lints, whose answers may hang on the order
	// the build's walk enters the cycle in, where the host's order bound saw no such cycle.
	ordered := buildOrderNamed(pass, host, standing, functions)
	result.BuildOrderNamed = len(ordered)
	named = append(named, ordered...)

	// The bodies the build lints already, a body presence took out among them, whose recorded answers the walk asks
	// first. The host's set is only read, here and by the walk: what still stands is the settlement's own map.
	var reached []fact_reach.Place
	for _, place := range host.Places() {
		if !standing[place] {
			reached = append(reached, place)
		}
	}
	// Every kind the host answered about the package, by the unit its answers are in, which a body that leaves reads
	// unknown for where it holds no record of it (fact_reach.Settle). A kind the build cannot ask is among them, since
	// its judgments read callees' answers from its state as any kind's may. A kind the host answered nothing of here is
	// not, registered or not: no answer of the host's about the package's objects is one of it, so it served none of
	// the package's bodies, whatever its judgments read (fact_reach.Host.Kinds), and an answer of it about another
	// package's object is compared before the walk (Imported).
	//
	// A kind that records every judgment is none of them (fact_resolver.Kind.RecordsEveryJudgment, #z9fze0t). The
	// rule is there because a kind's judgment may read a callee's body and leave no record of it, so a body the build
	// lints with no record of the kind may still have served an answer of the host's. Such a kind hands the resolver
	// every verdict its judgments reach, once final, and reads another function's body only by way of a verdict, so
	// its record in a package the host computed in is complete: a function the host holds no answer of the kind
	// about is one the kind never judged there, no answer of the host's rests on its body, and there is nothing to
	// compare. Where it did judge the function the host holds the answer, recorded at its body, which the walk asks
	// as the body leaves. A package handed an unsure answer keeps nothing, and has no side here at all, so the build
	// lints it whole (check.FactReachCapture.Packages). Nor has a package the store answered a question about, loaded
	// from source and answered from its entry: that answer was judged in another run, on bodies this one never judged,
	// so no kind's record of the package is complete (check.FactReachCapture.StoreAnswered).
	units := map[string]bool{}
	for _, kind := range host.Kinds() {
		units[kind.Name] = kind.Parameters
	}
	for _, name := range fact_resolver.RecordingNames() {
		delete(units, name)
	}
	var kinds []fact_reach.Kind
	for _, name := range slices.Sorted(maps.Keys(units)) {
		kinds = append(kinds, fact_reach.Kind{Name: name, Parameters: units[name]})
	}
	result.Settlement = fact_reach.Settle(standing, reached, named, kinds, source, asked.compare)
	for _, removal := range result.Settlement.Removed {
		if row, isRow := standing_presence.RowOf(removal.Reason); isRow {
			result.PresenceRemoved[row]++
		}
	}

	// noany's bound and the edge rows again, to a fixed point with the facts: a body that left only for a differing
	// fact is present now, so its flows may route to a key, and it may start a function that still stands, or be
	// started by one. What still stands is read again, and what that names and takes out is settled, the bodies as
	// reached, until a round takes nothing out. Each round takes a body out of the standing set or ends, so there are
	// at most as many rounds as bodies stood. A question asked in an earlier round is answered from the same verdicts
	// again, and counted again where a later round's body records it.
	for {
		namedAgain, leavingAgain := standing_presence.Reread(pass, host, result.Settlement.Standing, functions.find)
		again := maps.Clone(result.Settlement.Standing)
		reachedAgain := result.keepLeaving(again, leavingAgain)
		for _, left := range reachedAgain {
			delete(again, left)
		}
		settled := fact_reach.Settle(again, reachedAgain, namedAgain, kinds, source, asked.compare)
		if len(reachedAgain) == 0 && len(settled.Removed) == 0 {
			break
		}
		for _, removal := range settled.Removed {
			if row, isRow := standing_presence.RowOf(removal.Reason); isRow {
				result.PresenceRemoved[row]++
			}
		}
		result.Settlement.Standing = settled.Standing
		result.Settlement.Removed = append(result.Settlement.Removed, settled.Removed...)
		result.Settlement.Asked += settled.Asked
		result.Settlement.Differing = summedCounts(result.Settlement.Differing, settled.Differing)
		result.Settlement.Unknown = summedCounts(result.Settlement.Unknown, settled.Unknown)
		result.Settlement.Unrecorded = summedCounts(result.Settlement.Unrecorded, settled.Unrecorded)
	}
	return result, nil
}

// summedCounts are each kind's count of first and second, summed, in a map of their own.
func summedCounts(first map[string]int, second map[string]int) map[string]int {
	summed := maps.Clone(first)
	for kind, count := range second {
		summed[kind] += count
	}
	return summed
}
