package consistency_no_any

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// flowSource hands values on in every way the rule records, an assignment, a definition, a call, a return, a
// composite literal and a send, and writes any in a declaration's type only when withAny is set, so the two packages
// differ in their candidate alone.
func flowSource(withAny bool) string {
	parameterType := "int"
	if withAny {
		parameterType = "any"
	}
	return "package flows\n\n" +
		"type pair struct{ left, right int }\n\n" +
		"func take(value " + parameterType + ") int { return 0 }\n\n" +
		"func run(channel chan int) int {\n" +
		"\tcount := 1\n" +
		"\tvar total int\n" +
		"\ttotal = count\n" +
		"\talias := total\n" +
		"\t_ = alias\n" +
		"\t_ = take(total)\n" +
		"\t_ = pair{left: total, right: count}\n" +
		"\tchannel <- total\n" +
		"\treturn total\n" +
		"}\n"
}

// walkedState runs the rule's listener over source as the syntax dispatcher would, every node with its ancestors, and
// returns what the rule gathered.
func walkedState(test *testing.T, source string) *packageState {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "flows.go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing the package: %v", parseError)
	}
	information := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Instances:  map[*ast.Ident]types.Instance{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Scopes:     map[ast.Node]*types.Scope{},
	}
	checked, checkError := (&types.Config{}).Check("example.com/flows", fileSet, []*ast.File{file}, information)
	if checkError != nil {
		test.Fatalf("type-checking the package: %v", checkError)
	}
	pass := &analysis.Pass{
		Fset:      fileSet,
		Files:     []*ast.File{file},
		Pkg:       checked,
		TypesInfo: information,
		ResultOf:  map[*analysis.Analyzer]any{},
	}
	context := rule.NewContext(Name, pass, nil, flag.NewFlagSet(Name, flag.ContinueOnError), nil,
		func(string, analysis.Diagnostic) {})
	enterFile(context, file)
	stack := []ast.Node{}
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		judgeNode(context, node, stack)
		stack = append(stack, node)
		return true
	})
	exitPackage(context)
	return rule.State[packageState](context)
}

// A package that writes no any and no empty interface records none of the values it hands on, since only a candidate
// is ever judged by them: its flows stay empty however many values it hands on, which is what spares nearly every
// package the cost. The same package with one any written in a parameter's type records them all.
func TestFlowsAreRecordedOnlyForAPackageWithACandidate(test *testing.T) {
	test.Parallel()
	without := walkedState(test, flowSource(false))
	if len(without.handings) == 0 {
		test.Fatal("the package hands values on, yet the walk kept no node that does")
	}
	if recorded := len(without.flows.sinkFixed) + len(without.flows.feeds) + len(without.flows.definedFrom) +
		len(without.flows.sourceFixed); recorded != 0 {
		test.Errorf("a package with no candidate recorded %d flows", recorded)
	}
	with := walkedState(test, flowSource(true))
	if len(with.candidates) != 1 {
		test.Fatalf("want the one any as the candidate, got %d", len(with.candidates))
	}
	if len(with.flows.sinkFixed) == 0 || len(with.flows.feeds) == 0 || len(with.flows.definedFrom) == 0 {
		test.Errorf("a package with a candidate recorded too little: %d sinks, %d feeds, %d definitions",
			len(with.flows.sinkFixed), len(with.flows.feeds), len(with.flows.definedFrom))
	}
}
