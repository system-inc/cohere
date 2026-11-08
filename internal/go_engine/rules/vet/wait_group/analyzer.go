// Package wait_group is vet's waitgroup pass, a call to (*sync.WaitGroup).Add as the first statement of a function
// literal that a go statement starts, `go func() { group.Add(1); ... }()`. The new goroutine may not have run yet
// when Wait is called, so Wait can return before the work it was meant to wait for has begun; Add belongs before the
// go statement. It sits on the engine's syntax dispatcher, hearing each call on the walk the dispatcher makes once for
// every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/waitgroup/waitgroup.go, whose
// logic it keeps and whose message it reports word for word, at the same position. The original walks every call
// with the inspector's stack and matches the stack's last six nodes by type; the port hears the same calls in the
// same preorder with the dispatcher's stack of their ancestors and matches the same six nodes, the call and the five
// ancestors nearest it. The registry runs the port in the original's place, and the original stays the oracle: the
// parity tests beside this file run both over the same packages. The two agree everywhere, so there is no
// differences fixture.
package wait_group

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "waitgroup"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for misuses of sync.WaitGroup\n\n" +
	"This analyzer detects mistaken calls to the (*sync.WaitGroup).Add\n" +
	"method from inside a new goroutine, causing Add to race with Wait:\n\n" +
	"\t// WRONG\n" +
	"\tvar wg sync.WaitGroup\n" +
	"\tgo func() {\n" +
	"\t        wg.Add(1) // \"WaitGroup.Add called from inside new goroutine\"\n" +
	"\t        defer wg.Done()\n" +
	"\t        ...\n" +
	"\t}()\n" +
	"\twg.Wait() // (may return prematurely before new goroutine starts)\n\n" +
	"The correct code calls Add before starting the goroutine:\n\n" +
	"\t// RIGHT\n" +
	"\tvar wg sync.WaitGroup\n" +
	"\twg.Add(1)\n" +
	"\tgo func() {\n" +
	"\t\tdefer wg.Done()\n" +
	"\t\t...\n" +
	"\t}()\n" +
	"\twg.Wait()"

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/waitgroup"

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, with its ancestors,
// as the original's WithStack visits them, judged only in a package that imports sync, which it asks once for the
// package, on the first call, and keeps in the context's state.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node:  checkCall,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	return analyzer
}

// packageGate is what the port asks once of a package, in the context's state: whether it imports sync, as the
// original asks before it walks anything.
type packageGate struct {
	// decided is whether the import has been asked of the package yet.
	decided bool
	// importsSync is its answer, once decided.
	importsSync bool
}

// checkCall reports node when it calls (*sync.WaitGroup).Add as the first statement of a function literal a go
// statement starts, judged from stack, the call's ancestors, outermost first.
func checkCall(context *rule.Context, node ast.Node, stack []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	// A package that does not import sync itself is skipped whole, as the original skips it, even where it reaches a
	// WaitGroup through another package's declarations.
	gate := rule.State[packageGate](context)
	if !gate.decided {
		gate.decided, gate.importsSync = true, imports(context.Package, "sync")
	}
	if !gate.importsSync {
		return
	}
	if !isMethodNamed(typeutil.Callee(context.TypesInfo, call), "sync", "WaitGroup", "Add") {
		return
	}
	if !startsNewGoroutine(stack) {
		return
	}
	context.Reportf(call.Lparen, "WaitGroup.Add called from inside new goroutine")
}

// startsNewGoroutine reports whether the call whose ancestors stack holds, outermost first, is the first statement
// of a function literal that a go statement's call holds, `go func() { call; ... }()`: the original's wantSuffix of
// the inspector's stack, matched node type by node type from the call up, its parent the last of stack. As in the
// original, the function literal is any child of the go statement's call, its function or one of its arguments, `go
// run(func() { call })`, since the stack records only node types.
func startsNewGoroutine(stack []ast.Node) bool {
	// The five ancestors judged, the go statement among them. A shorter stack, a call nearer its file, has no go
	// statement above it to match, as the original's suffix is longer than its stack there.
	if len(stack) < 5 {
		return false
	}
	statement, isExpressionStatement := stack[len(stack)-1].(*ast.ExprStmt)
	if !isExpressionStatement {
		return false
	}
	block, isBlock := stack[len(stack)-2].(*ast.BlockStmt)
	// The block holds the statement, so its list is never empty. Only the first statement is judged: by a later one,
	// the goroutine may already have done what the reader cannot see from here.
	if !isBlock || block.List[0] != statement {
		return false
	}
	if _, isLiteral := stack[len(stack)-3].(*ast.FuncLit); !isLiteral {
		return false
	}
	if _, isStartedCall := stack[len(stack)-4].(*ast.CallExpr); !isStartedCall {
		return false
	}
	_, isGoStatement := stack[len(stack)-5].(*ast.GoStmt)
	return isGoStatement
}

// imports reports whether importer imports the package at path directly. x/tools' typesinternal.Imports.
func imports(importer *types.Package, path string) bool {
	for _, imported := range importer.Imports() {
		if imported.Path() == path {
			return true
		}
	}
	return false
}

// isMethodNamed reports whether object is a method of a package-level type named typeName in the package at
// packagePath, by value or pointer receiver, named one of names, and false for nil. x/tools'
// typesinternal.IsMethodNamed.
func isMethodNamed(object types.Object, packagePath string, typeName string, names ...string) bool {
	function, isFunction := object.(*types.Func)
	if !isFunction {
		return false
	}
	receiver := function.Signature().Recv()
	if receiver == nil {
		return false
	}
	named := receiverNamed(receiver)
	return named != nil && isTypeNamed(named, packagePath, typeName) && slices.Contains(names, function.Name())
}

// receiverNamed is the named type of receiver, looking through one pointer and aliases, and nil when there is none.
// x/tools' typesinternal.ReceiverNamed, less the pointer flag nothing here reads.
func receiverNamed(receiver *types.Var) *types.Named {
	receiverType := receiver.Type()
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	named, _ := types.Unalias(receiverType).(*types.Named) // A receiver of no named type, an interface's, is nil.
	return named
}

// isTypeNamed reports whether typed is, or aliases, a package-level defined type in the package at packagePath named
// one of names. x/tools' typesinternal.IsTypeNamed.
func isTypeNamed(typed types.Type, packagePath string, names ...string) bool {
	named, isNamed := types.Unalias(typed).(*types.Named)
	if !isNamed {
		return false
	}
	typeName := named.Obj()
	return typeName != nil && isPackageLevel(typeName) && typeName.Pkg().Path() == packagePath &&
		slices.Contains(names, typeName.Name())
}

// isPackageLevel reports whether object is declared in its package's scope. x/tools' typesinternal.IsPackageLevel.
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}
