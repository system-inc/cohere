package dispatch

import (
	"fmt"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// namingCheck reports every source function, with what it read from its context: the package's name and the line
// the file set puts the function on. A synthetic function, the package initializer, has no line of its own.
func namingCheck(context *rule.Context, function *ir.Function) {
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
	analyzer := RuleAnalyzer("naming", "reports every source function", &rule.Listener{Function: namingCheck}, NewIr())
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), analyzer, "dispatched")
}

// The walk calls each listener for what it declares and nothing else, in order: for each function in the order
// staticcheck_ir.Functions reads them, every listener's Function, in the order they were registered, then each
// instruction of a listed kind in block order, then FunctionExit; and PackageExit once, after every function. The
// expected log is built from a probe that reads the same functions through buildir in the same checker.
func TestTheWalkCallsEachListenerForWhatItDeclaresInOrder(test *testing.T) {
	test.Parallel()
	heard := []string{}
	dispatcher := NewIr()
	listening := RuleAnalyzer("listening", "records each call", &rule.Listener{
		Function: func(_ *rule.Context, function *ir.Function) {
			heard = append(heard, "function "+function.Name())
		},
		Instructions: []ir.Instruction{(*ir.Call)(nil), (*ir.Return)(nil)},
		Instruction: func(_ *rule.Context, instruction ir.Instruction) {
			heard = append(heard, fmt.Sprintf("instruction %T in %s", instruction, instruction.Parent().Name()))
		},
		FunctionExit: func(_ *rule.Context, function *ir.Function) {
			heard = append(heard, "exit "+function.Name())
		},
		PackageExit: func(*rule.Context) {
			heard = append(heard, "package")
		},
	}, dispatcher)
	alongside := RuleAnalyzer("alongside", "records each function", &rule.Listener{
		Function: func(_ *rule.Context, function *ir.Function) {
			heard = append(heard, "alongside function "+function.Name())
		},
	}, dispatcher)
	probe, functions := functionProbe()
	graph := analyzeFixture(test, listening, alongside, probe)
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}

	expected := []string{}
	unlisted := 0
	for _, function := range *functions {
		expected = append(expected, "function "+function.Name(), "alongside function "+function.Name())
		for _, block := range function.Blocks {
			for _, instruction := range block.Instrs {
				switch instruction.(type) {
				case *ir.Call, *ir.Return:
					expected = append(expected, fmt.Sprintf("instruction %T in %s", instruction, function.Name()))
				default:
					unlisted++
				}
			}
		}
		expected = append(expected, "exit "+function.Name())
	}
	expected = append(expected, "package")
	if len(*functions) < 4 || unlisted == 0 {
		test.Fatalf("the fixture read as %d functions with %d instructions of kinds no listener lists, too few to "+
			"prove the walk or its filter", len(*functions), unlisted)
	}
	if !slices.Equal(heard, expected) {
		test.Errorf("the listeners heard\n%s\nwhere they declared\n%s", strings.Join(heard, "\n"),
			strings.Join(expected, "\n"))
	}
}

// A listener that panics is a crash under its own rule on the package, spelled as the engine spells an analyzer's
// panic, with none of its findings there, the one it reported before panicking included, and it is called no more
// on that package. A listener beside it still hears every function and reports all its findings.
func TestAPanickingListenerCrashesItsOwnRuleAlone(test *testing.T) {
	test.Parallel()
	dispatcher := NewIr()
	crashingCalls := 0
	crashingExited := false
	crashing := RuleAnalyzer("crashing", "reports, then panics on the second function", &rule.Listener{
		Function: func(context *rule.Context, function *ir.Function) {
			crashingCalls++
			context.Report(analysis.Diagnostic{Pos: context.Files[0].Name.Pos(), Message: "before the panic"})
			if crashingCalls == 2 {
				panic("planted panic")
			}
		},
		PackageExit: func(*rule.Context) {
			crashingExited = true
		},
	}, dispatcher)
	steady := RuleAnalyzer("steady", "reports every function", &rule.Listener{
		Function: func(context *rule.Context, function *ir.Function) {
			context.Report(analysis.Diagnostic{Pos: context.Files[0].Name.Pos(), Message: function.Name()})
		},
	}, dispatcher)
	probe, functions := functionProbe()
	var outcomes Result
	resultProbe := &analysis.Analyzer{
		Name:     "result_probe",
		Doc:      "reads the dispatcher's result",
		Requires: []*analysis.Analyzer{dispatcher.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			result, isResult := pass.ResultOf[dispatcher.Analyzer].(Result)
			if !isResult {
				return nil, fmt.Errorf("the dispatcher's result is %T", pass.ResultOf[dispatcher.Analyzer])
			}
			outcomes = result
			return nil, nil
		},
	}
	graph := analyzeFixture(test, crashing, steady, probe, resultProbe)
	if outcome := outcomes[rule.Namespace+"crashing"]; outcome.Crash == nil || len(outcome.Diagnostics) != 0 {
		test.Errorf("the dispatcher's outcome for the panicking rule holds the crash %v and %d findings, where the "+
			"crash alone was expected", outcome.Crash, len(outcome.Diagnostics))
	}
	crashed, steadied := 0, 0
	for _, action := range graph.Roots {
		switch action.Analyzer {
		case crashing:
			crashed++
			if action.Err == nil || !strings.HasPrefix(action.Err.Error(),
				"running analyzer crashing on dispatched: it panicked with planted panic, at ") ||
				!strings.Contains(action.Err.Error(), "TestAPanickingListenerCrashesItsOwnRuleAlone") {
				test.Errorf("the panicking rule ended with %v, where a crash naming the panic and the test's frame "+
					"was expected", action.Err)
			}
			if len(action.Diagnostics) != 0 {
				test.Errorf("the panicking rule reported %d findings on the package it crashed on",
					len(action.Diagnostics))
			}
		case steady:
			steadied++
			if action.Err != nil {
				test.Errorf("the rule beside the panic failed: %v", action.Err)
			}
			reported := []string{}
			for _, diagnostic := range action.Diagnostics {
				reported = append(reported, diagnostic.Message)
			}
			walked := []string{}
			for _, function := range *functions {
				walked = append(walked, function.Name())
			}
			if len(walked) < 4 || !slices.Equal(reported, walked) {
				test.Errorf("the rule beside the panic reported %q, where one finding for each of %q was expected",
					reported, walked)
			}
		}
	}
	if crashed != 1 || steadied != 1 {
		test.Fatalf("the graph ran the panicking rule %d times and the one beside it %d, where once each was "+
			"expected", crashed, steadied)
	}
	if crashingCalls != 2 || crashingExited {
		test.Errorf("the panicking listener was called %d times and exited the package %t, where it should stop at "+
			"its panic, the second call", crashingCalls, crashingExited)
	}
}

// A listener reads what it requires, which the dispatcher requires for it, and its rule's own options, as the
// settings set them on its analyzer. One that reads what it does not require crashes its rule, naming what it read.
func TestAListenerReadsWhatItRequiresAndItsOwnOptions(test *testing.T) {
	test.Parallel()
	dispatcher := NewIr()
	reading := RuleAnalyzer("reading", "reports what it reads", &rule.Listener{
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		PackageExit: func(context *rule.Context) {
			_, isInspector := context.ResultOf(inspect.Analyzer).(*inspector.Inspector)
			context.Report(analysis.Diagnostic{
				Pos: context.Files[0].Name.Pos(),
				Message: fmt.Sprintf("inspector %t, greeting %s, int of %d bytes", isInspector,
					context.Option("greeting"), context.TypesSizes.Sizeof(types.Typ[types.Int])),
			})
		},
	}, dispatcher)
	reading.Flags.String("greeting", "", "what the rule under test says")
	if setError := reading.Flags.Set("greeting", "hello"); setError != nil {
		test.Fatal(setError)
	}
	trespassing := RuleAnalyzer("trespassing", "reads what it does not require", &rule.Listener{
		PackageExit: func(context *rule.Context) {
			context.ResultOf(inspect.Analyzer)
		},
	}, dispatcher)
	if !slices.Contains(dispatcher.Analyzer.Requires, inspect.Analyzer) {
		test.Errorf("the dispatcher requires %v, which leaves out what a listener requires",
			dispatcher.Analyzer.Requires)
	}
	graph := analyzeFixture(test, reading, trespassing)
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

// IsGenerated is staticcheck's judgment of each file: the fixture's file with a generated header reads generated, and
// its hand-written files do not.
func TestIsGeneratedReadsStaticchecksJudgment(test *testing.T) {
	test.Parallel()
	judged := map[string]bool{}
	judging := RuleAnalyzer("judging", "records each file's judgment", &rule.Listener{
		PackageExit: func(context *rule.Context) {
			for _, file := range context.Files {
				filename := context.Fset.Position(file.Pos()).Filename
				judged[filepath.Base(filename)] = context.IsGenerated(filename)
			}
		},
	}, NewIr())
	for _, action := range analyzeFixture(test, judging).Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}
	expected := map[string]bool{"dispatched.go": false, "generated.go": true, "plain.go": false}
	if !maps.Equal(judged, expected) {
		test.Errorf("IsGenerated judged the fixture's files %v, where %v was expected", judged, expected)
	}
}

// functionProbe is an analyzer that reads the functions the dispatcher walks, through buildir in the same checker,
// and where it leaves them once the checker has run.
func functionProbe() (*analysis.Analyzer, *[]*ir.Function) {
	functions := &[]*ir.Function{}
	probe := &analysis.Analyzer{
		Name:     "probe",
		Doc:      "reads the functions the dispatcher walks",
		Requires: []*analysis.Analyzer{staticcheck_ir.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			read, readError := staticcheck_ir.Functions(pass)
			*functions = read
			return nil, readError
		},
	}
	return probe, functions
}

// analyzeFixture runs analyzers over the dispatched fixture in one checker.
func analyzeFixture(test *testing.T, analyzers ...*analysis.Analyzer) *checker.Graph {
	test.Helper()
	directory := analysistest.TestData()
	// The fixture is a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, []string{"dispatched"}, []string{
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

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
