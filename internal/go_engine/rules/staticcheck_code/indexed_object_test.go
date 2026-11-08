package staticcheck_code_test

import (
	"fmt"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/pattern"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// indexedPackages are the fixture packages under indexed, each reaching sort, holder or neither a different way.
var indexedPackages = []string{
	"indexed/direct", "indexed/blank", "indexed/aliased", "indexed/through", "indexed/method", "indexed/holder",
	"indexed/provider", "indexed/field", "indexed/none",
}

// indexedSymbols are the symbols each package is asked about, by path and name: sort's type and a name sort lacks,
// the fixture's own declarations, and a package no fixture reaches. Each package is also asked about its own Value.
var indexedSymbols = [][2]string{
	{"sort", "IntSlice"}, {"sort", "Missing"}, {"indexed/holder", "Box"}, {"indexed/aliased", "Ints"},
	{"indexed/provider", "Shared"}, {"indexed/aliased", "Value"}, {"fmt", "Println"},
}

// indexedComparison is what one package's side by side run of IndexedObject found.
type indexedComparison struct {
	// questions counts the symbols asked.
	questions int
	// mismatches describes each question whose two answers differed.
	mismatches []string
	// met holds each answer the run gave, by its package, the symbol and the answer.
	met map[string]int
}

// indexedAnalyzer asks both sides about every symbol of indexedSymbols in a package, its result an
// indexedComparison. It requires code.RequiredAnalyzers, whose type index CouldMatchAny reads, and staticcheck's
// generated pass, which PassContext does.
var indexedAnalyzer = &analysis.Analyzer{
	Name:       "compareindexedobject",
	Doc:        "asks staticcheck's code.CouldMatchAny of a Symbol pattern and the port's IndexedObject, and compares",
	Requires:   append([]*analysis.Analyzer{generated.Analyzer}, code.RequiredAnalyzers...),
	Run:        compareIndexed,
	ResultType: reflect.TypeFor[*indexedComparison](),
}

// For every symbol, staticcheck's code.CouldMatchAny of a parsed `(Symbol "path.name")`, which asks its type index
// for the object, holds exactly when the port's IndexedObject finds one, in every package of the indexed fixture: one
// that imports sort, one that imports it blank, one that reaches its type only through another package's alias, one
// that calls a method of it through that alias, one that reads a field of a package it never imports, and one that
// imports nothing, each asked about its own declaration too, which the index never holds. The test fails unless each
// of those ways was answered as the index answers it, so a fixture that stops holding one cannot pass unproven.
func TestIndexedObjectMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, indexedPackages, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the indexed fixture: %v", loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{indexedAnalyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing the indexed fixture: %v", analyzeError)
	}
	questions := 0
	met := map[string]int{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		compared, isComparison := action.Result.(*indexedComparison)
		if !isComparison {
			test.Fatalf("running %s: its result is %T, where a comparison was expected", action, action.Result)
		}
		questions += compared.questions
		for _, mismatch := range compared.mismatches {
			test.Error(mismatch)
		}
		for label, count := range compared.met {
			met[label] += count
		}
	}
	test.Logf("%d questions asked of each side", questions)
	for _, label := range requiredIndexedLabels() {
		test.Logf("%s: %d", label, met[label])
		if met[label] == 0 {
			test.Errorf("the fixture never met %s, so the comparison proves nothing of it", label)
		}
	}
}

// compareIndexed asks both sides about every symbol in the pass's package, recording each question they answer
// differently and every answer by package and symbol.
func compareIndexed(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	compared := &indexedComparison{met: map[string]int{}}
	symbols := append([][2]string{{pass.Pkg.Path(), "Value"}}, indexedSymbols...)
	for _, symbol := range symbols {
		upstream := code.CouldMatchAny(pass, pattern.MustParse(fmt.Sprintf("(Symbol %q)", symbol[0]+"."+symbol[1])))
		port := staticcheck_code.IndexedObject(context, symbol[0], symbol[1]) != nil
		compared.questions++
		if upstream != port {
			compared.mismatches = append(compared.mismatches, fmt.Sprintf(
				"%s asked about %s.%s: staticcheck answered %t, the port %t",
				pass.Pkg.Path(), symbol[0], symbol[1], upstream, port))
		}
		name := symbol[0] + "." + symbol[1]
		if symbol[0] == pass.Pkg.Path() {
			name = "its own Value"
		}
		compared.met[fmt.Sprintf("%s, %s, %t", pass.Pkg.Path(), name, upstream)]++
	}
	return compared, nil
}

// requiredIndexedLabels are the answers the fixture must give for the comparison to prove each way a package reaches
// another or does not.
func requiredIndexedLabels() []string {
	return []string{
		"indexed/direct, sort.IntSlice, true",
		"indexed/direct, sort.Missing, false",
		"indexed/blank, sort.IntSlice, true",
		"indexed/aliased, sort.IntSlice, true",
		"indexed/through, sort.IntSlice, false",
		"indexed/through, indexed/aliased.Ints, true",
		"indexed/method, sort.IntSlice, true",
		"indexed/field, indexed/holder.Box, true",
		"indexed/field, indexed/provider.Shared, true",
		"indexed/provider, indexed/holder.Box, true",
		"indexed/none, sort.IntSlice, false",
		"indexed/none, fmt.Println, false",
		"indexed/direct, its own Value, false",
		"indexed/none, its own Value, false",
	}
}
