package staticcheck_code_test

import (
	"go/ast"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// Both sides name the callee of every call of the calls fixture alike. The test fails unless every way a call can
// spell its callee was met, each named or not as the shape allows: an identifier naming a function, a builtin or a
// variable, a selector naming a function, a method or a field, a parenthesized callee, an instantiation with one
// type argument or several, and a callee that is a closure, a call, an index or a type, so a fixture that stops
// holding one cannot pass by asking nothing of it.
func TestCallNameMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "comparecallname", []fixtureLoad{pathFixture("calls")}, compareCallName)
	requireMet(test, met, []string{
		"Ident, named", "Ident, a builtin", "Ident, unnamed",
		"SelectorExpr, named", "SelectorExpr, unnamed",
		"ParenExpr, named", "ParenExpr, unnamed",
		"IndexExpr, named", "IndexExpr, unnamed", "IndexListExpr, named",
		"FuncLit, unnamed", "CallExpr, unnamed", "IndexExpr of a value, unnamed", "ArrayType, unnamed",
	})
}

// compareCallName asks both sides the name of every call of the pass's package, labeling each by how its callee is
// spelled and what the answer named.
func compareCallName(pass *analysis.Pass, context *rule.Context) []question {
	questions := []question{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			upstream := code.CallName(pass, call)
			questions = append(questions, ask("CallName", pass.Fset.Position(call.Pos()),
				callNameLabel(pass, call, upstream),
				func() string { return upstream }, func() string { return staticcheck_code.CallName(context, call) }))
			// CallIndex must place staticcheck's name among names a step from it, the empty one among them, as
			// slices.Index places it.
			probes := []string{upstream + "s", "", "fmt.Println", upstream, "(*bytes.Buffer).Write", "len"}
			questions = append(questions, ask("CallIndex", pass.Fset.Position(call.Pos()),
				callNameLabel(pass, call, upstream),
				func() string { return strconv.Itoa(slices.Index(probes, upstream)) },
				func() string { return strconv.Itoa(staticcheck_code.CallIndex(context, call, probes...)) }))
			return true
		})
	}
	return questions
}

// callNameLabel is how call spells its callee and what staticcheck's answer named: a function or method, a builtin,
// or nothing.
func callNameLabel(pass *analysis.Pass, call *ast.CallExpr, name string) string {
	spelling := reflect.TypeOf(call.Fun).Elem().Name()
	// An index whose index is a type instantiates; one whose index is a value takes an element.
	if index, isIndex := call.Fun.(*ast.IndexExpr); isIndex && !pass.TypesInfo.Types[index.Index].IsType() {
		spelling = "IndexExpr of a value"
	}
	switch {
	case name == "":
		return spelling + ", unnamed"
	case types.Universe.Lookup(name) != nil:
		return spelling + ", a builtin"
	default:
		return spelling + ", named"
	}
}
