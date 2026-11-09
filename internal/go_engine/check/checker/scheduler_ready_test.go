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

// lowerAndUpper is a module where upper imports lower, both roots, loaded whole before the test analyzes them.
func lowerAndUpper(test *testing.T, module string) []*packages.Package {
	test.Helper()
	return writeModule(test, module, map[string]string{
		"lower/lower.go": packageOf("lower", 10),
		"upper/upper.go": "package upper\n\nimport \"example.com/" + module + "/lower\"\n\n" +
			"func Upper() { lower.Lower() }\n",
	}, "./...")
}

// gates are one channel per package below roots, by name, closed by the test to make the package ready.
func gates(roots []*packages.Package) (map[string]chan struct{}, func(*packages.Package) <-chan struct{}) {
	byName := map[string]chan struct{}{}
	packages.Visit(roots, nil, func(visited *packages.Package) { byName[visited.Name] = make(chan struct{}) })
	return byName, func(asked *packages.Package) <-chan struct{} { return byName[asked.Name] }
}

// No action on a package starts before its package is ready: lower and upper are each held until the test closes
// its gate, lower first, and each pass asks whether its own gate was closed when it ran. A graph that ignored
// readiness would start lower at once, upper as soon as lower's facts were in, and both passes would find their gates
// still shut.
func TestNoActionStartsBeforeItsPackageIsReady(test *testing.T) {
	test.Parallel()

	roots := lowerAndUpper(test, "gated")
	byName, ready := gates(roots)
	var lock sync.Mutex
	var early []string
	var ran []string
	gated := &analysis.Analyzer{
		Name:      "gated",
		Doc:       "gated asks, from each pass, whether its package's gate was closed",
		FactTypes: []analysis.Fact{new(declaredFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			lock.Lock()
			defer lock.Unlock()
			ran = append(ran, pass.Pkg.Name())
			select {
			case <-byName[pass.Pkg.Name()]:
			default:
				early = append(early, pass.Pkg.Name())
			}
			return nil, nil
		},
	}
	analyzed := make(chan error, 1)
	go func() {
		_, analyzeError := Analyze([]*analysis.Analyzer{quietAnalyzer, gated}, roots, &Options{Ready: ready})
		analyzed <- analyzeError
	}()
	// Long enough for a graph that ignored the gates to have run both packages.
	time.Sleep(100 * time.Millisecond)
	close(byName["lower"])
	time.Sleep(100 * time.Millisecond)
	close(byName["upper"])
	select {
	case analyzeError := <-analyzed:
		if analyzeError != nil {
			test.Fatalf("analyzing: %v", analyzeError)
		}
	case <-time.After(30 * time.Second):
		test.Fatalf("want the graph analyzed once both gates closed, and it had not returned after 30 seconds")
	}
	slices.Sort(ran)
	if !slices.Equal(ran, []string{"lower", "upper"}) {
		test.Errorf("want gated run on lower and upper, got %v", ran)
	}
	if len(early) > 0 {
		test.Errorf("want no pass before its package was ready, and %v ran early", early)
	}
}

// An abandoned graph starts no package that has not started, and returns once those running finish, though a
// package it waits on is never ready: lower runs, then the graph is abandoned with upper still held, and upper never
// runs.
func TestAnAbandonedGraphStartsNothingMoreAndReturns(test *testing.T) {
	test.Parallel()

	roots := lowerAndUpper(test, "abandoned")
	byName, ready := gates(roots)
	close(byName["lower"])
	defer close(byName["upper"])
	observer := &groupObserver{finished: map[string]bool{}}
	abandon := make(chan struct{})
	analyzed := make(chan *Graph, 1)
	go func() {
		graph, analyzeError := Analyze([]*analysis.Analyzer{quietAnalyzer}, roots,
			&Options{Ready: ready, Abandon: abandon, observeGroup: observer.observe})
		if analyzeError != nil {
			test.Errorf("analyzing: %v", analyzeError)
		}
		analyzed <- graph
	}()
	deadline := time.Now().Add(30 * time.Second)
	for !observer.hasFinished("lower") {
		if time.Now().After(deadline) {
			test.Fatalf("want lower analyzed, and it had not finished after 30 seconds")
		}
		time.Sleep(time.Millisecond)
	}
	close(abandon)
	select {
	case <-analyzed:
	case <-time.After(30 * time.Second):
		test.Fatalf("want the abandoned graph to return, and it had not after 30 seconds")
	}
	observer.lock.Lock()
	defer observer.lock.Unlock()
	if !slices.Equal(observer.started, []string{"lower"}) {
		test.Errorf("want only lower started, got %v", observer.started)
	}
}

// Analyzing beside the load, each pass meets its package type-checked whole: three packages in a line, each long
// enough that its check takes a moment, are analyzed by a fact analyzer, so on every package, started as the load
// begins and gated by the load's own readiness, and each pass asks whether its package's types were complete and its
// syntax and type information there. A graph that did not wait for the load would meet a package before its check.
func TestAPassBesideTheLoadMeetsItsPackageTypeCheckedWhole(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	body := strings.Repeat("\tvalue = value*31 + len(text)\n", 3000)
	files := map[string]string{
		"go.mod": "module example.com/whole\n\ngo 1.21\n",
		"bottom/bottom.go": "package bottom\n\n// Bottom folds text.\nfunc Bottom(text string) int {\n\tvalue := 0\n" +
			body + "\treturn value\n}\n",
		"middle/middle.go": "package middle\n\nimport \"example.com/whole/bottom\"\n\n" +
			"// Middle folds text again.\nfunc Middle(text string) int {\n\tvalue := bottom.Bottom(text)\n" + body +
			"\treturn value\n}\n",
		"top/top.go": "package top\n\nimport \"example.com/whole/middle\"\n\n" +
			"// Top folds text once more.\nfunc Top(text string) int {\n\tvalue := middle.Middle(text)\n" + body +
			"\treturn value\n}\n",
	}
	for name, contents := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	var lock sync.Mutex
	var partial []string
	var ran []string
	whole := &analysis.Analyzer{
		Name:      "whole",
		Doc:       "whole asks, from each pass, whether its package was type-checked whole",
		FactTypes: []analysis.Fact{new(declaredFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			checked := pass.Pkg.Complete() && pass.TypesInfo != nil && len(pass.TypesInfo.Defs) > 0 &&
				len(pass.TypesInfo.Types) > 0 && len(pass.Files) > 0
			lock.Lock()
			defer lock.Unlock()
			ran = append(ran, pass.Pkg.Path())
			if !checked {
				partial = append(partial, pass.Pkg.Path())
			}
			return nil, nil
		},
	}
	analyzed := make(chan error, 1)
	loaded, loadError := load.PackagesOverlapping(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		func(overlap *load.Overlap) {
			go func() {
				_, analyzeError := Analyze([]*analysis.Analyzer{whole}, overlap.Packages,
					&Options{Ready: overlap.Ready})
				analyzed <- analyzeError
			}()
		})
	if loadError != nil {
		test.Fatalf("loading: %v", loadError)
	}
	if len(loaded.Diagnostics) > 0 {
		test.Fatalf("want the fixture to type-check, got %v", loaded.Diagnostics)
	}
	select {
	case analyzeError := <-analyzed:
		if analyzeError != nil {
			test.Fatalf("analyzing: %v", analyzeError)
		}
	case <-time.After(60 * time.Second):
		test.Fatalf("want the graph analyzed, and it had not returned after 60 seconds")
	}
	slices.Sort(ran)
	want := []string{"example.com/whole/bottom", "example.com/whole/middle", "example.com/whole/top"}
	if !slices.Equal(ran, want) {
		test.Errorf("want whole run on %v, got %v", want, ran)
	}
	if len(partial) > 0 {
		test.Errorf("want every pass to meet its package type-checked whole, and %v did not", partial)
	}
}
