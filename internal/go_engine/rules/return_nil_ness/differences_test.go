package return_nil_ness

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/nilness"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// adjudicatedDifference is what each side exports on one function of the differences fixture, "" for no fact.
type adjudicatedDifference struct {
	original string
	port     string
}

// adjudicatedDifferences is every function of the differences fixture where the two part, with both answers.
var adjudicatedDifferences = map[string]adjudicatedDifference{
	// Staticcheck's processPhis sets outer's phi after inner's, so outer reads the inner just loaded, which may be
	// nil. The port reads both edges first, so outer is the inner the loop dereferenced.
	"InnerFirst": {original: "", port: "nilness: [{MaybeNil NeverNil}]"},
}

// The differences fixture marks what the port exports on both orders of the loop: the same fact either way, since
// it reads every edge into a block before it sets any of the block's phis.
func TestPortReadsPhisTheSameInEitherOrder(test *testing.T) {
	test.Parallel()
	analysistest.Run(test, analysistest.TestData(), Analyzer, "differences")
}

// Over the differences fixture the two part only where adjudicatedDifferences records it, each side giving the answer
// recorded, and agree on every other function.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	// The fixture is a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, []string{"differences"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{nilness.Analysis, Analyzer}, loaded.Roots,
		&checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	exported := map[*analysis.Analyzer]map[string]string{nilness.Analysis: {}, Analyzer: {}}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, objectFact := range action.AllObjectFacts() {
			if objectFact.Object.Pkg() == action.Package.Types {
				exported[action.Analyzer][objectFact.Object.Name()] = fmt.Sprint(objectFact.Fact)
			}
		}
	}
	names := slices.Sorted(maps.Keys(exported[nilness.Analysis]))
	names = append(names, slices.Sorted(maps.Keys(exported[Analyzer]))...)
	names = append(names, slices.Sorted(maps.Keys(adjudicatedDifferences))...)
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		expected, adjudicated := adjudicatedDifferences[name]
		if !adjudicated {
			expected = adjudicatedDifference{original: exported[nilness.Analysis][name],
				port: exported[nilness.Analysis][name]}
		}
		if exported[nilness.Analysis][name] != expected.original || exported[Analyzer][name] != expected.port {
			test.Errorf("on %s the original exported %q and the port %q, where %q and %q were expected", name,
				exported[nilness.Analysis][name], exported[Analyzer][name], expected.original, expected.port)
		}
	}
}
