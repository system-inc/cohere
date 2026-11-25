package test_failure_in_goroutine

import (
	"go/types"
	"maps"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
)

// noAnswers is a store that holds no answer and reloaded nothing (fact_resolver.Store), so a run over it computes
// every answer it is asked and records each, as a run that saves its entries does.
type noAnswers struct{}

func (noAnswers) Answers(*types.Package) *fact_resolver.Answers {
	return nil
}

func (noAnswers) Reloaded(*types.Package) bool {
	return false
}

// The answers a run records, which are what it saves of the kind and what a build compares, hold the kind only for
// functions the rule asked about, each a function of the package the rule was judging: of the nine functions and
// methods the fixtures start as goroutines from their own packages, and none of another package, testing and parity's
// other among them, though both declare functions that stop a test. The kind is not registered, so nothing computes
// it for a function no rule asked about.
//
// The resolver records an answer by its object's objectpath, and an unexported package-level function has none, so
// eight of the nine are asked about and never recorded: only the method's answer is. The test holds that as it is, so
// what a build can compare of the kind is plain here.
func TestARunRecordsTheKindOnlyForTheFunctionsTheRuleAskedAbout(test *testing.T) {
	test.Parallel()
	roots := loadFixtures(test)
	hand := newHeardAnswers(roots[0].Fset, nil)
	resolver := fact_resolver.New(noAnswers{})
	resolver.HoldAnswers(hand)
	options := check.DispatchOptions()
	options.Resolver = resolver
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{Analyzer}, roots, options)
	if analyzeError != nil {
		test.Fatalf("running the rule: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running the rule on %s: %v", action.Package.ID, action.Err)
		}
	}
	if _, isRegistered := fact_resolver.ZeroAnswer(Kind.Name); isRegistered {
		test.Errorf("%s is registered, so every run computes it for every function of every package it saves",
			Kind.Name)
	}
	// named are the paths of the functions asked about that have one, by package, and asked whether each function
	// asked about, by full name, has one.
	named := map[*types.Package][]objectpath.Path{}
	asked := map[string]bool{}
	for _, question := range hand.asked {
		function, isFunction := question.object.(*types.Func)
		if question.kind != Kind.Name || !isFunction {
			continue
		}
		if _, isSeen := asked[function.FullName()]; isSeen {
			continue
		}
		path, pathError := objectpath.For(function)
		asked[function.FullName()] = pathError == nil
		if pathError == nil {
			named[function.Pkg()] = append(named[function.Pkg()], path)
		}
	}
	want := map[string]bool{"(*parity.suite).stops": true, "CheckConcurrentTesting.fn2": false,
		"differences.later": false, "differences.recursive": false, "parity.fails": false,
		"parity.stopsEitherWay": false, "parity.stopsInSecondFile": false, "parity.stopsUnderLineDirective": false,
		"parity.fatalEitherWay": false}
	if !maps.Equal(asked, want) {
		test.Errorf("the rule asked about these functions, each with whether it has an objectpath: %v, want %v", asked,
			want)
	}
	own := map[*types.Package]bool{}
	for _, root := range roots {
		own[root.Types] = true
	}
	packages.Visit(roots, nil, func(reached *packages.Package) {
		if reached.Types == nil {
			return
		}
		recorded := slices.Sorted(maps.Keys(resolver.Recorded(reached.Types)[Kind.Name]))
		slices.Sort(named[reached.Types])
		if !slices.Equal(recorded, named[reached.Types]) {
			test.Errorf("the run recorded %s for %q of %s, want %q, the functions the rule asked about that have an "+
				"objectpath", Kind.Name, recorded, reached.PkgPath, named[reached.Types])
		}
		if len(recorded) > 0 && !own[reached.Types] {
			test.Errorf("the run recorded %s for %q of %s, a package the rule was not judging", Kind.Name, recorded,
				reached.PkgPath)
		}
	})
}
