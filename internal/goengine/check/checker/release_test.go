package checker

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"weak"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// heavyResult stands in for a large intermediate Result such as staticcheck's IR of a package.
type heavyResult struct {
	contents []byte
}

// heavyResults holds a weak pointer to each heavy Result an analyzer returned, by analyzer name, so a test can tell
// whether the graph still holds it without holding it itself.
var heavyResults = struct {
	mutex    sync.Mutex
	pointers map[string]weak.Pointer[heavyResult]
}{pointers: map[string]weak.Pointer[heavyResult]{}}

// heavyAnalyzer is an analyzer named name whose Result is a heavyResult of a mebibyte.
func heavyAnalyzer(name string) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:       name,
		Doc:        name + " returns a large result",
		ResultType: reflect.TypeFor[*heavyResult](),
		Run: func(pass *analysis.Pass) (any, error) {
			result := &heavyResult{contents: make([]byte, 1<<20)}
			heavyResults.mutex.Lock()
			heavyResults.pointers[name] = weak.Make(result)
			heavyResults.mutex.Unlock()
			return result, nil
		},
	}
}

// collected runs the collector and reports whether the heavy Result of the analyzer named name is gone.
func collected(name string) bool {
	runtime.GC()
	heavyResults.mutex.Lock()
	defer heavyResults.mutex.Unlock()
	return heavyResults.pointers[name].Value() == nil
}

// requiredHeavy is required by readerAnalyzer and is no root, so its Result is an intermediate one.
var requiredHeavy = heavyAnalyzer("requiredHeavy")

// rootHeavy is a root, so the graph keeps its Result, as upstream does.
var rootHeavy = heavyAnalyzer("rootHeavy")

// readerAnalyzer reads requiredHeavy's Result, as a rule reads inspect's or buildir's.
var readerAnalyzer = &analysis.Analyzer{
	Name:     "reader",
	Doc:      "reader reads requiredHeavy's result",
	Requires: []*analysis.Analyzer{requiredHeavy},
	Run: func(pass *analysis.Pass) (any, error) {
		if _, found := pass.ResultOf[requiredHeavy].(*heavyResult); !found {
			pass.Reportf(pass.Files[0].Package, "requiredHeavy's result was not there to read")
		}
		return nil, nil
	},
}

// observerAnalyzer runs after readerAnalyzer has finished, while the graph is still running, and its Result is
// whether requiredHeavy's Result had been collected by then.
var observerAnalyzer = &analysis.Analyzer{
	Name:       "observer",
	Doc:        "observer reports whether requiredHeavy's result is gone once its reader finished",
	Requires:   []*analysis.Analyzer{readerAnalyzer},
	ResultType: reflect.TypeFor[bool](),
	Run: func(pass *analysis.Pass) (any, error) {
		return collected("requiredHeavy"), nil
	},
}

// The Result of an analyzer no root needs any more is collected as soon as the last action that reads it has
// finished, while the graph is still running, and a root's Result is still held once the graph has finished.
func TestAResultIsReleasedWhenItsLastReaderFinishesAndARootsIsKept(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":     "module example.com/release\n\ngo 1.21\n",
		"release.go": "package release\n\nconst Name = \"release\"\n",
	} {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	loaded, loadError := packages.Load(&packages.Config{
		Mode: packages.LoadAllSyntax,
		Dir:  directory,
		Env:  append(os.Environ(), "GOWORK=off"),
	}, "./...")
	if loadError != nil || len(loaded) != 1 || len(loaded[0].Errors) > 0 {
		test.Fatalf("loading the fixture: %v, %+v", loadError, loaded)
	}

	analyzers := []*analysis.Analyzer{observerAnalyzer, rootHeavy}
	graph, analyzeError := Analyze(analyzers, loaded, &Options{Sequential: true})
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	for _, root := range graph.Roots {
		if root.Err != nil || len(root.Diagnostics) > 0 {
			test.Fatalf("%s: %v %+v", root, root.Err, root.Diagnostics)
		}
		if root.Analyzer != observerAnalyzer {
			continue
		}
		released, isBool := root.Result.(bool)
		if !isBool || !released {
			test.Errorf("want requiredHeavy's result collected once reader finished, got %v", root.Result)
		}
	}
	if collected("rootHeavy") {
		test.Errorf("want the root's result still held while the graph is, and it was collected")
	}
	runtime.KeepAlive(graph)
}
