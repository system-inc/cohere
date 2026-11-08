package checker

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// failedKey stands in for the engine's key, an analyzer that is never run.
var failedKey = &analysis.Analyzer{
	Name: "failed_key",
	Doc:  "keys what a tolerant pass ran past and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		pass.Reportf(pass.Files[0].Package, "the key ran")
		return nil, nil
	},
}

// heardFailures is what one pass of a tolerancePasses analyzer found: whether ResultOf held failedKey, each failed
// prerequisite under it as its name and error, sorted, and the names of the results ResultOf held beside it, sorted.
type heardFailures struct {
	keyed   bool
	failed  []string
	results []string
}

// tolerancePasses records, by analyzer name and package path, what each pass of the analyzers it builds heard.
type tolerancePasses struct {
	mutex sync.Mutex
	heard map[string]heardFailures
}

// analyzer is one named name that requires required and records what its pass heard, learning facts when facts is
// set, so it runs on every dependency too, and failing with failure on a package whose path ends in failsOn, unless
// failsOn is empty.
func (passes *tolerancePasses) analyzer(
	name string,
	facts bool,
	failsOn string,
	required ...*analysis.Analyzer,
) *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name:       name,
		Doc:        name + " records what it heard of its prerequisites on each package",
		Requires:   required,
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			heard := heardFailures{}
			value, keyed := pass.ResultOf[failedKey]
			failed, isMap := value.(map[*analysis.Analyzer]error)
			heard.keyed = keyed && isMap
			for failedAnalyzer, failedError := range failed {
				heard.failed = append(heard.failed, failedAnalyzer.Name+": "+failedError.Error())
			}
			slices.Sort(heard.failed)
			for resultAnalyzer := range pass.ResultOf {
				if resultAnalyzer != failedKey {
					heard.results = append(heard.results, resultAnalyzer.Name)
				}
			}
			slices.Sort(heard.results)
			passes.mutex.Lock()
			passes.heard[pass.Analyzer.Name+" "+pass.Pkg.Path()] = heard
			passes.mutex.Unlock()
			if failsOn != "" && strings.HasSuffix(pass.Pkg.Path(), failsOn) {
				return nil, fmt.Errorf("planted failure of %s on %s", name, pass.Pkg.Path())
			}
			return name, nil
		},
	}
	if facts {
		analyzer.FactTypes = []analysis.Fact{new(declaredFact)}
	}
	return analyzer
}

// Under Options.Failed and Options.Tolerant, an action Tolerant names runs past a prerequisite that failed on its own
// package: tolerant runs on upper though failing failed there, without failing's input, and hears failing's error
// under the key, while strict, which requires failing too and is not tolerant, fails as upstream's does. A failed
// vertical prerequisite still fails a tolerant action: vertical fails on lower, so vertical on upper never runs.
// vertical on lower, tolerant with nothing failed, holds the key's nil map, which still reads as a map. With either
// option unset, every action fails with a failed prerequisite, as upstream's does, and no pass holds the key.
func TestATolerantActionRunsPastAFailedPrerequisiteOnItsOwnPackage(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":         "module example.com/tolerant\n\ngo 1.21\n",
		"lower/lower.go": "package lower\n\nfunc Lower() {}\n",
		"upper/upper.go": "package upper\n\nimport \"example.com/tolerant/lower\"\n\nfunc Upper() { lower.Lower() }\n",
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

	const lower = "example.com/tolerant/lower"
	const upper = "example.com/tolerant/upper"
	for _, configuration := range []struct {
		name        string
		setFailed   bool
		setTolerant bool
		// runsPast is whether a tolerant action runs past what failed, which it does only with both options set.
		runsPast bool
	}{
		{name: "both set", setFailed: true, setTolerant: true, runsPast: true},
		{name: "both unset"},
		{name: "the key alone", setFailed: true},
		{name: "the predicate alone", setTolerant: true},
	} {
		passes := &tolerancePasses{heard: map[string]heardFailures{}}
		failing := passes.analyzer("failing", false, "upper")
		healthy := passes.analyzer("healthy", false, "")
		tolerant := passes.analyzer("tolerant", false, "", failing, healthy)
		strict := passes.analyzer("strict", false, "", failing)
		vertical := passes.analyzer("vertical", true, "lower", healthy)
		options := &Options{}
		if configuration.setFailed {
			options.Failed = failedKey
		}
		if configuration.setTolerant {
			options.Tolerant = func(analyzer *analysis.Analyzer) bool {
				return analyzer == tolerant || analyzer == vertical
			}
		}
		graph, analyzeError := Analyze([]*analysis.Analyzer{tolerant, strict, vertical}, loaded.Roots, options)
		if analyzeError != nil {
			test.Fatalf("%s: analyzing: %v", configuration.name, analyzeError)
		}

		failedOnUpper := "failed prerequisites: failing@" + upper
		expectedErrors := map[string]string{
			"tolerant": failedOnUpper,
			"strict":   failedOnUpper,
			"vertical": "failed prerequisites: vertical@" + lower,
		}
		expectedHeard := map[string]heardFailures{
			"failing " + upper:  {},
			"healthy " + upper:  {},
			"healthy " + lower:  {},
			"vertical " + lower: {results: []string{"healthy"}},
		}
		if configuration.runsPast {
			expectedErrors["tolerant"] = ""
			expectedHeard["tolerant "+upper] = heardFailures{keyed: true,
				failed: []string{"failing: planted failure of failing on " + upper}, results: []string{"healthy"}}
			expectedHeard["vertical "+lower] = heardFailures{keyed: true, results: []string{"healthy"}}
		}
		for _, root := range graph.Roots {
			actual := ""
			if root.Err != nil {
				actual = root.Err.Error()
			}
			if actual != expectedErrors[root.Analyzer.Name] || len(root.Diagnostics) > 0 {
				test.Errorf("%s: %s ended with %q and %+v, where %q and no diagnostic were expected",
					configuration.name, root, actual, root.Diagnostics, expectedErrors[root.Analyzer.Name])
			}
		}
		equal := func(left, right heardFailures) bool {
			return left.keyed == right.keyed && slices.Equal(left.failed, right.failed) &&
				slices.Equal(left.results, right.results)
		}
		if !maps.EqualFunc(passes.heard, expectedHeard, equal) {
			test.Errorf("%s: the passes heard %+v, where %+v was expected", configuration.name, passes.heard,
				expectedHeard)
		}
	}
}
