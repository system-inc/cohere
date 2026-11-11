// Package correctness_require_drained_child_output is the Go engine's rule that a child's piped output is read
// before the code waits for the child to exit.
//
// exec.Cmd's StdoutPipe and StderrPipe hand back the read end of a pipe the child writes into. The pipe holds about
// 64 KB, and past that the child blocks on its next write until someone reads, so it never exits and Wait, or Run,
// which waits, never returns. os/exec's documentation calls a Wait before the reads have finished incorrect, and vet
// does not check it. Servers.app hung this way in Swift: a ps every three seconds, waited on before its output was
// read, parked a hung child each time its table outgrew the buffer. It is the Go twin of
// nexus/correctness-require-drained-child-output.
//
// Each function is judged on its own control-flow graph. A pipe is a local variable the function declares from
// StdoutPipe or StderrPipe and never writes again, of a command the function names and never reassigns. Any mention
// of the pipe after it is made, outside a defer statement, counts as handing it to something that reads it, a
// goroutine started before the wait included, since the rule cannot see how far a callee reads. Unknown falls to
// silence: a wait is reported only when some path from the pipe's creation reaches it with no mention on the way.
package correctness_require_drained_child_output

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/cfg"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/child_process"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-drained-child-output"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_require_drained_child_output",
	Doc: "Flags exec.Cmd's Wait or Run reached through the function's control-flow graph from a StdoutPipe or " +
		"StderrPipe of the same command with nothing reading the pipe on the way, since output past the pipe " +
		"buffer blocks the child and the wait never returns.",
	// ctrlflow builds each function's graph with calls that never return ending their path, so a wait after an
	// os.Exit or a log.Fatal is never reached.
	Requires: []*analysis.Analyzer{ctrlflow.Analyzer, inspect.Analyzer},
	Run:      run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var pipeUnreadAtWaitMessage = policy.MessageOf(Name, "pipeUnreadAtWait")

func run(pass *analysis.Pass) (any, error) {
	graphs, isGraphs := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isGraphs || !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("correctness_require_drained_child_output: a required analyzer's result is missing")
	}
	child_process.Functions(pass, inspected, graphs, func(body *ast.BlockStmt, graph *cfg.CFG) {
		judgeGraph(pass, body, graph)
	})
	return nil, nil
}

// judgeGraph reports, for every pipe a live block of graph makes, each wait on its command that some path from the
// pipe's creation reaches with nothing mentioning the pipe on the way. body is the function's body, read whole for
// the writes that make a pipe or a command one the rule cannot follow.
func judgeGraph(pass *analysis.Pass, body *ast.BlockStmt, graph *cfg.CFG) {
	for _, block := range graph.Blocks {
		if !block.Live {
			continue
		}
		for index, node := range block.Nodes {
			made, isPipe := child_process.PipeMade(pass, node)
			if !isPipe || child_process.Rewritten(pass, body, made) {
				continue
			}
			made.Block = block
			made.Index = index
			for _, wait := range unreadWaits(pass, made) {
				method, _ := child_process.CommandMethod(pass, wait)
				pass.Report(analysis.Diagnostic{
					Pos:      wait.Pos(),
					End:      wait.End(),
					Category: pipeUnreadAtWaitMessage.Id,
					Message:  pipeUnreadAtWaitMessage.Render(map[string]string{"stream": made.Stream, "wait": method}),
				})
			}
		}
	}
}

// unreadWaits are the waits on the pipe's command that a path from just after the pipe's creation reaches with no
// node on the way mentioning the pipe, each once, in the order the walk meets them. A path ends at a node that
// mentions the pipe, at a wait, and at the creation itself, which a loop that leads back to it makes a new pipe that
// is judged on its own. The creation's block is entered mid-way first, and from its top if a loop returns to it.
func unreadWaits(pass *analysis.Pass, made child_process.Pipe) []*ast.CallExpr {
	type step struct {
		block *cfg.Block
		from  int
	}
	waits := []*ast.CallExpr{}
	reported := map[*ast.CallExpr]bool{}
	visited := map[*cfg.Block]bool{}
	pending := []step{{block: made.Block, from: made.Index + 1}}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		ended := false
		for _, node := range current.block.Nodes[current.from:] {
			if node == made.Made || child_process.Mentions(pass, node, made.Variable) {
				ended = true
				break
			}
			found := child_process.WaitsIn(pass, node, made.Command)
			for _, wait := range found {
				if !reported[wait] {
					reported[wait] = true
					waits = append(waits, wait)
				}
			}
			if len(found) > 0 {
				ended = true
				break
			}
		}
		if ended {
			continue
		}
		for _, successor := range current.block.Succs {
			// A block no path from the entry reaches never runs.
			if !successor.Live || visited[successor] {
				continue
			}
			visited[successor] = true
			pending = append(pending, step{block: successor, from: 0})
		}
	}
	return waits
}
