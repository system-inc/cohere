package checker

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// dependentsRuns counts, by analyzer name and package path, the runs of the analyzers it builds.
type dependentsRuns struct {
	mutex sync.Mutex
	runs  map[string]int
}

// analyzer is one named name that requires required, counts its runs, and learns facts when facts is set, so it runs
// on every dependency too.
func (counted *dependentsRuns) analyzer(name string, facts bool, required ...*analysis.Analyzer) *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name:       name,
		Doc:        name + " counts its runs on each package",
		Requires:   required,
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			counted.mutex.Lock()
			defer counted.mutex.Unlock()
			counted.runs[pass.Analyzer.Name+" "+pass.Pkg.Path()]++
			return name, nil
		},
	}
	if facts {
		analyzer.FactTypes = []analysis.Fact{new(declaredFact)}
	}
	return analyzer
}

// Under Options.ForDependents, an edge it names is kept only where an action requiring the first analyzer on the
// package requires the second too. served stands in for a dispatcher. It requires walked and kept, whose results its
// run reads, kept being one that reader requires beside it, and leftOut, which nothing in the graph requires beside
// it and which learns facts and requires below, as staticcheck's purity does buildir. So on upper served, walked,
// kept, reader and observer run once each, and leftOut and below run nowhere, upper and lower alike; and walked's
// Result, read by served alone once leftOut is in no group, is collected as served finishes, which observer, running
// after it, sees. Unset, every requirement is an edge, as upstream's is, and leftOut and below run on both packages.
func TestAnEdgeForDependentsIsKeptOnlyWhereADependentRequiresItToo(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":         "module example.com/fordependents\n\ngo 1.21\n",
		"lower/lower.go": "package lower\n\nfunc Lower() {}\n",
		"upper/upper.go": "package upper\n\nimport \"example.com/fordependents/lower\"\n\n" +
			"func Upper() { lower.Lower() }\n",
	} {
		path := filepath.Join(directory, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	loaded, loadError := load.Packages(directory, []string{"./upper"}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Roots) != 1 || len(loaded.Roots[0].Errors) > 0 {
		test.Fatalf("loading the fixture: %v, %+v", loadError, loaded)
	}

	const lower = "example.com/fordependents/lower"
	const upper = "example.com/fordependents/upper"
	// One configuration after the other, not as parallel subtests, since both name walked alike in heavyResults.
	for _, set := range []bool{true, false} {
		counted := &dependentsRuns{runs: map[string]int{}}
		walked := heavyAnalyzer("for_dependents_walked")
		below := counted.analyzer("below", false)
		leftOut := counted.analyzer("left_out", true, below, walked)
		kept := counted.analyzer("kept", false)
		served := counted.analyzer("served", false, walked, kept, leftOut)
		served.Run = func(pass *analysis.Pass) (any, error) {
			// An error, which fails reader and observer, the roots, so the graph's end shows it.
			_, isHeavy := pass.ResultOf[walked].(*heavyResult)
			_, isKept := pass.ResultOf[kept].(string)
			if !isHeavy || !isKept {
				return nil, fmt.Errorf("served was handed walked %t and kept %t, where both were expected", isHeavy,
					isKept)
			}
			counted.mutex.Lock()
			defer counted.mutex.Unlock()
			counted.runs["served "+pass.Pkg.Path()]++
			return "served", nil
		}
		reader := counted.analyzer("reader", false, served, kept)
		observer := &analysis.Analyzer{
			Name:       "for_dependents_observer",
			Doc:        "reports whether walked's result is gone once served, its only reader, finished",
			Requires:   []*analysis.Analyzer{served},
			ResultType: reflect.TypeFor[bool](),
			Run: func(pass *analysis.Pass) (any, error) {
				return collected("for_dependents_walked"), nil
			},
		}
		options := &Options{}
		if set {
			options.ForDependents = func(analyzer *analysis.Analyzer, required *analysis.Analyzer) bool {
				return analyzer == served && required != walked
			}
		}
		graph, analyzeError := Analyze([]*analysis.Analyzer{reader, observer}, loaded.Roots, options)
		if analyzeError != nil {
			test.Fatalf("with ForDependents set %t: analyzing: %v", set, analyzeError)
		}
		expected := map[string]int{"served " + upper: 1, "kept " + upper: 1, "reader " + upper: 1}
		if !set {
			for _, name := range []string{"left_out", "below"} {
				expected[name+" "+upper], expected[name+" "+lower] = 1, 1
			}
		}
		for _, root := range graph.Roots {
			if root.Err != nil || len(root.Diagnostics) > 0 {
				test.Errorf("with ForDependents set %t: %s ended with %v and %+v, where neither was expected", set,
					root, root.Err, root.Diagnostics)
			}
			if released, isBool := root.Result.(bool); set && root.Analyzer == observer && (!isBool || !released) {
				test.Errorf("with ForDependents set: walked's result was held once served, its only reader in the "+
					"graph, finished: %v", root.Result)
			}
		}
		if !maps.Equal(counted.runs, expected) {
			test.Errorf("with ForDependents set %t: the analyzers ran %v, where %v was expected", set, counted.runs,
				expected)
		}
		runtime.KeepAlive(graph)
	}
}
