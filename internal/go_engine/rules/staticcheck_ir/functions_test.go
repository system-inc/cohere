package staticcheck_ir

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
	"honnef.co/go/tools/unused"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// A result of buildir's shape reads, and one whose field was renamed, retyped or hidden, or that is no pointer to a
// struct at all, is an error naming what it found, never an empty slice that reads as a package with no functions.
// The first case is the control: a fake with buildir's field reads.
func TestFunctionsRefusesAResultOfAnotherShape(test *testing.T) {
	test.Parallel()
	functionsType := reflect.TypeFor[[]*ir.Function]()
	function := &ir.Function{}
	control := fakeResult("SrcFuncs", "", functionsType)
	reflect.ValueOf(control).Elem().Field(0).Set(reflect.ValueOf([]*ir.Function{function}))
	functions, readError := functionsOf(control)
	if readError != nil || len(functions) != 1 || functions[0] != function {
		test.Fatalf("a result of buildir's shape read as %v, %v", functions, readError)
	}
	cases := []struct {
		name   string
		result any
		says   string
	}{
		{"renamed", fakeResult("SourceFunctions", "", functionsType), "has no SrcFuncs field"},
		{"retyped", fakeResult("SrcFuncs", "", reflect.TypeFor[[]ir.Function]()), "where []*ir.Function was expected"},
		{"hidden", fakeResult("srcFuncs", "example.com/fake", functionsType), "has no SrcFuncs field"},
		{"no pointer", reflect.ValueOf(control).Elem().Interface(), "where a pointer to a struct was expected"},
		{"absent", nil, "where a pointer to a struct was expected"},
		{"nil pointer", (*ir.Function)(nil), "where a pointer to a struct was expected"},
	}
	for _, testCase := range cases {
		functions, readError := functionsOf(testCase.result)
		if readError == nil {
			test.Errorf("a result %s read as %d functions, where an error was expected", testCase.name, len(functions))
			continue
		}
		if !strings.Contains(readError.Error(), testCase.says) {
			test.Errorf("a result %s failed with %q, which does not say %q", testCase.name, readError, testCase.says)
		}
	}
}

// fakeResult is a pointer to a new struct of one field, named name, unexported when packagePath is set, of type
// fieldType. The struct is built by reflection, as buildir's result is read, so its field's spelling is a string here
// rather than an identifier of the house's.
func fakeResult(name string, packagePath string, fieldType reflect.Type) any {
	structure := reflect.StructOf([]reflect.StructField{{Name: name, PkgPath: packagePath, Type: fieldType}})
	return reflect.New(structure).Interface()
}

// Analyzer is staticcheck's real buildir pass, not the stand-in that fails, and every analyzer named buildir that
// any check cohere registers from staticcheck requires, however deep, is that one pointer, so the checker builds the
// representation once per package for all of them and the port.
func TestAnalyzerIsTheBuildirStaticchecksChecksRequire(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != buildirName || Analyzer.ResultType == nil {
		test.Fatalf("Analyzer is %q with result type %v, the stand-in for a buildir staticcheck's checks no longer "+
			"require once", Analyzer.Name, Analyzer.ResultType)
	}
	roots := []*analysis.Analyzer{unused.Analyzer.Analyzer}
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers, stylecheck.Analyzers) {
		roots = append(roots, wrapped.Analyzer)
	}
	requiring := 0
	visited := map[*analysis.Analyzer]bool{}
	var visit func(analyzer *analysis.Analyzer)
	visit = func(analyzer *analysis.Analyzer) {
		if visited[analyzer] {
			return
		}
		visited[analyzer] = true
		for _, required := range analyzer.Requires {
			if required.Name == buildirName {
				requiring++
				if required != Analyzer {
					test.Errorf("%s requires an analyzer named %s that is not Analyzer, so it would build its own",
						analyzer.Name, buildirName)
				}
			}
			visit(required)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	if requiring == 0 {
		test.Fatal("no check cohere registers from staticcheck requires buildir, so there is nothing to share")
	}
	test.Logf("%d analyzers require buildir", requiring)
}

// The port's reader and a staticcheck check run in one checker over a fixture, beside a probe that reads the result
// through the check's own requirement: buildir runs once per package, and both probes read the very same functions,
// pointer for pointer.
func TestFunctionsReadsTheIntermediateRepresentationStaticcheckReads(test *testing.T) {
	test.Parallel()
	check, requirement := checkRequiringBuildir(test)
	resultType := reflect.TypeFor[[]*ir.Function]()
	throughReader := &analysis.Analyzer{
		Name:       "through_reader",
		Doc:        "returns the functions Functions reads",
		Requires:   []*analysis.Analyzer{Analyzer},
		ResultType: resultType,
		Run: func(pass *analysis.Pass) (any, error) {
			return Functions(pass)
		},
	}
	throughCheck := &analysis.Analyzer{
		Name:       "through_check",
		Doc:        "returns the functions read through a staticcheck check's own requirement",
		Requires:   []*analysis.Analyzer{requirement},
		ResultType: resultType,
		Run: func(pass *analysis.Pass) (any, error) {
			return functionsOf(pass.ResultOf[requirement])
		},
	}
	graph := analyzeFixture(test, throughReader, check, throughCheck)
	builds := map[*packages.Package]int{}
	for action := range graph.All() {
		if action.Analyzer.Name == buildirName {
			builds[action.Package]++
		}
	}
	results := map[*analysis.Analyzer]map[*packages.Package][]*ir.Function{throughReader: {}, throughCheck: {}}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		if functions, isFunctions := action.Result.([]*ir.Function); isFunctions {
			results[action.Analyzer][action.Package] = functions
		}
	}
	if len(results[throughReader]) == 0 {
		test.Fatal("the reader's probe returned nothing, so nothing was compared")
	}
	for fixturePackage, fromReader := range results[throughReader] {
		if builds[fixturePackage] != 1 {
			test.Errorf("buildir ran %d times over %s, where once is one build shared", builds[fixturePackage],
				fixturePackage.PkgPath)
		}
		fromCheck := results[throughCheck][fixturePackage]
		if len(fromReader) == 0 || !slices.Equal(fromReader, fromCheck) {
			test.Errorf("over %s the reader read %d functions and %s's requirement %d, not the same pointers",
				fixturePackage.PkgPath, len(fromReader), check.Name, len(fromCheck))
		}
	}
}

// checkRequiringBuildir is the first check staticcheck registers that requires buildir itself, and that requirement.
func checkRequiringBuildir(test *testing.T) (*analysis.Analyzer, *analysis.Analyzer) {
	test.Helper()
	for _, wrapped := range staticcheck.Analyzers {
		for _, required := range wrapped.Analyzer.Requires {
			if required.Name == buildirName {
				return wrapped.Analyzer, required
			}
		}
	}
	test.Fatal("no check staticcheck registers requires buildir itself")
	return nil, nil
}

// analyzeFixture runs analyzers over the functions fixture in one checker.
func analyzeFixture(test *testing.T, analyzers ...*analysis.Analyzer) *checker.Graph {
	test.Helper()
	directory := analysistest.TestData()
	// The fixture is a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, []string{"functions"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	graph, analyzeError := checker.Analyze(analyzers, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	return graph
}
