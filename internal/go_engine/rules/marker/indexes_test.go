package marker_test

import (
	"fmt"
	"go/ast"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
)

// excuseProbe requires marker.Analyzer as the rules do, and reports what its file's index excuses at every call to
// target, so the fixture's wants read the shared result's own answer rather than a rule's findings about it. Its
// result is the indexes it was given, so the test can ask them for each file's index too.
var excuseProbe = &analysis.Analyzer{
	Name:       "excuse_probe",
	Doc:        "Reports the excuse each call to target takes from the comment index marker gives its file.",
	Requires:   []*analysis.Analyzer{marker.Analyzer},
	Run:        reportExcuses,
	ResultType: reflect.TypeFor[*marker.Indexes](),
}

func reportExcuses(pass *analysis.Pass) (any, error) {
	indexes, isIndexes := pass.ResultOf[marker.Analyzer].(*marker.Indexes)
	if !isIndexes {
		return nil, fmt.Errorf("reading the comment indexes: marker gave %T", pass.ResultOf[marker.Analyzer])
	}
	for _, file := range pass.Files {
		comments := indexes.ForFile(file)
		ast.Inspect(file, func(node ast.Node) bool {
			statement, isExpression := node.(*ast.ExprStmt)
			if !isExpression {
				return true
			}
			call, isCall := statement.X.(*ast.CallExpr)
			if !isCall {
				return true
			}
			if name, isIdentifier := call.Fun.(*ast.Ident); !isIdentifier || name.Name != "target" {
				return true
			}
			message := "nothing excuses it"
			switch comments.ExcuseFor(statement, checkedMarker) {
			case marker.WithReason:
				message = "a reason excuses it"
			case marker.WithoutReason:
				message = "an empty marker excuses nothing"
			case marker.None:
			}
			pass.Reportf(call.Pos(), "%s", message)
			return true
		})
	}
	return indexes, nil
}

// The shared result hands each file its own index. The fixture's two files each hold a call excused by a marker above
// it, one excused by a marker at the end of its line, and a bare call. The second file's excused calls sit on lines
// where the first file's index finds no marker, and its bare call directly below the line the first file's marker
// ends on, so the first file's index, asked of the second, would miss both excuses and excuse the bare call.
func TestAnalyzerHandsEachFileItsOwnIndex(test *testing.T) {
	test.Parallel()
	results := analysistest.Run(errorsOnly{test}, analysistest.TestData(), excuseProbe, "two_files")
	if len(results) != 1 {
		test.Fatalf("analyzing the fixture gave %d results, expected 1", len(results))
	}
	if results[0].Err != nil {
		test.Fatal(results[0].Err)
	}
	indexes, isIndexes := results[0].Result.(*marker.Indexes)
	if !isIndexes {
		test.Fatalf("the probe answered %T, expected the indexes it was given", results[0].Result)
	}
	files := results[0].Action.Package.Syntax
	if len(files) != 2 {
		test.Fatalf("the fixture's package holds %d files, expected 2", len(files))
	}
	if indexes.ForFile(files[0]) == indexes.ForFile(files[1]) {
		test.Error("both of the fixture's files were handed one index")
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
