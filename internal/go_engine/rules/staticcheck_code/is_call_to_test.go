package staticcheck_code_test

import (
	"fmt"
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// Both sides judge alike whether every node of the calls fixture is a call to each of a spread of names: the name
// the call's own callee has, "" which a call of nothing named has, and names it does not have, and whether it is a
// call to any of a spread of lists of them, the empty list among them. The test fails unless a call to its name, a
// call to another, and a node that is no call asked for "" were each met, so a fixture that stops holding one cannot
// pass by asking nothing of it.
func TestIsCallToMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "compareiscallto", []fixtureLoad{pathFixture("calls")}, compareIsCallTo)
	requireMet(test, met, []string{
		"IsCallTo of a call, its own name, true", "IsCallTo of a call, its own name \"\", true",
		"IsCallTo of a call, another name, false",
		"IsCallTo of no call, \"\", false", "IsCallTo of no call, another name, false",
		"IsCallToAny of a call, true", "IsCallToAny of a call, false", "IsCallToAny of no call, false",
	})
}

// compareIsCallTo asks both sides of every node of the pass's package whether it calls each name and any of each
// list of names.
func compareIsCallTo(pass *analysis.Pass, context *rule.Context) []question {
	questions := []question{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				return true
			}
			position := pass.Fset.Position(node.Pos())
			kind := "no call"
			ownName := "(none)"
			if call, isCall := node.(*ast.CallExpr); isCall {
				kind = "a call"
				ownName = code.CallName(pass, call)
			}
			for _, name := range []string{ownName, "", "fmt.Println", "len", "(*calls.counter).increment"} {
				asked := ask(fmt.Sprintf("IsCallTo %q", name), position, "",
					func() bool { return code.IsCallTo(pass, node, name) },
					func() bool { return staticcheck_code.IsCallTo(context, node, name) })
				nameKind := "another name"
				switch {
				case name == ownName && name == "":
					nameKind = "its own name \"\""
				case name == ownName:
					nameKind = "its own name"
				case name == "":
					nameKind = "\"\""
				}
				asked.label = fmt.Sprintf("IsCallTo of %s, %s, %s", kind, nameKind, asked.upstream)
				questions = append(questions, asked)
			}
			for _, names := range [][]string{nil, {""}, {"fmt.Println", "len"}, {"nothing", ownName}} {
				asked := ask(fmt.Sprintf("IsCallToAny %q", names), position, "",
					func() bool { return code.IsCallToAny(pass, node, names...) },
					func() bool { return staticcheck_code.IsCallToAny(context, node, names...) })
				asked.label = fmt.Sprintf("IsCallToAny of %s, %s", kind, asked.upstream)
				questions = append(questions, asked)
			}
			return true
		})
	}
	return questions
}
