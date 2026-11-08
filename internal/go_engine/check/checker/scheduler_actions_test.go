package checker

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/packages"
)

// analyzeWithin is Analyze, failing the test if it has not returned within ten seconds, so a scheduler that
// deadlocks fails the test rather than stalling the run.
func analyzeWithin(test *testing.T, analyzers []*analysis.Analyzer, roots []*packages.Package,
	options *Options) *Graph {
	test.Helper()
	analyzed := make(chan *Graph, 1)
	go func() {
		graph, analyzeError := Analyze(analyzers, roots, options)
		if analyzeError != nil {
			test.Errorf("analyzing: %v", analyzeError)
		}
		analyzed <- graph
	}()
	select {
	case graph := <-analyzed:
		if graph == nil {
			test.Fatalf("want a graph, got none")
		}
		return graph
	case <-time.After(10 * time.Second):
		test.Fatalf("want the graph analyzed, and it had not returned after 10 seconds")
		return nil
	}
}

// executionCounter counts the passes running at once, and the most that ever did.
type executionCounter struct {
	lock       sync.Mutex
	running    int
	mostAtOnce int
}

// counted is an analyzer named name whose pass takes a moment, counted by counter while it runs.
func counted(name string, counter *executionCounter) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: name,
		Doc:  name + " takes a moment on every package, counting the passes that run beside it",
		Run: func(pass *analysis.Pass) (any, error) {
			counter.enter()
			time.Sleep(10 * time.Millisecond)
			counter.leave()
			return nil, nil
		},
	}
}

// enter counts a pass that has started.
func (counter *executionCounter) enter() {
	counter.lock.Lock()
	defer counter.lock.Unlock()
	counter.running++
	counter.mostAtOnce = max(counter.mostAtOnce, counter.running)
}

// leave counts a pass that has finished.
func (counter *executionCounter) leave() {
	counter.lock.Lock()
	defer counter.lock.Unlock()
	counter.running--
}

// With two workers, no more than two actions ever execute at once, though every package is admitted together and
// each holds four actions ready from the start.
func TestNoMoreActionsExecuteAtOnceThanThereAreWorkers(test *testing.T) {
	test.Parallel()

	files := map[string]string{}
	for _, name := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot"} {
		files[name+"/"+name+".go"] = packageOf(name, 10)
	}
	roots := writeModule(test, "pool", files, "./...")
	counter := &executionCounter{}
	analyzers := []*analysis.Analyzer{counted("first", counter), counted("second", counter),
		counted("third", counter), counted("fourth", counter)}
	graph := analyzeWithin(test, analyzers, roots, &Options{slots: 6, workers: 2})
	if len(graph.Roots) != 24 {
		test.Fatalf("want 24 actions, 4 analyzers on 6 packages, got %d", len(graph.Roots))
	}
	if counter.mostAtOnce > 2 {
		test.Errorf("want at most 2 actions executing at once with 2 workers, got %d", counter.mostAtOnce)
	}
}

// With one worker, of two ready packages the one with the longer weighted path to the end of the graph runs first,
// though the other is the larger. Lower is small but middle and top wait on it in a chain, so its path is about
// 10 + 600 + 600 bytes against solo's 1000; once middle runs, top's path of 600 is shorter than solo's, so solo
// runs before top. Largest first would start solo, and queue order would too, since solo is granted first.
func TestTheReadyActionOnTheLongestPathRunsFirst(test *testing.T) {
	test.Parallel()

	roots := writeModule(test, "path", map[string]string{
		"lower/lower.go": packageOf("lower", 10),
		"middle/middle.go": "package middle\n\nimport \"example.com/path/lower\"\n\n// " + strings.Repeat("x", 600) +
			"\n\nfunc Middle() { lower.Lower() }\n",
		"top/top.go": "package top\n\nimport \"example.com/path/middle\"\n\n// " + strings.Repeat("x", 600) +
			"\n\nfunc Top() { middle.Middle() }\n",
		"solo/solo.go": packageOf("solo", 1000),
	}, "./...")
	var lock sync.Mutex
	var order []string
	ordered := &analysis.Analyzer{
		Name:      "ordered",
		Doc:       "ordered records the order its passes run in, and inherits facts so a package waits on its imports",
		FactTypes: []analysis.Fact{new(declaredFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			lock.Lock()
			order = append(order, pass.Pkg.Name())
			lock.Unlock()
			return nil, nil
		},
	}
	analyzeWithin(test, []*analysis.Analyzer{ordered}, roots, &Options{slots: 4, workers: 1})
	if want := []string{"lower", "middle", "solo", "top"}; !slices.Equal(order, want) {
		test.Errorf("want the passes run longest path first, %v, got %v", want, order)
	}
}

// totalAnalyzer reads inspect's Result, a horizontal edge, and its imports' package facts, a vertical one, and
// reports and exports the total it makes of them.
var totalAnalyzer = &analysis.Analyzer{
	Name:      "total",
	Doc:       "total counts the functions a package and every package below it declare",
	Requires:  []*analysis.Analyzer{inspect.Analyzer},
	FactTypes: []analysis.Fact{new(totalFact)},
	Run: func(pass *analysis.Pass) (any, error) {
		total := 0
		inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
		if !isInspector {
			return nil, fmt.Errorf("want inspect's result, got %T", pass.ResultOf[inspect.Analyzer])
		}
		inspected.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(ast.Node) { total++ })
		for _, imported := range pass.Pkg.Imports() {
			var below totalFact
			if pass.ImportPackageFact(imported, &below) {
				total += below.Functions
			}
		}
		pass.ExportPackageFact(&totalFact{Functions: total})
		pass.Reportf(pass.Files[0].Package, "%d functions here and below", total)
		return nil, nil
	},
}

// summarize is every action of graph, by analyzer and package, with its error, its findings and every fact it holds,
// inherited and exported, each as text, so two runs over the same packages compare line by line.
func summarize(graph *Graph) []string {
	var lines []string
	for action := range graph.All() {
		prefix := action.Analyzer.Name + "@" + action.Package.ID
		if action.Err != nil {
			lines = append(lines, prefix+" error "+action.Err.Error())
		}
		for _, diagnostic := range action.Diagnostics {
			lines = append(lines, prefix+" finding "+action.Package.Fset.Position(diagnostic.Pos).String()+" "+
				diagnostic.Message)
		}
		for _, fact := range action.AllObjectFacts() {
			lines = append(lines, fmt.Sprintf("%s object fact %s %v", prefix,
				types.ObjectString(fact.Object, nil), fact.Fact))
		}
		for _, fact := range action.AllPackageFacts() {
			lines = append(lines, fmt.Sprintf("%s package fact %s %v", prefix, fact.Package.Path(), fact.Fact))
		}
		objectFacts, packageFacts := action.ExportedFacts()
		lines = append(lines, fmt.Sprintf("%s exported %d object facts and %d package facts", prefix,
			len(objectFacts), len(packageFacts)))
	}
	slices.Sort(lines)
	return lines
}

// A sequential run and a parallel one over the same packages give every action the same findings and the same
// facts, inherited and exported, on a graph of ten packages that import each other in a lattice, under two fact
// analyzers, one of which also reads another analyzer's Result.
func TestASequentialAndAParallelRunGiveEveryActionTheSameResults(test *testing.T) {
	test.Parallel()

	files := map[string]string{}
	for index := range 10 {
		name := fmt.Sprintf("p%02d", index)
		var imports []string
		var calls []string
		for _, below := range []int{index - 1, index - 3} {
			if below < 0 {
				continue
			}
			imported := fmt.Sprintf("p%02d", below)
			imports = append(imports, "\"example.com/lattice/"+imported+"\"")
			calls = append(calls, fmt.Sprintf("%s.F%s()", imported, imported))
		}
		source := "package " + name + "\n\n"
		if len(imports) > 0 {
			source += "import (\n\t" + strings.Join(imports, "\n\t") + "\n)\n\n"
		}
		source += "func F" + name + "() {\n\t" + strings.Join(calls, "\n\t") + "\n}\n\nfunc helper" + name + "() {}\n"
		files[name+"/"+name+".go"] = source
	}
	roots := writeModule(test, "lattice", files, "./...")
	analyzers := []*analysis.Analyzer{declaredAnalyzer, totalAnalyzer}
	sequential := summarize(analyzeWithin(test, analyzers, roots, &Options{Sequential: true}))
	parallel := summarize(analyzeWithin(test, analyzers, roots, &Options{workers: 4}))

	// A run that inherited nothing would compare equal to another like it, so the sequential run is held to what
	// the lattice makes: p09 calls p08's and p06's functions, which it knows by their facts, and counts 118, each
	// package's two functions once along every import path down to it.
	for _, want := range []string{"calls Fp08, which a dependency declared", "calls Fp06, which a dependency declared",
		"118 functions here and below"} {
		if !slices.ContainsFunc(sequential, func(line string) bool {
			return strings.Contains(line, "/p09/p09.go") && strings.HasSuffix(line, want)
		}) {
			test.Errorf("want p09's finding %q in the sequential run, got %v", want, sequential)
		}
	}
	if !slices.Equal(sequential, parallel) {
		for _, line := range sequential {
			if !slices.Contains(parallel, line) {
				test.Errorf("only the sequential run has: %s", line)
			}
		}
		for _, line := range parallel {
			if !slices.Contains(sequential, line) {
				test.Errorf("only the parallel run has: %s", line)
			}
		}
		test.Errorf("want the same %d lines from both runs, got %d sequential and %d parallel", len(sequential),
			len(sequential), len(parallel))
	}
}
