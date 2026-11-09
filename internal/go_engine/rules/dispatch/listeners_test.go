package dispatch

import (
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// namingCheck reports every source function, with what it read from its context: the package's name and the line
// the file set puts the function on. A synthetic function, the package initializer, has no line of its own.
func namingCheck(context *rule.Context, function *intermediate_representation.Function) {
	if function.Synthetic != "" {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos: function.Pos(),
		Message: fmt.Sprintf("%s in %s, line %d", function.Name(), context.Package.Name(),
			context.Fset.Position(function.Pos()).Line),
	})
}

// A rule's analyzer reports what its listener found, at the positions it found it: the fixture's wants name each
// function, the closure among them, its package and its line, and analysistest fails on a want nothing matched and
// on a finding no want expected.
func TestRuleAnalyzerReportsTheListenersFindings(test *testing.T) {
	test.Parallel()
	analyzer := NewIntermediateRepresentation().RuleAnalyzer(rule.Namespace+"naming", "naming",
		"reports every source function",
		&rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: namingCheck}})
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), analyzer, "dispatched")
}

// A listener reads what it requires, which the dispatcher requires for it, and its rule's own options, as the
// settings set them on its analyzer. One that reads what it does not require crashes its rule, naming what it read.
func TestAListenerReadsWhatItRequiresAndItsOwnOptions(test *testing.T) {
	test.Parallel()
	dispatcher := NewIntermediateRepresentation()
	reading := dispatcher.RuleAnalyzer(rule.Namespace+"reading", "reading",
		"reports what it reads", &rule.Listener{
			Requires:                   []*analysis.Analyzer{inspect.Analyzer},
			IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: unheardFunction},
			PackageExit: func(context *rule.Context) {
				_, isInspector := context.ResultOf(inspect.Analyzer).(*inspector.Inspector)
				context.Report(analysis.Diagnostic{
					Pos: context.Files[0].Name.Pos(),
					Message: fmt.Sprintf("inspector %t, greeting %s, int of %d bytes", isInspector,
						context.Option("greeting"), context.TypesSizes.Sizeof(types.Typ[types.Int])),
				})
			},
		})
	reading.Flags.String("greeting", "", "what the rule under test says")
	if setError := reading.Flags.Set("greeting", "hello"); setError != nil {
		test.Fatal(setError)
	}
	trespassing := dispatcher.RuleAnalyzer(rule.Namespace+"trespassing", "trespassing",
		"reads what it does not require", &rule.Listener{
			IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: unheardFunction},
			PackageExit: func(context *rule.Context) {
				context.ResultOf(inspect.Analyzer)
			},
		})
	if !slices.Contains(dispatcher.Analyzer().Requires, inspect.Analyzer) {
		test.Errorf("the dispatcher requires %v, which leaves out what a listener requires",
			dispatcher.Analyzer().Requires)
	}
	graph := analyzeFixture(test, "dispatched", reading, trespassing)
	for _, action := range graph.Roots {
		switch action.Analyzer {
		case reading:
			if action.Err != nil || len(action.Diagnostics) != 1 ||
				action.Diagnostics[0].Message != "inspector true, greeting hello, int of 8 bytes" {
				test.Errorf("the reading rule ended with %v and reported %+v, where one finding naming the inspector, "+
					"the greeting hello and the pass's 8-byte int was expected", action.Err, action.Diagnostics)
			}
		case trespassing:
			if action.Err == nil || !strings.Contains(action.Err.Error(),
				"reads the result of analyzer inspect, which its listener does not require") {
				test.Errorf("the trespassing rule ended with %v, where a crash naming inspect was expected",
					action.Err)
			}
		}
	}
}

// unheardFunction is the walk's callback for a listener that judges only at PackageExit, which a listener of the walk
// over the engine's own intermediate representation declares to hear that walk at all.
func unheardFunction(*rule.Context, *intermediate_representation.Function) {
	// Judges nothing: the listener's PackageExit does its work.
}

// IsGenerated is staticcheck's judgment of each file: the fixture's file with a generated header reads generated, and
// its hand-written files do not.
func TestIsGeneratedReadsStaticchecksJudgment(test *testing.T) {
	test.Parallel()
	judged := map[string]bool{}
	judging := NewSyntax().RuleAnalyzer(rule.Namespace+"judging", "judging", "records each file's judgment",
		&rule.Listener{
			FileEnter: func(context *rule.Context, file *ast.File) {
				filename := context.Fset.PositionFor(file.Pos(), false).Filename
				judged[filepath.Base(filename)] = context.IsGenerated(filename)
			},
		})
	for _, action := range analyzeFixture(test, "dispatched", judging).Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}
	expected := map[string]bool{"dispatched.go": false, "generated.go": true, "plain.go": false}
	if !maps.Equal(judged, expected) {
		test.Errorf("IsGenerated judged the fixture's files %v, where %v was expected", judged, expected)
	}
}

// analyzeFixture runs analyzers over the fixture package named fixture in one checker.
func analyzeFixture(test *testing.T, fixture string, analyzers ...*analysis.Analyzer) *checker.Graph {
	test.Helper()
	graph, analyzeError := checker.Analyze(analyzers, loadFixture(test, fixture), &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	return graph
}

// loadFixture loads the fixture package named fixture, a tree laid out by import path, as analysistest loads one.
func loadFixture(test *testing.T, fixture string) []*packages.Package {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{fixture}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	return loaded.Roots
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
