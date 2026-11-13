package checker

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// Each analyzer's required fact analyzers are computed once an Analyze however many actions on packages restored from
// the host's run ask for them (#tb538na), and are what factAnalyzersRequired answers. root requires learner, which
// learns facts, and helper, which learns none and requires deeper, which does: on each of three restored packages,
// root's action asks for root's, learner's and deeper's ask for theirs, and learner's and deeper's ask again on what
// each package imports, so three analyzers are asked many times and each is computed once. Each root action's
// prerequisites are the unmemoized answer's, in its order.
func TestEachAnalyzersRequiredFactsAreComputedOnceAnAnalyze(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":           "module example.com/requiredfacts\n\ngo 1.21\n",
		"lower/lower.go":   "package lower\n\nfunc Lower() {}\n",
		"middle/middle.go": "package middle\n\nimport \"example.com/requiredfacts/lower\"\n\nfunc Middle() { lower.Lower() }\n",
		"upper/upper.go":   "package upper\n\nimport \"example.com/requiredfacts/middle\"\n\nfunc Upper() { middle.Middle() }\n",
	} {
		path := filepath.Join(directory, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	loaded, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Roots) != 3 {
		test.Fatalf("loading the fixture: %v, %+v", loadError, loaded)
	}

	// fact is the type an analyzer learns, nil for one that learns none: no two analyzers may learn one type.
	named := func(name string, fact analysis.Fact, required ...*analysis.Analyzer) *analysis.Analyzer {
		analyzer := &analysis.Analyzer{
			Name:     name,
			Doc:      name + " is one link of the requirements under test",
			Requires: required,
			Run:      func(*analysis.Pass) (any, error) { return nil, nil },
		}
		if fact != nil {
			analyzer.FactTypes = []analysis.Fact{fact}
		}
		return analyzer
	}
	deeper := named("deeper", new(totalFact))
	helper := named("helper", nil, deeper)
	learner := named("learner", new(declaredFact))
	root := named("root", nil, learner, helper)

	restored := func(*packages.Package) bool { return true }
	graph, analyzeError := Analyze([]*analysis.Analyzer{root}, loaded.Roots, &Options{
		Restored:           restored,
		Restore:            func(*Action) error { return nil },
		FactsBelowRestored: restored,
	})
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}

	asked := []*analysis.Analyzer{root, learner, deeper}
	if graph.requiredFacts.computed != len(asked) {
		test.Errorf("the required fact analyzers were computed %d times, want once for each of %d analyzers",
			graph.requiredFacts.computed, len(asked))
	}
	if len(graph.requiredFacts.of) != len(asked) {
		test.Errorf("the graph read the required fact analyzers of %d analyzers, want %d", len(graph.requiredFacts.of),
			len(asked))
	}
	for _, analyzer := range asked {
		kept, known := graph.requiredFacts.of[analyzer]
		if !known {
			test.Errorf("%s's required fact analyzers were never read", analyzer.Name)
			continue
		}
		if unmemoized := factAnalyzersRequired(analyzer); !slices.Equal(kept, unmemoized) {
			test.Errorf("%s's required fact analyzers are %v, unmemoized %v", analyzer.Name, kept, unmemoized)
		}
	}
	// The answer is root's two learners, in the order factAnalyzersRequired reads them, so the check above compares
	// something.
	if wanted := []*analysis.Analyzer{learner, deeper}; !slices.Equal(factAnalyzersRequired(root), wanted) {
		test.Errorf("root requires the fact analyzers %v, want learner and deeper", factAnalyzersRequired(root))
	}
	if len(graph.Roots) != len(loaded.Roots) {
		test.Fatalf("the graph has %d roots, want one for each of %d packages", len(graph.Roots), len(loaded.Roots))
	}
	for _, action := range graph.Roots {
		var prerequisites []*analysis.Analyzer
		for _, prerequisite := range action.Deps {
			if prerequisite.Package != action.Package {
				test.Errorf("%s depends on %s, on another package", action, prerequisite)
			}
			prerequisites = append(prerequisites, prerequisite.Analyzer)
		}
		if !reflect.DeepEqual(prerequisites, factAnalyzersRequired(root)) {
			test.Errorf("%s depends on %v, want the unmemoized %v", action, prerequisites,
				factAnalyzersRequired(root))
		}
	}
}
