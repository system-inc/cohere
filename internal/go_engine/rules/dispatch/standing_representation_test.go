package dispatch

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// heardCalls is what the standing tests' recording listeners heard over the representation of a package, each under
// its own name, from every run of it at once: each function, each return and each function left, the contexts it was
// called with, one per run, and the package its functions are of, which a test reads once the run is over.
type heardCalls struct {
	lock      sync.Mutex
	heard     map[string][]string
	contexts  map[string]map[*rule.Context]bool
	irPackage *intermediate_representation.Package
}

// newHeardCalls is a heardCalls that has heard nothing.
func newHeardCalls() *heardCalls {
	return &heardCalls{heard: map[string][]string{}, contexts: map[string]map[*rule.Context]bool{}}
}

// listen is a listener over the representation, file-local or not, that records under name each function it is
// handed, each return it hears with the function it is in, and each function it leaves.
func (heard *heardCalls) listen(name string, fileLocal bool) *rule.Listener {
	record := func(context *rule.Context, what string) {
		heard.lock.Lock()
		defer heard.lock.Unlock()
		heard.heard[name] = append(heard.heard[name], what)
		if heard.contexts[name] == nil {
			heard.contexts[name] = map[*rule.Context]bool{}
		}
		heard.contexts[name][context] = true
	}
	return &rule.Listener{
		FileLocal: fileLocal,
		IntermediateRepresentation: &rule.IntermediateRepresentationListener{
			Function: func(context *rule.Context, function *intermediate_representation.Function) {
				record(context, "function "+function.Name())
				heard.lock.Lock()
				defer heard.lock.Unlock()
				// Any source function's is the pass's package.
				heard.irPackage = function.Package
			},
			Instructions: []intermediate_representation.Instruction{(*intermediate_representation.Return)(nil)},
			Instruction: func(context *rule.Context, instruction intermediate_representation.Instruction) {
				record(context, "return in "+instruction.Parent().Name())
			},
			FunctionExit: func(context *rule.Context, function *intermediate_representation.Function) {
				record(context, "exit "+function.Name())
			},
		},
	}
}

// heardBy is what the listener named name heard, sorted, each line once: a function with several returns is heard
// returning once.
func (heard *heardCalls) heardBy(name string) []string {
	heard.lock.Lock()
	defer heard.lock.Unlock()
	return slices.Compact(slices.Sorted(slices.Values(heard.heard[name])))
}

// everyCallOf is what a recording listener hears of the functions named: each handed to it, returning and left,
// sorted.
func everyCallOf(functions ...string) []string {
	calls := []string{}
	for _, function := range functions {
		calls = append(calls, "function "+function, "return in "+function, "exit "+function)
	}
	slices.Sort(calls)
	return calls
}

// The standing fixture's source functions, as the representation names them: the synthetic package initializer, the
// function literal in kept's initializer, which is the initializer's, and the two functions no test plants as
// standing, which a confined listener hears, and the functions everyStandingFunction plants, with the function
// literal in stood, which it does not.
var (
	neverStandingFunctions = []string{"init", "init$1", "reached", "alsoReached"}
	plantedFunctions       = []string{"stood", "stood$1", "stoodToo", "alsoStood"}
)

// A listener over the representation whose rule is confined, on a package where three functions stand, is called for
// none of them, nor for the function literal inside one: no Function, no instruction, no FunctionExit. It hears the
// synthetic package initializer, the function literal in a variable's initializer and every function that does not
// stand. A listener beside it whose rule is not confined hears every function. Each holds whether the package is
// walked whole or split into shards, where each listener runs once per shard.
func TestAConfinedListenerOverTheRepresentationHearsNoStandingFunction(test *testing.T) {
	test.Parallel()
	for _, shardBytes := range []int{defaultShardBytes, 1} {
		test.Run(fmt.Sprintf("shards of %d bytes", shardBytes), func(test *testing.T) {
			test.Parallel()
			if shardBytes == 1 {
				requireSplitting(test)
			}
			dispatcher := NewIntermediateRepresentation()
			dispatcher.shardBytes = shardBytes
			heard := newHeardCalls()
			confinedName := rule.Namespace + "confined"
			confined := dispatcher.RuleAnalyzer(confinedName, "confined", "records every call",
				heard.listen("confined", true))
			free := dispatcher.RuleAnalyzer(rule.Namespace+"free", "free", "records every call",
				heard.listen("free", true))
			standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), confinedName)
			for _, action := range standing.analyze(test, confined, free).Roots {
				if action.Err != nil {
					test.Fatalf("running %s: %v", action, action.Err)
				}
			}
			if shardBytes == 1 && len(heard.contexts["confined"]) < 2 {
				test.Fatalf("the confined listener ran in %d context, so the package did not split",
					len(heard.contexts["confined"]))
			}
			heardConfined, heardFree := heard.heardBy("confined"), heard.heardBy("free")
			outsideStanding := everyCallOf(neverStandingFunctions...)
			everything := everyCallOf(slices.Concat(neverStandingFunctions, plantedFunctions)...)
			if !slices.Equal(heardConfined, outsideStanding) {
				test.Errorf("the confined listener heard\n%s\nwant every function that does not stand, whole\n%s",
					strings.Join(heardConfined, "\n"), strings.Join(outsideStanding, "\n"))
			}
			if !slices.Equal(heardFree, everything) {
				test.Errorf("the listener that is not confined heard\n%s\nwant every function, whole\n%s",
					strings.Join(heardFree, "\n"), strings.Join(everything, "\n"))
			}
		})
	}
}

// builtFunctions is the name of each function of irPackage that has blocks, declared or a function literal inside
// one, sorted, read off the package itself once a run is over, so nothing here builds one.
func builtFunctions(irPackage *intermediate_representation.Package) []string {
	built := []string{}
	var gather func(functions []*intermediate_representation.Function)
	gather = func(functions []*intermediate_representation.Function) {
		for _, function := range functions {
			if function.Blocks != nil {
				built = append(built, function.Name())
			}
			gather(function.AnonymousFunctions)
		}
	}
	gather(irPackage.Functions)
	slices.Sort(built)
	return built
}

// The dispatcher builds what it delivers and no more. With every started listener confined, a function that stands
// for all of them is never built, and the function literal inside it is never so much as created, while the synthetic
// package initializer, the literal in a variable's initializer and every function that does not stand are built,
// whether the listeners run over the whole package or once per shard. A function standing in a file one started
// listener is not confined on is built, since that listener hears it, and with one listener that is confined nowhere,
// or in a pass that holds no standing bodies at all, the whole package is built, as it always was.
func TestTheDispatcherBuildsNoFunctionThatStandsForEveryStartedListener(test *testing.T) {
	test.Parallel()
	everyFunction := slices.Sorted(slices.Values(slices.Concat(neverStandingFunctions, plantedFunctions)))
	for _, testCase := range []struct {
		name       string
		shardBytes int
		// confined is which of the two listeners' rules the planted standing bodies confine, and withheld the files
		// the host holds no findings of the second rule on, which leave that rule to hear them whole.
		confined []string
		withheld []string
		// keyed is whether the pass holds the planted standing bodies at all.
		keyed bool
		built []string
	}{
		{
			name: "every listener confined", shardBytes: defaultShardBytes, confined: []string{"one", "other"},
			keyed: true, built: slices.Sorted(slices.Values(neverStandingFunctions)),
		},
		{
			name: "every listener confined, split into shards", shardBytes: 1, confined: []string{"one", "other"},
			keyed: true, built: slices.Sorted(slices.Values(neverStandingFunctions)),
		},
		{
			name: "one listener confined on first.go alone", shardBytes: defaultShardBytes,
			confined: []string{"one", "other"}, withheld: []string{"second.go"}, keyed: true,
			built: slices.Sorted(slices.Values(slices.Concat(neverStandingFunctions, []string{"alsoStood"}))),
		},
		{
			name: "one listener not confined", shardBytes: defaultShardBytes, confined: []string{"one"}, keyed: true,
			built: everyFunction,
		},
		{
			name: "no standing bodies in the pass", shardBytes: defaultShardBytes, built: everyFunction,
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if testCase.shardBytes == 1 {
				requireSplitting(test)
			}
			dispatcher := NewIntermediateRepresentation()
			dispatcher.shardBytes = testCase.shardBytes
			heard := newHeardCalls()
			analyzers := []*analysis.Analyzer{}
			confined := []string{}
			for _, name := range []string{"one", "other"} {
				analyzers = append(analyzers, dispatcher.RuleAnalyzer(rule.Namespace+name, name, "records every call",
					heard.listen(name, true)))
				if slices.Contains(testCase.confined, name) {
					confined = append(confined, rule.Namespace+name)
				}
			}
			standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), confined...)
			for _, file := range testCase.withheld {
				standing.withheld[rule.Namespace+"other "+file] = true
			}
			options := &checker.Options{}
			if testCase.keyed {
				options = standing.options()
			}
			graph, analyzeError := checker.Analyze(analyzers, loadFixture(test, "standing"), options)
			if analyzeError != nil {
				test.Fatalf("analyzing the fixture: %v", analyzeError)
			}
			for _, action := range graph.Roots {
				if action.Err != nil {
					test.Fatalf("running %s: %v", action, action.Err)
				}
			}
			if testCase.shardBytes == 1 && len(heard.contexts["one"]) < 2 {
				test.Fatalf("a listener ran in %d context, so the package did not split", len(heard.contexts["one"]))
			}
			if heard.irPackage == nil {
				test.Fatal("no listener was handed a function, so the test read no package")
			}
			if built := builtFunctions(heard.irPackage); !slices.Equal(built, testCase.built) {
				test.Errorf("once the run was over the built functions were\n%s\nwant\n%s", strings.Join(built, "\n"),
					strings.Join(testCase.built, "\n"))
			}
			// What a listener heard is what was built for it: the listener confined everywhere hears no function
			// that stands, and the other every function of a file it is not confined on.
			heardOne, heardOther := heard.heardBy("one"), heard.heardBy("other")
			expectedOne := everyCallOf(everyFunction...)
			if slices.Contains(confined, rule.Namespace+"one") && testCase.keyed {
				expectedOne = everyCallOf(neverStandingFunctions...)
			}
			if !slices.Equal(heardOne, expectedOne) {
				test.Errorf("the first listener heard\n%s\nwant\n%s", strings.Join(heardOne, "\n"),
					strings.Join(expectedOne, "\n"))
			}
			if expectedOther := everyCallOf(testCase.built...); !slices.Equal(heardOther, expectedOther) {
				test.Errorf("the second listener heard\n%s\nwant every built function\n%s",
					strings.Join(heardOther, "\n"), strings.Join(expectedOther, "\n"))
			}
		})
	}
}

// A rule heard over the representation reports the same findings whether its package is walked whole or split into
// shards, with functions standing: a confined rule reporting every source function where it is declared reports its
// own on the functions that do not stand, the function literal in a variable's initializer among them, and the
// host's inside the standing ones, and tells the build what it took, while a rule beside it that is not confined
// reports its own on every function, the standing ones and the literal inside one included, and none of the host's.
func TestARuleOverTheRepresentationReportsTheSameFindingsWholeOrSplit(test *testing.T) {
	test.Parallel()
	reporting := func() *rule.Listener {
		return &rule.Listener{
			FileLocal: true,
			IntermediateRepresentation: &rule.IntermediateRepresentationListener{
				Function: func(context *rule.Context, function *intermediate_representation.Function) {
					if function.Synthetic == "" {
						context.Reportf(function.Pos(), "its own function")
					}
				},
			},
		}
	}
	ownOutside := []string{
		"first.go:6:12 its own function",
		"first.go:27:6 its own function",
		"second.go:10:6 its own function",
	}
	ownInside := []string{
		"first.go:14:6 its own function",
		"first.go:18:11 its own function",
		"first.go:34:6 its own function",
		"second.go:3:6 its own function",
	}
	for _, shardBytes := range []int{defaultShardBytes, 1} {
		test.Run(fmt.Sprintf("shards of %d bytes", shardBytes), func(test *testing.T) {
			test.Parallel()
			if shardBytes == 1 {
				requireSplitting(test)
			}
			dispatcher := NewIntermediateRepresentation()
			dispatcher.shardBytes = shardBytes
			confinedName := rule.Namespace + "confined"
			confined := dispatcher.RuleAnalyzer(confinedName, "confined", "reports every source function", reporting())
			free := dispatcher.RuleAnalyzer(rule.Namespace+"free", "free", "reports every source function",
				reporting())
			standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), confinedName)
			expected := map[*analysis.Analyzer][]string{
				confined: slices.Sorted(slices.Values(slices.Concat(ownOutside, hostsFindingsInStanding))),
				free:     slices.Sorted(slices.Values(slices.Concat(ownOutside, ownInside))),
			}
			for _, action := range standing.analyze(test, confined, free).Roots {
				if action.Err != nil {
					test.Fatalf("running %s: %v", action, action.Err)
				}
				reported := reportedBy(action)
				slices.Sort(reported)
				if !slices.Equal(reported, expected[action.Analyzer]) {
					test.Errorf("%s reported\n%s\nwant\n%s", action.Analyzer.Name, strings.Join(reported, "\n"),
						strings.Join(expected[action.Analyzer], "\n"))
				}
			}
			expectedTold := map[string][2]int{confinedName + " first.go": {2, 3}, confinedName + " second.go": {1, 1}}
			if told := standing.told(); !maps.Equal(told, expectedTold) {
				test.Errorf("the rules told the build they took %v, want the confined rule's alone, %v", told,
					expectedTold)
			}
		})
	}
}
