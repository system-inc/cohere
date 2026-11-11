// Package concurrency_require_joined_pipe_reader is the Go engine's rule that a goroutine reading a child's piped
// output is joined before the code waits for the child.
//
// exec.Cmd's Wait closes the read end of every pipe from StdoutPipe or StderrPipe once the child exits, and os/exec's
// documentation calls a Wait before the reads have finished incorrect. A goroutine copying the pipe keeps the child
// from blocking, which correctness-require-drained-child-output accepts as a drain, but a Wait that does not first
// join that goroutine can close the pipe while output is still buffered in it, and the goroutine loses the tail: the
// last lines of a log, the end of a JSON document, with no error the reader is sure to see. It is the companion of the
// drained-output rule, #pte6ec9's second.
//
// Each function is judged on its own control-flow graph. A reader is a go statement, or a sync.WaitGroup's or an
// errgroup.Group's Go, that mentions a pipe the function makes. Its signals are what the code could join it by: the
// group whose Go started it, and every channel, WaitGroup or errgroup.Group it captures from the function. A wait on
// the pipe's command is reported when some path from the reader's start reaches it with no node on the way mentioning
// a signal. A reader with no signal can't be joined, so any wait after it is reported.
//
// Unknown falls to silence. A reader is not judged when it calls a function of this package or a function value,
// which could signal it by means the rule cannot see, captures a mutex, a condition, a context or a value of
// sync/atomic, or reads a package variable of one of those kinds or of a channel or group. Nor is it judged when one
// of its signals is handed to a call or mentioned in another function literal, since another goroutine may forward
// the join. The pipe or its command written again is silence, as in the drained-output rule.
package concurrency_require_joined_pipe_reader

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/child_process"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/concurrency-require-joined-pipe-reader"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "concurrency_require_joined_pipe_reader",
	Doc: "Flags exec.Cmd's Wait or Run reached through the function's control-flow graph from a goroutine reading " +
		"a StdoutPipe or StderrPipe of the same command, before anything joins that goroutine, since Wait closes the " +
		"pipe once the child exits and the goroutine can lose the output's tail.",
	// ctrlflow builds each function's graph with calls that never return ending their path, so a wait after an
	// os.Exit or a log.Fatal is never reached.
	Requires: []*analysis.Analyzer{ctrlflow.Analyzer, inspect.Analyzer},
	Run:      run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var readerUnjoinedAtWaitMessage = policy.MessageOf(Name, "readerUnjoinedAtWait")

// kind is what a captured variable's type says about joining a goroutine.
type kind int

const (
	// kindPlain is a value with no part in synchronizing: a buffer, a writer, a slice, a count.
	kindPlain kind = iota
	// kindSignal is a channel, a sync.WaitGroup or an errgroup.Group, which the code joins a goroutine by.
	kindSignal
	// kindUnknown is a mutex, a condition, a once, a context, a value of sync/atomic or a function, which could
	// synchronize in a way the rule can't follow.
	kindUnknown
)

func run(pass *analysis.Pass) (any, error) {
	graphs, isGraphs := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isGraphs || !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("concurrency_require_joined_pipe_reader: a required analyzer's result is missing")
	}
	child_process.Functions(pass, inspected, graphs, func(body *ast.BlockStmt, graph *cfg.CFG) {
		judgeGraph(pass, body, graph)
	})
	return nil, nil
}

// reader is one goroutine started to read a pipe: the graph node that starts it, at its index in its block, and the
// objects the code could join it by.
type reader struct {
	started ast.Node
	block   *cfg.Block
	index   int
	signals []types.Object
}

// judgeGraph reports, for every pipe a live block of graph makes and every goroutine started to read it, each wait on
// the pipe's command that some path from the goroutine's start reaches with no node on the way mentioning one of its
// signals.
func judgeGraph(pass *analysis.Pass, body *ast.BlockStmt, graph *cfg.CFG) {
	for _, block := range graph.Blocks {
		if !block.Live {
			continue
		}
		for _, node := range block.Nodes {
			made, isPipe := child_process.PipeMade(pass, node)
			if !isPipe || child_process.Rewritten(pass, body, made) {
				continue
			}
			for _, started := range readers(pass, body, graph, made) {
				for _, wait := range unjoinedWaits(pass, started, made.Command) {
					method, _ := child_process.CommandMethod(pass, wait)
					pass.Report(analysis.Diagnostic{
						Pos:      wait.Pos(),
						End:      wait.End(),
						Category: readerUnjoinedAtWaitMessage.Id,
						Message: readerUnjoinedAtWaitMessage.Render(map[string]string{
							"stream": made.Stream,
							"wait":   method,
						}),
					})
				}
			}
		}
	}
}

// readers are the goroutines a live block of graph starts that mention the pipe, each one the rule can judge.
func readers(pass *analysis.Pass, body *ast.BlockStmt, graph *cfg.CFG, made child_process.Pipe) []reader {
	found := []reader{}
	for _, block := range graph.Blocks {
		if !block.Live {
			continue
		}
		for index, node := range block.Nodes {
			start, runs, group := goroutineStart(pass, node)
			if start == nil || !mentionsIn(pass, runs, made.Variable) {
				continue
			}
			signals, judged := signalsOf(pass, runs, group)
			if !judged || escapes(pass, body, start, signals) {
				continue
			}
			found = append(found, reader{started: node, block: block, index: index, signals: signals})
		}
	}
	return found
}

// goroutineStart reads node as starting a goroutine: the call that starts it, what the goroutine runs, and the group
// it was handed to. A go statement's call starts it and runs in it, and a sync.WaitGroup's or an errgroup.Group's Go
// starts the function literal it is handed, which is what runs. A Go of any other type is never one.
func goroutineStart(pass *analysis.Pass, node ast.Node) (*ast.CallExpr, ast.Node, types.Object) {
	if statement, isGo := node.(*ast.GoStmt); isGo {
		return statement.Call, statement.Call, nil
	}
	var expression ast.Expr
	switch statement := node.(type) {
	case *ast.ExprStmt:
		expression = statement.X
	case *ast.AssignStmt:
		if len(statement.Rhs) == 1 {
			expression = statement.Rhs[0]
		}
	}
	call, isCall := ast.Unparen(expression).(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return nil, nil, nil
	}
	callee := typeutil.StaticCallee(pass.TypesInfo, call)
	if callee == nil || callee.Name() != "Go" || callee.Signature().Recv() == nil {
		return nil, nil, nil
	}
	receiverType := callee.Signature().Recv().Type()
	if !child_process.IsNamed(receiverType, "sync", "WaitGroup") &&
		!child_process.IsNamed(receiverType, "golang.org/x/sync/errgroup", "Group") {
		return nil, nil, nil
	}
	literal, isLiteral := ast.Unparen(call.Args[0]).(*ast.FuncLit)
	if !isLiteral {
		return nil, nil, nil
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return nil, nil, nil
	}
	group, isNamed := child_process.CommandOf(pass.TypesInfo, selector.X)
	if !isNamed || group.Path != "" {
		return nil, nil, nil
	}
	return call, literal, group.Root
}

// signalsOf is every object the code could join the goroutine running runs by, and whether the rule can judge it at
// all. group, when the goroutine was handed to a group's Go, is the first signal. A captured object the rule can't
// read the meaning of, a field of a kind that signals, or a call into this package or through a function value,
// makes the goroutine one it does not judge.
func signalsOf(pass *analysis.Pass, runs ast.Node, group types.Object) ([]types.Object, bool) {
	signals := []types.Object{}
	if group != nil {
		signals = append(signals, group)
	}
	seen := map[types.Object]bool{}
	judged := true
	// What the goroutine declares inside itself is its own, never a way to reach it from outside: every function
	// literal in what it runs, the goroutine's own included.
	literals := []*ast.FuncLit{}
	ast.Inspect(runs, func(node ast.Node) bool {
		if literal, isLiteral := node.(*ast.FuncLit); isLiteral {
			literals = append(literals, literal)
		}
		return true
	})
	declaredInside := func(variable *types.Var) bool {
		for _, literal := range literals {
			if literal.Pos() <= variable.Pos() && variable.Pos() < literal.End() {
				return true
			}
		}
		return false
	}
	ast.Inspect(runs, func(node ast.Node) bool {
		if !judged {
			return false
		}
		switch inner := node.(type) {
		case *ast.CallExpr:
			if !callIsReadable(pass, inner) {
				judged = false
			}
		case *ast.Ident:
			variable, isVariable := pass.TypesInfo.Uses[inner].(*types.Var)
			if !isVariable || seen[variable] || declaredInside(variable) {
				return true
			}
			seen[variable] = true
			// A field of a kind that signals, job.done closed or job.group released, joins the goroutine through a
			// value the rule doesn't name as a signal, so the goroutine isn't judged.
			if variable.IsField() {
				if kindOf(variable.Type()) != kindPlain {
					judged = false
				}
				return judged
			}
			switch kindOf(variable.Type()) {
			case kindSignal:
				if variable.Pkg() != nil && variable.Parent() == variable.Pkg().Scope() {
					judged = false
					return false
				}
				signals = append(signals, variable)
			case kindUnknown:
				judged = false
			case kindPlain:
			}
		}
		return judged
	})
	return signals, judged
}

// callIsReadable is whether a call inside a goroutine is one that cannot signal its end by a way the rule doesn't see:
// a conversion, a builtin, or a call of a function or method declared in another package. A call into this package,
// or through a function value, could close a channel or release a group the goroutine never names.
func callIsReadable(pass *analysis.Pass, call *ast.CallExpr) bool {
	if typeAndValue, isRecorded := pass.TypesInfo.Types[call.Fun]; isRecorded &&
		(typeAndValue.IsType() || typeAndValue.IsBuiltin()) {
		return true
	}
	if _, isLiteral := ast.Unparen(call.Fun).(*ast.FuncLit); isLiteral {
		return true
	}
	// An interface's method is read by the interface's own package: one declared here could be implemented to
	// signal, and one of another package, as io.Writer's Write, is that package's.
	callee, isFunction := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !isFunction || callee.Pkg() == nil {
		return false
	}
	return callee.Pkg() != pass.Pkg
}

// kindOf reads a captured variable's type, through a pointer.
func kindOf(candidate types.Type) kind {
	underlying := candidate
	if pointer, isPointer := types.Unalias(candidate).(*types.Pointer); isPointer {
		underlying = pointer.Elem()
	}
	if _, isChannel := underlying.Underlying().(*types.Chan); isChannel {
		return kindSignal
	}
	if _, isFunction := underlying.Underlying().(*types.Signature); isFunction {
		return kindUnknown
	}
	if child_process.IsNamed(underlying, "sync", "WaitGroup") ||
		child_process.IsNamed(underlying, "golang.org/x/sync/errgroup", "Group") {
		return kindSignal
	}
	named, isNamed := types.Unalias(underlying).(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil {
		return kindPlain
	}
	switch named.Obj().Pkg().Path() {
	case "sync", "sync/atomic":
		return kindUnknown
	case "context":
		if named.Obj().Name() == "Context" || named.Obj().Name() == "CancelFunc" {
			return kindUnknown
		}
	}
	return kindPlain
}

// escapes is whether any signal is handed to a call other than the one that starts the goroutine, or mentioned in a
// function literal outside that call, anywhere in body, so another goroutine or a callee may join the reader in its
// place.
func escapes(pass *analysis.Pass, body *ast.BlockStmt, start *ast.CallExpr, signals []types.Object) bool {
	isSignal := map[types.Object]bool{}
	for _, signal := range signals {
		isSignal[signal] = true
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == start {
			return false
		}
		switch inner := node.(type) {
		case *ast.FuncLit:
			for object := range isSignal {
				if mentionsIn(pass, inner, object) {
					found = true
				}
			}
			return false
		case *ast.CallExpr:
			for _, argument := range inner.Args {
				for object := range isSignal {
					if mentionsIn(pass, argument, object) {
						found = true
					}
				}
			}
		}
		return !found
	})
	return found
}

// unjoinedWaits are the waits on named that a path from just after the goroutine's start reaches with no node on the
// way mentioning one of its signals, each once. A path ends at a node that mentions a signal, at a wait, and at the
// start itself, which a loop that leads back to it makes a new goroutine judged on its own.
func unjoinedWaits(pass *analysis.Pass, started reader, named child_process.Command) []*ast.CallExpr {
	type step struct {
		block *cfg.Block
		from  int
	}
	waits := []*ast.CallExpr{}
	reported := map[*ast.CallExpr]bool{}
	visited := map[*cfg.Block]bool{}
	pending := []step{{block: started.block, from: started.index + 1}}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		ended := false
		for _, node := range current.block.Nodes[current.from:] {
			if node == started.started || joins(pass, node, started.signals) {
				ended = true
				break
			}
			found := child_process.WaitsIn(pass, node, named)
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

// joins is whether node mentions any signal outside a defer statement, which counts as joining the goroutine: a
// WaitGroup's Wait, a receive, a range or a select over the channel, and anything else done with it alike, since the
// rule has already set aside a signal that escapes. A deferred Wait runs on the way out, after the child's.
func joins(pass *analysis.Pass, node ast.Node, signals []types.Object) bool {
	for _, signal := range signals {
		if child_process.Mentions(pass, node, signal) {
			return true
		}
	}
	return false
}

// mentionsIn says node names object anywhere in it, function literals included.
func mentionsIn(pass *analysis.Pass, node ast.Node, object types.Object) bool {
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		if identifier, isIdentifier := inner.(*ast.Ident); isIdentifier && pass.TypesInfo.Uses[identifier] == object {
			found = true
		}
		return !found
	})
	return found
}
