// Package wait_group is vet's waitgroup pass, a call to (*sync.WaitGroup).Add as the first statement of a function
// literal that a go statement starts, `go func() { group.Add(1); ... }()`. The new goroutine may not have run yet
// when Wait is called, so Wait can return before the work it was meant to wait for has begun; Add belongs before the
// go statement.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/waitgroup/waitgroup.go, whose
// logic it keeps and whose message it reports word for word, at the same position. The original walks every call
// with the inspector's stack and matches the stack's last six nodes by type; the port walks the same calls in the
// same preorder with cursors and climbs the same six nodes through their parents. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture.
package wait_group

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "waitgroup",
	Doc: "check for misuses of sync.WaitGroup\n\n" +
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
		"\twg.Wait()",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/waitgroup",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	// A package that does not import sync itself is skipped whole, as the original skips it, even where it reaches a
	// WaitGroup through another package's declarations.
	if !imports(pass.Pkg, "sync") {
		return nil, nil
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("wait_group: inspect's result is missing")
	}
	// Preorder visits each call in the order the original's WithStack pushes it.
	for callCursor := range inspected.Root().Preorder((*ast.CallExpr)(nil)) {
		call, isCall := callCursor.Node().(*ast.CallExpr)
		if !isCall {
			// Invariant: Preorder filtered to calls yields only calls.
			panic("wait_group: Preorder over calls yielded another node")
		}
		if !isMethodNamed(typeutil.Callee(pass.TypesInfo, call), "sync", "WaitGroup", "Add") {
			continue
		}
		if !startsNewGoroutine(callCursor) {
			continue
		}
		pass.Reportf(call.Lparen, "WaitGroup.Add called from inside new goroutine")
	}
	return nil, nil
}

// startsNewGoroutine reports whether the call at callCursor is the first statement of a function literal that a go
// statement's call holds, `go func() { call; ... }()`: the original's wantSuffix of the inspector's stack, matched
// node type by node type from the call up. As in the original, the function literal is any child of the go
// statement's call, its function or one of its arguments, `go run(func() { call })`, since the stack records only
// node types. Each climb stops at the first node of the wrong type, so it never reaches the root, which has no
// parent.
func startsNewGoroutine(callCursor inspector.Cursor) bool {
	statementCursor := callCursor.Parent()
	statement, isExpressionStatement := statementCursor.Node().(*ast.ExprStmt)
	if !isExpressionStatement {
		return false
	}
	blockCursor := statementCursor.Parent()
	block, isBlock := blockCursor.Node().(*ast.BlockStmt)
	// The block holds the statement, so its list is never empty. Only the first statement is judged: by a later one,
	// the goroutine may already have done what the reader cannot see from here.
	if !isBlock || block.List[0] != statement {
		return false
	}
	literalCursor := blockCursor.Parent()
	if _, isLiteral := literalCursor.Node().(*ast.FuncLit); !isLiteral {
		return false
	}
	startedCursor := literalCursor.Parent()
	if _, isStartedCall := startedCursor.Node().(*ast.CallExpr); !isStartedCall {
		return false
	}
	_, isGoStatement := startedCursor.Parent().Node().(*ast.GoStmt)
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
