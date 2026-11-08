package loop_closure

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

// forEachLastStatement calls onLast on each last statement of statements, the original's forEachLastStmt. Last is
// defined recursively: when the last statement is an if, each branch's last statements are visited, and so for a
// loop's body, each case of a switch or type switch, and each clause of a select.
func forEachLastStatement(statements []ast.Stmt, onLast func(last ast.Stmt)) {
	if len(statements) == 0 {
		return
	}
	switch last := statements[len(statements)-1].(type) {
	case *ast.IfStmt:
		// The chain `if ... else if ... else ...`, followed to its end: each if's body, and the final else's block.
		for branch := last; branch != nil; {
			forEachLastStatement(branch.Body.List, onLast)
			if alternative, isBlock := branch.Else.(*ast.BlockStmt); isBlock {
				forEachLastStatement(alternative.List, onLast)
			}
			// The next if of the chain, or nil once it ends, in an else block or with no else at all.
			chained, _ := branch.Else.(*ast.IfStmt) // Ignored: a missing next if is the nil that ends the chain.
			branch = chained
		}
	case *ast.ForStmt:
		forEachLastStatement(last.Body.List, onLast)
	case *ast.RangeStmt:
		forEachLastStatement(last.Body.List, onLast)
	case *ast.SwitchStmt:
		forEachCaseBody(last.Body, onLast)
	case *ast.TypeSwitchStmt:
		forEachCaseBody(last.Body, onLast)
	case *ast.SelectStmt:
		for _, clause := range last.Body.List {
			// A select's body holds only communication clauses, so nothing is skipped here.
			if communication, isCommunication := clause.(*ast.CommClause); isCommunication {
				forEachLastStatement(communication.Body, onLast)
			}
		}
	default:
		onLast(last)
	}
}

// forEachCaseBody visits the last statements of each case clause in body, a switch's or a type switch's.
func forEachCaseBody(body *ast.BlockStmt, onLast func(last ast.Stmt)) {
	for _, clause := range body.List {
		// A switch's body holds only case clauses, so nothing is skipped here.
		if caseClause, isCaseClause := clause.(*ast.CaseClause); isCaseClause {
			forEachLastStatement(caseClause.Body, onLast)
		}
	}
}

// functionLiteralStatements is the statements of function's body when function is a function literal, as written,
// so `go (func() { ... })()` is not one, and nil otherwise, the original's litStmts.
func functionLiteralStatements(function ast.Expr) []ast.Stmt {
	literal, isLiteral := function.(*ast.FuncLit)
	if !isLiteral {
		return nil
	}
	return literal.Body.List
}

// asynchronousFunction is the function call runs in another goroutine and does not wait for, the original's goInvoke:
// the argument of golang.org/x/sync/errgroup's (*Group).Go, and nil for any other call.
func asynchronousFunction(information *types.Info, call *ast.CallExpr) ast.Expr {
	// A call of the real Go always has its one argument. A package at errgroup's path whose Go takes any number of
	// functions may be called with none, where the original indexes past the end and panics, and launches nothing.
	if !isMethodCall(information, call, "golang.org/x/sync/errgroup", "Group", "Go") || len(call.Args) == 0 {
		return nil
	}
	return call.Args[0]
}

// parallelSubtest is the statements of a testing.T.Run call's function literal that run concurrently with the loop,
// the original's parallelSubtest: those after a call to t.Parallel on the literal's own *testing.T, following the
// literal's final labeled statement. Statements before t.Parallel run synchronously, which `test := test` before it
// relies on, and a jump to a label may skip t.Parallel or the reference, so a label starts the count again.
func parallelSubtest(information *types.Info, call *ast.CallExpr) []ast.Stmt {
	if !isMethodCall(information, call, "testing", "T", "Run") {
		return nil
	}
	// `t.Run(subtest())` passes both arguments in one call, and no literal.
	if len(call.Args) != 2 {
		return nil
	}
	literal, isLiteral := call.Args[1].(*ast.FuncLit)
	if !isLiteral {
		return nil
	}
	// The literal's *testing.T, which an unnamed parameter, `func(*testing.T)`, leaves without a name to call
	// Parallel on. A literal given the real Run has its one parameter. A package at testing's path whose Run takes a
	// function of none has no *testing.T at all, where the original indexes past the end and panics.
	parameters := literal.Type.Params.List
	if len(parameters) == 0 || len(parameters[0].Names) == 0 {
		return nil
	}
	subtest := information.Defs[parameters[0].Names[0]]
	if subtest == nil {
		return nil
	}
	// The body's statements dominate those after them unless there is a jump, a simple and fast dominance that is
	// right often enough.
	var concurrent []ast.Stmt
	afterParallel := false
	for _, listed := range literal.Body.List {
		statement, labeled := unlabel(listed)
		if labeled {
			concurrent = nil
			afterParallel = false
		}
		if afterParallel {
			concurrent = append(concurrent, statement)
			continue
		}
		afterParallel = callsParallelOn(information, statement, subtest)
	}
	return concurrent
}

// callsParallelOn reports whether statement is a call of testing.T.Parallel on subtest, named as written,
// `subtest.Parallel()`, not through parentheses.
func callsParallelOn(information *types.Info, statement ast.Stmt, subtest types.Object) bool {
	expressionStatement, isExpressionStatement := statement.(*ast.ExprStmt)
	if !isExpressionStatement {
		return false
	}
	call, isCall := expressionStatement.X.(*ast.CallExpr)
	if !isCall || !isMethodCall(information, call, "testing", "T", "Parallel") {
		return false
	}
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	receiver, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && information.Uses[receiver] == subtest
}

// unlabel is statement without its labels, nested ones included, and whether it had one, the original's unlabel.
func unlabel(statement ast.Stmt) (ast.Stmt, bool) {
	inner := statement
	labeled := false
	for {
		labeledStatement, isLabeled := inner.(*ast.LabeledStmt)
		if !isLabeled {
			return inner, labeled
		}
		labeled = true
		inner = labeledStatement.Stmt
	}
}

// isMethodCall reports whether call calls the method named method of the package-level type typeName declared in
// the package at packagePath, through a pointer receiver or not, the original's isMethodCall over x/tools'
// typesinternal.IsMethodNamed.
func isMethodCall(
	information *types.Info,
	call *ast.CallExpr,
	packagePath string,
	typeName string,
	method string,
) bool {
	function, isFunction := typeutil.Callee(information, call).(*types.Func)
	if !isFunction || function.Name() != method {
		return false
	}
	receiver := function.Signature().Recv()
	if receiver == nil {
		return false
	}
	named := receiverNamed(receiver)
	if named == nil {
		return false
	}
	// x/tools' typesinternal.IsTypeNamed, on a type already named, and its IsPackageLevel: a type declared inside a
	// function is not the package's, whatever its name.
	declared := named.Obj()
	return declared != nil &&
		declared.Pkg() != nil &&
		declared.Parent() == declared.Pkg().Scope() &&
		declared.Pkg().Path() == packagePath &&
		declared.Name() == typeName
}

// receiverNamed is the named type of receiver, looking through one pointer and through aliases, and nil when there
// is none, as for a method of an interface literal, x/tools' typesinternal.ReceiverNamed less the pointer it reports.
func receiverNamed(receiver *types.Var) *types.Named {
	receiverType := types.Unalias(receiver.Type())
	if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
		receiverType = types.Unalias(pointer.Elem())
	}
	named, isNamed := receiverType.(*types.Named)
	if !isNamed {
		return nil
	}
	return named
}
