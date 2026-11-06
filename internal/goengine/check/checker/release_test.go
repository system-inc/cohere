package checker

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"weak"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/load"
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
	loaded, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Roots) != 1 || len(loaded.Roots[0].Errors) > 0 {
		test.Fatalf("loading the fixture: %v, %+v", loadError, loaded)
	}

	analyzers := []*analysis.Analyzer{observerAnalyzer, rootHeavy}
	graph, analyzeError := Analyze(analyzers, loaded.Roots, &Options{Sequential: true})
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

// declaredAnalyzer exports a declaredFact for every function its package declares and reports each call to a
// function that has one, so, like printf and ctrlflow, it runs on every dependency of the packages it is given.
var declaredAnalyzer = &analysis.Analyzer{
	Name:      "declared",
	Doc:       "declared reports each call to a function a dependency declared",
	FactTypes: []analysis.Fact{new(declaredFact)},
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.FuncDecl:
					if function, isFunction := pass.TypesInfo.Defs[typed.Name].(*types.Func); isFunction {
						pass.ExportObjectFact(function, new(declaredFact))
					}
				case *ast.CallExpr:
					selector, isSelector := typed.Fun.(*ast.SelectorExpr)
					if !isSelector {
						return true
					}
					function, isFunction := pass.TypesInfo.Uses[selector.Sel].(*types.Func)
					if isFunction && pass.ImportObjectFact(function, new(declaredFact)) {
						pass.Reportf(typed.Pos(), "calls %s, which a dependency declared", function.Name())
					}
				}
				return true
			})
		}
		return nil, nil
	},
}

// A dependency the graph was not given is analyzed only for its facts, so once its last action has finished its
// syntax trees and type information are let go, while the graph that holds every action is still alive, and the
// facts it exported still reach the package that imports it. A package the graph was given keeps both.
func TestADependencysSyntaxIsReleasedOnceItsLastActionFinishesAndAGivenPackagesIsKept(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":         "module example.com/syntax\n\ngo 1.21\n",
		"lower/lower.go": "package lower\n\nfunc Lower() {}\n",
		"upper/upper.go": "package upper\n\nimport \"example.com/syntax/lower\"\n\nfunc Upper() { lower.Lower() }\n",
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
	lower, imported := loaded.Roots[0].Imports["example.com/syntax/lower"]
	if !imported || len(lower.Syntax) != 1 || lower.TypesInfo == nil {
		test.Fatalf("want lower loaded from source as upper's dependency, got %+v", loaded.Roots[0].Imports)
	}
	lowerFile := weak.Make(lower.Syntax[0])
	lowerTypesInfo := weak.Make(lower.TypesInfo)

	graph, analyzeError := Analyze([]*analysis.Analyzer{declaredAnalyzer}, loaded.Roots, nil)
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	if len(graph.Roots) != 1 || graph.Roots[0].Err != nil || len(graph.Roots[0].Diagnostics) != 1 ||
		graph.Roots[0].Diagnostics[0].Message != "calls Lower, which a dependency declared" {
		test.Fatalf("want one finding on upper, from lower's fact, got %+v", graph.Roots)
	}
	if lower.Syntax != nil || lower.TypesInfo != nil {
		test.Errorf("want lower's syntax and type information let go, got %d files and %v", len(lower.Syntax),
			lower.TypesInfo)
	}
	runtime.GC()
	if lowerFile.Value() != nil || lowerTypesInfo.Value() != nil {
		test.Errorf("want lower's syntax tree and type information collected while the graph is alive, and the " +
			"graph still holds them")
	}
	if len(loaded.Roots[0].Syntax) != 1 || loaded.Roots[0].TypesInfo == nil {
		test.Errorf("want upper, a package the graph was given, to keep its syntax and type information")
	}
	runtime.KeepAlive(graph)
}
