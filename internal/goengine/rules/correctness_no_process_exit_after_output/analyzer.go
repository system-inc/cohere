// Package correctness_no_process_exit_after_output is the Go engine's rule that a function does not end the process
// while it holds deferred calls.
//
// os.Exit ends the process on the spot, and log.Fatal calls it, so neither runs a deferred call. A function that has
// already said `defer writer.Flush()` or `defer file.Close()` and then exits loses the buffered output or leaves the
// file half written, with no error and often a zero exit status. It is the Go form of the TypeScript twin's hazard,
// a `process.exit()` that cuts off output still in flight.
package correctness_no_process_exit_after_output

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-no-process-exit-after-output"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_no_process_exit_after_output",
	Doc: "Flags a call to os.Exit or log.Fatal, Fatalf or Fatalln that a defer statement of the same function " +
		"reaches through its control-flow graph, since the exit ends the process without running the deferred call.",
	// ctrlflow builds each function's graph with calls that never return, os.Exit, log.Fatal, panic and a helper
	// that always exits, ending their path, so a defer on a path that has already left is never counted.
	Requires: []*analysis.Analyzer{ctrlflow.Analyzer, inspect.Analyzer},
	Run:      run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var exitAfterOutputMessage = policy.MessageOf(Name, "exitAfterOutput")

func run(pass *analysis.Pass) (any, error) {
	graphs, isGraphs := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isGraphs || !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("correctness_no_process_exit_after_output: a required analyzer's result is missing")
	}
	// Each function is judged on its own graph. A function literal has a graph of its own, and the graph of the
	// function around it does not descend into it, so a defer inside a literal never counts for the function
	// around it, nor the other way.
	inspected.Preorder([]ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)}, func(node ast.Node) {
		var graph *cfg.CFG
		switch function := node.(type) {
		case *ast.FuncDecl:
			// A declaration with no body has no graph, and one whose name did not type-check has none either.
			if function.Body == nil {
				return
			}
			if _, isFunc := pass.TypesInfo.Defs[function.Name].(*types.Func); !isFunc {
				return
			}
			graph = graphs.FuncDecl(function)
		case *ast.FuncLit:
			graph = graphs.FuncLit(function)
		}
		if graph != nil {
			judgeGraph(pass, graph)
		}
	})
	return nil, nil
}

// judgeGraph reports every exit in graph that a live defer statement reaches: one earlier in the exit's own block,
// or one anywhere in a block from which the exit's block can be reached, its own block included when a loop leads
// back to it. A defer in a loop's body is pending at an exit the next turn meets, so that exit is reported too.
func judgeGraph(pass *analysis.Pass, graph *cfg.CFG) {
	predecessors := map[*cfg.Block][]*cfg.Block{}
	for _, block := range graph.Blocks {
		for _, successor := range block.Succs {
			predecessors[successor] = append(predecessors[successor], block)
		}
	}
	for _, block := range graph.Blocks {
		if !block.Live {
			continue
		}
		for index, node := range block.Nodes {
			call := exitCall(node)
			if call == nil {
				continue
			}
			callee := exitCallee(pass, call)
			if callee == "" {
				continue
			}
			if !holdsDefer(block.Nodes[:index]) && !reachedByDefer(block, predecessors) {
				continue
			}
			pass.Report(analysis.Diagnostic{
				Pos:      call.Pos(),
				End:      call.End(),
				Category: exitAfterOutputMessage.Id,
				Message:  exitAfterOutputMessage.Render(map[string]string{"call": callee}),
			})
		}
	}
}

// exitCall is the call a graph node makes as a statement of its own, or as a deferred call: os.Exit and log.Fatal
// return nothing, so outside a function literal they can stand nowhere else. A `defer os.Exit(code)` runs before
// every call deferred ahead of it, and so skips them. A `go` statement's call is left alone: it runs on another
// goroutine at a moment this function's graph cannot place.
func exitCall(node ast.Node) *ast.CallExpr {
	switch statement := node.(type) {
	case *ast.ExprStmt:
		call, isCall := ast.Unparen(statement.X).(*ast.CallExpr)
		if isCall {
			return call
		}
	case *ast.DeferStmt:
		return statement.Call
	}
	return nil
}

// exitCallee names the exit call calls, as the message prints it, or is empty when call is no exit. The exits are
// os.Exit and the standard log's Fatal, Fatalf and Fatalln, as package functions and as methods of *log.Logger,
// resolved by the callee's package and receiver so a Fatal of any other type, testing's among them, is no exit.
func exitCallee(pass *analysis.Pass, call *ast.CallExpr) string {
	callee := typeutil.StaticCallee(pass.TypesInfo, call)
	if callee == nil || callee.Pkg() == nil {
		return ""
	}
	receiver := callee.Signature().Recv()
	switch callee.Pkg().Path() {
	case "os":
		if receiver == nil && callee.Name() == "Exit" {
			return "os.Exit"
		}
	case "log":
		switch callee.Name() {
		case "Fatal", "Fatalf", "Fatalln":
		default:
			return ""
		}
		if receiver == nil {
			return "log." + callee.Name()
		}
		if isPointerToLogLogger(receiver.Type()) {
			return "Logger." + callee.Name()
		}
	}
	return ""
}

func isPointerToLogLogger(candidate types.Type) bool {
	pointer, isPointer := types.Unalias(candidate).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(pointer.Elem()).(*types.Named)
	if !isNamed {
		return false
	}
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "log" && named.Obj().Name() == "Logger"
}

// reachedByDefer is whether a live block holding a defer statement leads to block along the graph's edges. The
// walk runs backward from block through its predecessors, so it visits exactly the blocks a path to block can pass
// through before it.
func reachedByDefer(block *cfg.Block, predecessors map[*cfg.Block][]*cfg.Block) bool {
	visited := map[*cfg.Block]bool{}
	pending := append([]*cfg.Block{}, predecessors[block]...)
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[current] {
			continue
		}
		visited[current] = true
		// A block no path from the entry reaches never runs, so its defer is never pending.
		if current.Live && holdsDefer(current.Nodes) {
			return true
		}
		pending = append(pending, predecessors[current]...)
	}
	return false
}

func holdsDefer(nodes []ast.Node) bool {
	for _, node := range nodes {
		if _, isDefer := node.(*ast.DeferStmt); isDefer {
			return true
		}
	}
	return false
}
