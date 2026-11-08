package checker

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// dependentsKey stands in for the engine's key, an analyzer that is never run.
var dependentsKey = &analysis.Analyzer{
	Name: "dependents_key",
	Doc:  "keys the set of a pass's dependents and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		pass.Reportf(pass.Files[0].Package, "the key ran")
		return nil, nil
	},
}

// heardDependents is what one pass found under dependentsKey: whether ResultOf held it, and the names of the
// analyzers in the set, sorted.
type heardDependents struct {
	present bool
	names   []string
}

// dependentsRecorder is an analyzer that records, by package path, what each of its passes finds under dependentsKey
// and how many results ResultOf holds beside it.
type dependentsRecorder struct {
	mutex    sync.Mutex
	heard    map[string]heardDependents
	others   map[string]int
	analyzer *analysis.Analyzer
}

// newDependentsRecorder is a recorder whose analyzer is named name and requires required, and learns facts, so it runs
// on every dependency of the packages it is given, when facts is set.
func newDependentsRecorder(name string, facts bool, required ...*analysis.Analyzer) *dependentsRecorder {
	recorder := &dependentsRecorder{heard: map[string]heardDependents{}, others: map[string]int{}}
	recorder.analyzer = &analysis.Analyzer{
		Name:     name,
		Doc:      name + " records the set of its dependents on each package",
		Requires: required,
		Run: func(pass *analysis.Pass) (any, error) {
			value, present := pass.ResultOf[dependentsKey]
			set, isSet := value.(map[*analysis.Analyzer]bool)
			heard := heardDependents{present: present && isSet}
			for analyzer, included := range set {
				if included {
					heard.names = append(heard.names, analyzer.Name)
				}
			}
			slices.Sort(heard.names)
			recorder.mutex.Lock()
			defer recorder.mutex.Unlock()
			recorder.heard[pass.Pkg.Path()] = heard
			recorder.others[pass.Pkg.Path()] = len(pass.ResultOf)
			if present {
				recorder.others[pass.Pkg.Path()]--
			}
			return nil, nil
		},
	}
	if facts {
		recorder.analyzer.FactTypes = []analysis.Fact{new(declaredFact)}
	}
	return recorder
}

// Under Options.Dependents, a pass's set holds exactly the analyzers whose actions on its own package require it:
// served, which learns facts and so runs on lower too, hears its two readers on upper and nobody on lower, where only
// its own action on upper, a vertical edge, requires it. An analyzer that requires served but is not in the graph is
// in no set, and a reader nothing requires holds an empty set, which is still a set. Unset, no pass holds the key, and
// ResultOf holds only what each analyzer requires.
func TestAPassHoldsExactlyItsSamePackageDependentsUnderDependents(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":         "module example.com/dependents\n\ngo 1.21\n",
		"lower/lower.go": "package lower\n\nfunc Lower() {}\n",
		"upper/upper.go": "package upper\n\nimport \"example.com/dependents/lower\"\n\nfunc Upper() { lower.Lower() }\n",
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

	for _, gathered := range []bool{true, false} {
		served := newDependentsRecorder("served", true)
		firstReader := newDependentsRecorder("first_reader", false, served.analyzer)
		secondReader := newDependentsRecorder("second_reader", false, served.analyzer)
		// absent requires served too, and is never given, so it is in no graph.
		absent := newDependentsRecorder("absent", false, served.analyzer)
		options := &Options{}
		if gathered {
			options.Dependents = dependentsKey
		}
		graph, analyzeError := Analyze([]*analysis.Analyzer{firstReader.analyzer, secondReader.analyzer},
			loaded.Roots, options)
		if analyzeError != nil {
			test.Fatalf("analyzing: %v", analyzeError)
		}
		for _, root := range graph.Roots {
			if root.Err != nil || len(root.Diagnostics) > 0 {
				test.Fatalf("%s: %v %+v", root, root.Err, root.Diagnostics)
			}
		}

		expected := map[string]heardDependents{
			"example.com/dependents/lower": {},
			"example.com/dependents/upper": {},
		}
		expectedReader := map[string]heardDependents{"example.com/dependents/upper": {}}
		if gathered {
			expected = map[string]heardDependents{
				"example.com/dependents/lower": {present: true},
				"example.com/dependents/upper": {present: true, names: []string{"first_reader", "second_reader"}},
			}
			expectedReader = map[string]heardDependents{"example.com/dependents/upper": {present: true}}
		}
		equal := func(left, right heardDependents) bool {
			return left.present == right.present && slices.Equal(left.names, right.names)
		}
		if !maps.EqualFunc(served.heard, expected, equal) {
			test.Errorf("with Dependents set %t, served heard %+v, where %+v was expected", gathered, served.heard,
				expected)
		}
		for _, reader := range []*dependentsRecorder{firstReader, secondReader} {
			if !maps.EqualFunc(reader.heard, expectedReader, equal) {
				test.Errorf("with Dependents set %t, %s heard %+v, where %+v was expected", gathered,
					reader.analyzer.Name, reader.heard, expectedReader)
			}
			if reader.others["example.com/dependents/upper"] != 1 {
				test.Errorf("with Dependents set %t, %s's ResultOf held %d results beside the key, where served's "+
					"alone was expected", gathered, reader.analyzer.Name, reader.others["example.com/dependents/upper"])
			}
		}
		if served.others["example.com/dependents/lower"] != 0 || served.others["example.com/dependents/upper"] != 0 {
			test.Errorf("with Dependents set %t, served's ResultOf held %v results beside the key, where it requires "+
				"nothing", gathered, served.others)
		}
		if len(absent.heard) != 0 {
			test.Errorf("absent, never given, ran on %v", slices.Sorted(maps.Keys(absent.heard)))
		}
	}
}
