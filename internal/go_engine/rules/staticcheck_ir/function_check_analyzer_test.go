package staticcheck_ir

import (
	"fmt"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// namingCheck reports every source function, with what it read from its context: the package's name and the line
// the file set puts the function on. A synthetic function, the package initializer, has no line of its own.
func namingCheck(context *rule.Context, function *ir.Function) {
	if function.Synthetic != "" || context.TypesInfo == nil {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos: function.Pos(),
		Message: fmt.Sprintf("%s in %s, line %d", function.Name(), context.Package.Name(),
			context.Fset.Position(function.Pos()).Line),
	})
}

// The wrapper calls the check on every function buildir builds, the closure among them, with a context whose package,
// type information and file set are the pass's, and each finding reaches the pass: the fixture's wants name each
// function, its package and its line, and analysistest fails on a want nothing matched and on a finding no want
// expected.
func TestFunctionCheckAnalyzerCallsTheCheckOnEveryFunction(test *testing.T) {
	test.Parallel()
	analyzer := FunctionCheckAnalyzer("naming", "reports every source function", namingCheck)
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), analyzer, "functions")
}

// Report hands the sink the name the check was registered under, so a check never names its rule.
func TestContextReportStampsTheRegisteredRuleName(test *testing.T) {
	test.Parallel()
	stamped := []string{}
	context := rule.NewContext(
		rule.Namespace+"planted", types.NewPackage("example.com/planted", "planted"), &types.Info{},
		token.NewFileSet(), func(ruleName string, _ analysis.Diagnostic) {
			stamped = append(stamped, ruleName)
		},
	)
	context.Report(analysis.Diagnostic{Message: "planted"})
	if len(stamped) != 1 || stamped[0] != rule.Namespace+"planted" {
		test.Errorf("Report stamped %q, where %q was registered", stamped, rule.Namespace+"planted")
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
