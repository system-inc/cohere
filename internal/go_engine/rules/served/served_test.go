package served_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/served"
)

// servedTypesAnalyzer builds an Index over its package and answers what each file serves, so the test reads the
// Index's own answer rather than a rule's findings about it.
var servedTypesAnalyzer = &analysis.Analyzer{
	Name:       "served_types",
	Doc:        "Records the types each file of a package serves, as served.Index judges them.",
	Run:        recordServedTypes,
	ResultType: reflect.TypeFor[map[string][]string](),
}

// recordServedTypes maps each file's name to the types it serves in source order, each written as its name and how
// the file serves it, so a file serving nothing maps to an empty list rather than going missing.
func recordServedTypes(pass *analysis.Pass) (any, error) {
	index := served.NewIndex(pass)
	servedByFile := map[string][]string{}
	for _, file := range pass.Files {
		fileName := filepath.Base(pass.Fset.PositionFor(file.Package, false).Filename)
		descriptions := []string{}
		for _, servedType := range index.Types(file) {
			description := servedType.TypeName.Name() + " by its methods"
			if servedType.Declared {
				description = servedType.TypeName.Name() + " declared"
			}
			descriptions = append(descriptions, description)
		}
		servedByFile[fileName] = descriptions
	}
	return servedByFile, nil
}

// Index counts a type by a real method anywhere in the package, at its declaration and at the receiver of a method a
// file holds of a type declared elsewhere, and counts no type without methods, no sum type's variant whose only method
// marks it, and every no-op whose empty method takes a parameter, which is behavior rather than membership.
func TestIndexTypesCountsTypesWithRealMethods(test *testing.T) {
	test.Parallel()
	// The fixture's package is not named index, which go list would resolve to the standard library's directory.
	results := analysistest.Run(errorsOnly{test}, analysistest.TestData(), servedTypesAnalyzer, "fixture")
	if len(results) != 1 {
		test.Fatalf("analyzing the fixture gave %d results, expected 1", len(results))
	}
	if results[0].Err != nil {
		test.Fatal(results[0].Err)
	}
	servedByFile, isServedByFile := results[0].Result.(map[string][]string)
	if !isServedByFile {
		test.Fatalf("the analyzer answered %T, expected a map of file names to served types", results[0].Result)
	}
	cases := []struct {
		name     string
		fileName string
		expected []string
	}{
		{"a type with a real method declared beside it", "grove.go", []string{"Grove declared"}},
		{"a type whose real method sits in another file", "leaf.go", []string{"Leaf declared"}},
		{"a real method of a type declared elsewhere", "leaf_fall.go", []string{"Leaf by its methods"}},
		{"a type with no methods", "point.go", []string{}},
		{"a sum type's variant whose only method is a marker", "instruction.go", []string{}},
		{"a no-op whose empty method takes a parameter", "noop.go", []string{"noopTracer declared"}},
	}
	// Every fixture file is a case, so a file added without one fails here rather than going unjudged.
	if len(servedByFile) != len(cases) {
		test.Errorf("the fixture has %d files, expected %d", len(servedByFile), len(cases))
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			found, isFound := servedByFile[testCase.fileName]
			if !isFound {
				subtest.Fatalf("finding %s among the analyzed files", testCase.fileName)
			}
			if !slices.Equal(found, testCase.expected) {
				subtest.Errorf("%s serves %q, expected %q", testCase.fileName, found, testCase.expected)
			}
		})
	}
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
