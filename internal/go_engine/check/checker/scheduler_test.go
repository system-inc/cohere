package checker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// writeModule writes a module named example.com/<module> with files, by path below the module root, and loads
// patterns from it.
func writeModule(test *testing.T, module string, files map[string]string, patterns ...string) []*packages.Package {
	test.Helper()
	directory := test.TempDir()
	goModule := "module example.com/" + module + "\n\ngo 1.21\n"
	if writeError := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(goModule), 0o644); writeError != nil {
		test.Fatalf("writing go.mod: %v", writeError)
	}
	for name, contents := range files {
		path := filepath.Join(directory, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	loaded, loadError := load.Packages(directory, patterns, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("loading the fixture: %s: %v", root.ID, root.Errors)
		}
	}
	return loaded.Roots
}

// packageOf is a package declaration named name padded with a comment of padding bytes, so its file's size is
// what a test orders by.
func packageOf(name string, padding int) string {
	return "package " + name + "\n\n// " + strings.Repeat("x", padding) + "\n\nfunc " + strings.ToUpper(name[:1]) +
		name[1:] + "() {}\n"
}

// groupObserver records each package group as it starts and finishes, for Options.observeGroup, which the scheduler
// calls from several goroutines at once.
type groupObserver struct {
	lock       sync.Mutex
	running    int
	mostAtOnce int
	started    []string
	finished   map[string]bool
}

// observe is Options.observeGroup.
func (observer *groupObserver) observe(observedPackage *packages.Package, running bool) {
	observer.lock.Lock()
	defer observer.lock.Unlock()
	if !running {
		observer.running--
		observer.finished[observedPackage.Name] = true
		return
	}
	observer.running++
	observer.mostAtOnce = max(observer.mostAtOnce, observer.running)
	observer.started = append(observer.started, observedPackage.Name)
}

// hasFinished is whether the group of the package named name has finished.
func (observer *groupObserver) hasFinished(name string) bool {
	observer.lock.Lock()
	defer observer.lock.Unlock()
	return observer.finished[name]
}

// slowAnalyzer holds each package for a moment, so groups that may run at once do.
var slowAnalyzer = &analysis.Analyzer{
	Name: "slow",
	Doc:  "slow takes a moment on every package",
	Run: func(pass *analysis.Pass) (any, error) {
		time.Sleep(20 * time.Millisecond)
		return nil, nil
	},
}

// With two slots, no more than two package groups ever run at once, however many are ready together.
func TestNoMoreGroupsRunAtOnceThanThereAreSlots(test *testing.T) {
	test.Parallel()

	files := map[string]string{}
	for _, name := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot"} {
		files[name+"/"+name+".go"] = packageOf(name, 10)
	}
	roots := writeModule(test, "bound", files, "./...")
	observer := &groupObserver{finished: map[string]bool{}}
	graph, analyzeError := Analyze([]*analysis.Analyzer{slowAnalyzer}, roots,
		&Options{slots: 2, observeGroup: observer.observe})
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	if len(observer.started) != 6 || len(graph.Roots) != 6 {
		test.Fatalf("want 6 groups run, got %v", observer.started)
	}
	if observer.mostAtOnce > 2 {
		test.Errorf("want at most 2 groups at once with 2 slots, got %d", observer.mostAtOnce)
	}
}

// quietAnalyzer runs on every package and does nothing, so only the scheduler orders the groups.
var quietAnalyzer = &analysis.Analyzer{
	Name: "quiet",
	Doc:  "quiet reports nothing",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, nil
	},
}

// With one slot, independent packages start largest first. Their IDs, and so the order the graph holds them in, run
// smallest to largest, so neither queue order nor smallest first passes.
func TestTheLargestReadyGroupStartsFirst(test *testing.T) {
	test.Parallel()

	files := map[string]string{}
	for index, name := range []string{"alpha", "bravo", "charlie", "delta"} {
		files[name+"/"+name+".go"] = packageOf(name, 100*(index+1))
	}
	roots := writeModule(test, "order", files, "./...")
	observer := &groupObserver{finished: map[string]bool{}}
	if _, analyzeError := Analyze([]*analysis.Analyzer{quietAnalyzer}, roots,
		&Options{slots: 1, observeGroup: observer.observe}); analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	if want := []string{"delta", "charlie", "bravo", "alpha"}; !slices.Equal(observer.started, want) {
		test.Errorf("want the groups started largest first, %v, got %v", want, observer.started)
	}
}

// A package's group starts only once every action it inherits facts from has finished in its own group: upper
// imports lower and is the larger, so with one slot a group made ready one prerequisite early would start upper
// first, and its pass would find lower's group unfinished.
func TestAGroupStartsOnlyOnceEveryActionItInheritsFactsFromHasFinished(test *testing.T) {
	test.Parallel()

	roots := writeModule(test, "readiness", map[string]string{
		"lower/lower.go": packageOf("lower", 10),
		"upper/upper.go": "package upper\n\nimport \"example.com/readiness/lower\"\n\n// " + strings.Repeat("x", 1000) +
			"\n\nfunc Upper() { lower.Lower() }\n",
	}, "./upper")
	observer := &groupObserver{finished: map[string]bool{}}
	var lowerUnfinished bool
	var upperRan bool
	readiness := &analysis.Analyzer{
		Name:      "readiness",
		Doc:       "readiness asks, from upper's pass, whether lower's group has finished",
		FactTypes: []analysis.Fact{new(declaredFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			if pass.Pkg.Name() == "upper" {
				upperRan = true
				lowerUnfinished = !observer.hasFinished("lower")
			}
			return nil, nil
		},
	}
	if _, analyzeError := Analyze([]*analysis.Analyzer{readiness}, roots,
		&Options{slots: 1, observeGroup: observer.observe}); analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	if !upperRan {
		test.Fatalf("want upper analyzed, and its pass never ran")
	}
	if lowerUnfinished {
		test.Errorf("want lower's group finished before upper's pass, and it had not, with groups started %v",
			observer.started)
	}
}

// A group waits on the prerequisites of every action in it, not only its first. Upper imports lower and is the
// larger. Its group holds quietAnalyzer's action, which has no prerequisite on another package, and then the fact
// analyzer's, which inherits from lower's. Upper's actions run in the order Analyze grouped them, the depth-first
// postorder of the roots, which it builds analyzer by analyzer, so quiet's action comes first; the test asserts that
// order from the root's group rather than trusting it. With one slot, a group that waited on its first action alone
// would start upper before lower, and the fact analyzer's pass would find lower's group unfinished. A deadline turns
// a scheduler that never returns into a failure rather than a stalled run.
func TestAGroupWaitsOnEveryActionsPrerequisitesNotOnlyItsFirsts(test *testing.T) {
	test.Parallel()

	roots := writeModule(test, "every", map[string]string{
		"lower/lower.go": packageOf("lower", 10),
		"upper/upper.go": "package upper\n\nimport \"example.com/every/lower\"\n\n// " + strings.Repeat("x", 1000) +
			"\n\nfunc Upper() { lower.Lower() }\n",
	}, "./upper")
	observer := &groupObserver{finished: map[string]bool{}}
	var lowerUnfinished bool
	var upperRan bool
	facts := &analysis.Analyzer{
		Name: "facts",
		Doc: "facts reports upper's call to lower's function and asks, from upper's pass, whether lower's group " +
			"has finished",
		FactTypes: []analysis.Fact{new(declaredFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			if pass.Pkg.Name() == "upper" {
				upperRan = true
				lowerUnfinished = !observer.hasFinished("lower")
			}
			return declaredAnalyzer.Run(pass)
		},
	}
	analyzed := make(chan *Graph, 1)
	go func() {
		graph, analyzeError := Analyze([]*analysis.Analyzer{quietAnalyzer, facts}, roots,
			&Options{slots: 1, observeGroup: observer.observe})
		if analyzeError != nil {
			test.Errorf("analyzing: %v", analyzeError)
		}
		analyzed <- graph
	}()
	var graph *Graph
	select {
	case graph = <-analyzed:
	case <-time.After(30 * time.Second):
		test.Fatalf("want the graph analyzed, and it had not returned after 30 seconds, with groups started %v",
			observer.started)
	}
	if graph == nil {
		test.Fatalf("want a graph, got none")
	}
	var factsOnUpper *Action
	for _, root := range graph.Roots {
		if root.Analyzer == facts {
			factsOnUpper = root
		}
	}
	if factsOnUpper == nil {
		test.Fatalf("want the fact analyzer's action on upper among the roots, got %v", graph.Roots)
	}
	if order := factsOnUpper.syntax.actions; len(order) != 2 || order[0].Analyzer != quietAnalyzer ||
		order[1].Analyzer != facts {
		test.Fatalf("want upper's group to hold quiet's action, then the fact analyzer's, got %v", order)
	}
	if !upperRan {
		test.Fatalf("want upper analyzed, and the fact analyzer's pass on it never ran")
	}
	if factsOnUpper.Err != nil || len(factsOnUpper.Diagnostics) != 1 ||
		factsOnUpper.Diagnostics[0].Message != "calls Lower, which a dependency declared" {
		test.Errorf("want one finding on upper from lower's fact, got %v %+v", factsOnUpper.Err,
			factsOnUpper.Diagnostics)
	}
	if lowerUnfinished {
		test.Errorf("want lower's group finished in its own slot before upper's fact pass, and it had not, with "+
			"groups started %v", observer.started)
	}
}
