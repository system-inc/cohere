package staticcheck_code_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/facts/tokenfile"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// comparisonMismatchesShown is how many mismatches a failing comparison prints, the rest counted, since a broken
// helper answers thousands of nodes alike.
const comparisonMismatchesShown = 25

// question is one question asked of both sides, and what each answered, spelled to compare.
type question struct {
	// name is the helper asked, and what it was asked about, such as "IsCallTo fmt.Println".
	name     string
	position token.Position
	// label is the shape the question met, which the test requires the fixture to hold.
	label    string
	upstream string
	port     string
}

// fixtureLoad is one load of a fixture: the directory it is loaded from, its patterns and its environment.
type fixtureLoad struct {
	directory   string
	patterns    []string
	environment []string
}

// helperComparer asks both sides its questions of the nodes of one pass's package.
type helperComparer func(pass *analysis.Pass, context *rule.Context) []question

// pathFixture is the load of a fixture package under testdata/src in `GOPATH` mode, where a file's language version
// is its own `//go:build` line's and otherwise none.
func pathFixture(name string) fixtureLoad {
	directory := analysistest.TestData()
	return fixtureLoad{directory, []string{name}, []string{"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off"}}
}

// moduleFixture is the load of a fixture module under testdata/src in module mode with nothing fetched, so its go
// line gives its files a language version.
func moduleFixture(name string) fixtureLoad {
	return fixtureLoad{
		filepath.Join(analysistest.TestData(), "src", name), []string{"./..."},
		[]string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"},
	}
}

// kindOf is the name of node's type in go/ast, such as "CallExpr", which labels the shape a question met.
func kindOf(node ast.Node) string {
	return reflect.TypeOf(node).Elem().Name()
}

// ask asks one question of both sides, at position, of the shape label names.
func ask[Answer any](name string, position token.Position, label string, upstream, port func() Answer) question {
	return question{name, position, label, answerOf(upstream), answerOf(port)}
}

// answerOf is what asking did, spelled to compare: its answer, or what it panicked with, since a helper that panics
// where staticcheck's does is answering alike.
func answerOf[Answer any](asking func() Answer) (answer string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			answer = fmt.Sprintf("a panic, %v", recovered)
		}
	}()
	return fmt.Sprintf("%#v", asking())
}

// compareHelper asks compare's questions of every package of each load inside a real analysis pass, so staticcheck's
// helper reads the pass it was written for and the port the context built from it, and fails on each question the
// two answer differently and on a run that asked nothing. It returns how often each label was met, for the test to
// require.
func compareHelper(test *testing.T, name string, loads []fixtureLoad, compare helperComparer) map[string]int {
	test.Helper()
	// The analyzer requires the generated pass, which PassContext reads, and the tokenfile pass, which staticcheck's
	// code.File reads for a node's language version.
	analyzer := &analysis.Analyzer{
		Name:     name,
		Doc:      "asks staticcheck's helper and the port the same questions, and records both answers",
		Requires: []*analysis.Analyzer{generated.Analyzer, tokenfile.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			context, contextError := staticcheck_report.PassContext(pass)
			if contextError != nil {
				return nil, contextError
			}
			return compare(pass, context), nil
		},
		ResultType: reflect.TypeFor[[]question](),
	}
	asked := 0
	mismatches := []string{}
	met := map[string]int{}
	for _, each := range loads {
		loaded, loadError := load.Packages(each.directory, each.patterns, each.environment, nil)
		if loadError != nil {
			test.Fatalf("loading %v from %s: %v", each.patterns, each.directory, loadError)
		}
		for _, loadedPackage := range loaded.Roots {
			if len(loadedPackage.Errors) > 0 {
				test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
			}
		}
		graph, analyzeError := checker.Analyze([]*analysis.Analyzer{analyzer}, loaded.Roots, &checker.Options{})
		if analyzeError != nil {
			test.Fatalf("analyzing %v: %v", each.patterns, analyzeError)
		}
		for _, action := range graph.Roots {
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			questions, isQuestions := action.Result.([]question)
			if !isQuestions {
				test.Fatalf("running %s: its result is %T, where questions were expected", action, action.Result)
			}
			for _, asking := range questions {
				asked++
				met[asking.label]++
				if asking.upstream != asking.port {
					mismatches = append(mismatches, fmt.Sprintf("%s at %s: staticcheck answered %s, the port %s",
						asking.name, asking.position, asking.upstream, asking.port))
				}
			}
		}
	}
	test.Logf("%d questions asked of each side, %d answered differently", asked, len(mismatches))
	if asked == 0 {
		test.Error("no question was asked, so the comparison proves nothing")
	}
	for _, mismatch := range mismatches[:min(len(mismatches), comparisonMismatchesShown)] {
		test.Error(mismatch)
	}
	if len(mismatches) > comparisonMismatchesShown {
		test.Errorf("and %d more mismatches", len(mismatches)-comparisonMismatchesShown)
	}
	return met
}

// requireMet fails for each label the fixture never met, logging how often it met each, so a fixture that stops
// holding a shape cannot pass by asking nothing of it.
func requireMet(test *testing.T, met map[string]int, labels []string) {
	test.Helper()
	for _, label := range labels {
		test.Logf("%s: %d", label, met[label])
		if met[label] == 0 {
			test.Errorf("the fixture never met %s, so the comparison proves nothing of it", label)
		}
	}
}
