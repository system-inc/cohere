package consistency_no_any

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// flowSource hands values on in every way the rule records, an assignment, a definition, a call, a return, a
// composite literal and a send, and writes any in a declaration's type only when withAny is set, so the two packages
// differ in their candidate alone. The assignment is written assignments times, so two packages can differ in how many
// values they hand on alone.
func flowSource(withAny bool, assignments int) string {
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
		strings.Repeat("\ttotal = count\n", assignments) +
		"\talias := total\n" +
		"\t_ = alias\n" +
		"\t_ = take(total)\n" +
		"\t_ = pair{left: total, right: count}\n" +
		"\tchannel <- total\n" +
		"\treturn total\n" +
		"}\n"
}

// checkedPackage is one type-checked file, the pass the rule is run on, and each node the syntax dispatcher would call
// the listener on.
type checkedPackage struct {
	file  *ast.File
	pass  *analysis.Pass
	heard []heardNode
}

// heardNode is a node of a kind the listener lists, with its ancestors, outermost first, the node itself left out.
type heardNode struct {
	node  ast.Node
	stack []ast.Node
}

// checkedPackageOf parses and type-checks source, gives its pass the package's inspector, as the dispatcher's pass
// holds it, and keeps each node of a kind the listener lists, as the dispatcher's walk calls Node on no other.
func checkedPackageOf(test *testing.T, source string) checkedPackage {
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
	listed := map[reflect.Type]bool{}
	for _, kind := range listener.Nodes {
		listed[reflect.TypeOf(kind)] = true
	}
	heard := []heardNode{}
	stack := []ast.Node{}
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if listed[reflect.TypeOf(node)] {
			heard = append(heard, heardNode{node: node, stack: append([]ast.Node{}, stack...)})
		}
		stack = append(stack, node)
		return true
	})
	return checkedPackage{
		file: file,
		pass: &analysis.Pass{
			Fset:      fileSet,
			Files:     []*ast.File{file},
			Pkg:       checked,
			TypesInfo: information,
			ResultOf:  map[*analysis.Analyzer]any{inspect.Analyzer: inspector.New([]*ast.File{file})},
		},
		heard: heard,
	}
}

// run runs the rule's listener over the package as the syntax dispatcher would, and returns what the rule gathered
// and the findings it reported.
func (checked checkedPackage) run() (*packageState, []analysis.Diagnostic) {
	reported := []analysis.Diagnostic{}
	context := rule.NewContext(Name, checked.pass, listener.Requires, flag.NewFlagSet(Name, flag.ContinueOnError), nil,
		func(_ string, diagnostic analysis.Diagnostic) { reported = append(reported, diagnostic) })
	enterFile(context, checked.file)
	for _, each := range checked.heard {
		judgeNode(context, each.node, each.stack)
	}
	exitPackage(context)
	return rule.State[packageState](context), reported
}

// A package that writes no any and no empty interface records none of the values it hands on, since only a candidate
// is ever judged by them: its flows stay empty however many values it hands on, which is what spares nearly every
// package the cost, and it reports nothing. The same package with one any written in a parameter's type records them
// all, and reports that any, since nothing fixes the parameter's type.
func TestFlowsAreRecordedOnlyForAPackageWithACandidate(test *testing.T) {
	test.Parallel()
	without, reportedWithout := checkedPackageOf(test, flowSource(false, 1)).run()
	if recorded := len(without.flows.sinkFixed) + len(without.flows.feeds) + len(without.flows.definedFrom) +
		len(without.flows.sourceFixed); recorded != 0 {
		test.Errorf("a package with no candidate recorded %d flows", recorded)
	}
	if len(reportedWithout) != 0 {
		test.Errorf("a package with no candidate reported %d findings", len(reportedWithout))
	}
	with, reportedWith := checkedPackageOf(test, flowSource(true, 1)).run()
	if len(with.candidates) != 1 {
		test.Fatalf("want the one any as the candidate, got %d", len(with.candidates))
	}
	if len(with.flows.sinkFixed) == 0 || len(with.flows.feeds) == 0 || len(with.flows.definedFrom) == 0 {
		test.Errorf("a package with a candidate recorded too little: %d sinks, %d feeds, %d definitions",
			len(with.flows.sinkFixed), len(with.flows.feeds), len(with.flows.definedFrom))
	}
	if len(reportedWith) != 1 {
		test.Errorf("want the one any reported, got %d findings", len(reportedWith))
	}
}

// A package with no candidate keeps nothing of the values it hands on as the walk meets them, since only a package
// holding a candidate is walked for them, at its exit. So running the listener over a package that hands a value on
// once allocates as much as over one that hands values on hundreds of times; a listener that kept each node that hands
// a value on would grow its list as the second is walked.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestAPackageWithNoCandidateKeepsNothingItHandsOn(test *testing.T) {
	few := checkedPackageOf(test, flowSource(false, 1))
	many := checkedPackageOf(test, flowSource(false, 512))
	fewAllocations := testing.AllocsPerRun(20, func() { few.run() })
	manyAllocations := testing.AllocsPerRun(20, func() { many.run() })
	if manyAllocations != fewAllocations {
		test.Errorf("handing values on 512 times allocated %.0f times, once %.0f, where the two must be the same",
			manyAllocations, fewAllocations)
	}
}
