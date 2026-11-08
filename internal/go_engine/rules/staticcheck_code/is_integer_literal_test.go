package staticcheck_code_test

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// Both sides judge alike whether every node of the integers and constants fixtures is an integer literal equal to
// each of a spread of values: integers matching and not, one too large for an int64, a whole float, and a string and
// a boolean, which constant.Compare compares with themselves, so that both sides find every literal equal to them.
// The test fails unless literals equal and unequal, both odd comparisons, and near misses, a constant by name, a
// constant expression and statements, were each met, so a fixture that stops holding one cannot pass by asking
// nothing of it.
func TestIsIntegerLiteralMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "compareisintegerliteral",
		[]fixtureLoad{pathFixture("integers"), pathFixture("constants")}, compareIsIntegerLiteral)
	requireMet(test, met, []string{
		"IsIntegerLiteral of BasicLit, true", "IsIntegerLiteral of BasicLit, false",
		"IsIntegerLiteral of UnaryExpr, true", "IsIntegerLiteral of ParenExpr, true",
		"IsIntegerLiteral of Ident, false", "IsIntegerLiteral of BinaryExpr, false",
		"IsIntegerLiteral of AssignStmt, false", "IsIntegerLiteral of BlockStmt, false",
		"IsIntegerLiteral of BasicLit against a string, true", "IsIntegerLiteral of BasicLit against a boolean, true",
	})
}

// compareIsIntegerLiteral asks both sides of every node of the pass's package whether it is an integer literal of
// each of a spread of values.
func compareIsIntegerLiteral(pass *analysis.Pass, context *rule.Context) []question {
	values := []constant.Value{
		constant.MakeInt64(0), constant.MakeInt64(1), constant.MakeInt64(-1), constant.MakeInt64(5),
		constant.MakeInt64(42), constant.MakeInt64(16), constant.Shift(constant.MakeInt64(1), token.SHL, 70),
		constant.MakeFloat64(1), constant.MakeString("1"), constant.MakeBool(false),
	}
	questions := []question{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				return true
			}
			position := pass.Fset.Position(node.Pos())
			for _, value := range values {
				asked := ask(fmt.Sprintf("IsIntegerLiteral %s", value), position, "",
					func() bool { return code.IsIntegerLiteral(pass, node, value) },
					func() bool { return staticcheck_code.IsIntegerLiteral(context, node, value) })
				against := ""
				switch value.Kind() {
				case constant.String:
					against = " against a string"
				case constant.Bool:
					against = " against a boolean"
				}
				asked.label = fmt.Sprintf("IsIntegerLiteral of %s%s, %s", kindOf(node), against, asked.upstream)
				questions = append(questions, asked)
			}
			return true
		})
	}
	return questions
}
