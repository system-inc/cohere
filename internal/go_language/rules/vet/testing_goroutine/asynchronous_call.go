package testing_goroutine

import "go/ast"

// asynchronousCall is a region of code to judge for forbidden calls, as one call starts it asynchronously: a
// `go fun()` or a `t.Run(name, fun)`. x/tools' asyncCall.
type asynchronousCall struct {
	// region is the code judged for forbidden calls.
	region ast.Node
	// asynchronous is the *ast.GoStmt or the t.Run *ast.CallExpr that starts region.
	asynchronous ast.Node
	// scope is where a test must be declared for a forbidden call on it to be the subtest's own, and nil for a go
	// statement, where no test's call is its own.
	scope ast.Node
	// function is fun, as written in `go fun()` or `t.Run(name, fun)`, less its parentheses.
	function ast.Expr
}
