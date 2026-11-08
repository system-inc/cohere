package staticcheck_code_test

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// noPosition is a node at no position, which is in no file.
type noPosition struct{}

func (noPosition) Pos() token.Pos { return token.NoPos }

// Both sides judge alike, for every node of the tests fixture and every object its files define or use, whether it
// is in a test file. The test fails unless it met a node in a test file and in another file, in each under a line
// directive naming the other kind, an object of another package, and a node at no position, so a fixture that stops
// holding one cannot pass by asking nothing of it.
func TestIsInTestMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "compareisintest", []fixtureLoad{pathFixture("tests")}, compareIsInTest)
	requireMet(test, met, []string{
		"a test file", "another file", "a test file under a directive naming another file",
		"another file under a directive naming a test file", "another package", "nowhere",
	})
}

// compareIsInTest asks both sides of every node of the pass's files, every object they define or use, and a node at
// no position, whether it is in a test file, labeling each by where it is.
func compareIsInTest(pass *analysis.Pass, context *rule.Context) []question {
	questions := []question{}
	askOf := func(node staticcheck_report.Positioner) {
		upstream := code.IsInTest(pass, node)
		questions = append(questions, ask("IsInTest", pass.Fset.Position(node.Pos()), placeLabel(pass, node),
			func() bool { return upstream }, func() bool { return staticcheck_code.IsInTest(context, node) }))
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if node != nil {
				askOf(node)
			}
			return true
		})
	}
	for _, object := range pass.TypesInfo.Defs {
		if object != nil {
			askOf(object)
		}
	}
	for _, object := range pass.TypesInfo.Uses {
		askOf(object)
	}
	askOf(noPosition{})
	return questions
}

// placeLabel is where node is: in a test file or another, under a line directive naming the other kind, in a file of
// another package, or nowhere.
func placeLabel(pass *analysis.Pass, node staticcheck_report.Positioner) string {
	file := pass.Fset.File(node.Pos())
	if file == nil {
		return "nowhere"
	}
	isPackageFile := false
	for _, syntax := range pass.Files {
		if pass.Fset.File(syntax.Pos()) == file {
			isPackageFile = true
		}
	}
	if !isPackageFile {
		return "another package"
	}
	isTest := strings.HasSuffix(file.Name(), "_test.go")
	displayed := filepath.Base(pass.Fset.Position(node.Pos()).Filename)
	switch {
	case isTest && displayed != filepath.Base(file.Name()) && !strings.HasSuffix(displayed, "_test.go"):
		return "a test file under a directive naming another file"
	case !isTest && strings.HasSuffix(displayed, "_test.go"):
		return "another file under a directive naming a test file"
	case isTest:
		return "a test file"
	default:
		return "another file"
	}
}
