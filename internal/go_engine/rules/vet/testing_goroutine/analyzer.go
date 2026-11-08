// Package testing_goroutine is vet's testinggoroutine pass, a call that ends a test abruptly, t.Fatal, t.FailNow,
// t.Skip and their kin, made from a goroutine the test started, where it stops that goroutine and not the test. A
// region of code is judged when a test or benchmark starts it asynchronously: the body of `go fun()`, where fun is a
// function literal, a local variable first assigned one, or a function or method this package declares, and, behind
// the experimental subtest flag, the body of `t.Run(name, fun)`, where the call is reported when its receiver is a
// test declared outside the subtest.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0
// go/analysis/passes/testinggoroutine/testinggoroutine.go and util.go, whose logic it keeps and whose messages it
// reports word for word, at the same position. The registry runs the port in the original's place, and the original
// stays the oracle: the parity tests beside this file run both over the same packages, with the subtest flag on and
// off, and agree there.
//
// They differ in one place, where the port is right (#x1m1wtm), and the differences fixture records each finding it
// changes. A go statement's call arguments, and a t.Run's name, are evaluated by the goroutine executing the
// statement, not the one it starts, so the port judges them as part of the region enclosing the statement: a
// forbidden call there is the test's own when that is the test itself, and goes unreported, and is reported against
// the enclosing goroutine when the statement is in a region of its own. The original judges them with the region the
// statement starts. A function literal an argument hands over, rather than calls in place, stays with the started
// region, as in the original, since the started goroutine is what is likely to call it. The other change is that the
// original's unchecked assertion on a parameter's type, which panics on a parameter typed `*a.b.T`, is checked here;
// such a parameter does not type-check, and the checker runs neither on a package that does not.
package testing_goroutine

import (
	"fmt"
	"go/ast"
	"go/token"
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
	Name: "testinggoroutine",
	Doc: "report calls to (*testing.T).Fatal from goroutines started by a test\n\n" +
		"Functions that abruptly terminate a test, such as the Fatal, Fatalf, FailNow, and\n" +
		"Skip{,f,Now} methods of *testing.T, must be called from the test goroutine itself.\n" +
		"This checker detects calls to these functions that occur within a goroutine\n" +
		"started by the test. For example:\n\n" +
		"\tfunc TestFoo(t *testing.T) {\n" +
		"\t    go func() {\n" +
		"\t        t.Fatal(\"oops\") // error: (*T).Fatal called from non-test goroutine\n" +
		"\t    }()\n" +
		"\t}",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/testinggoroutine",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// reportSubtest is the subtest flag, the original's, by name, default and usage: whether a forbidden call in the
// function a t.Run starts, on a test declared outside it, is reported too.
var reportSubtest bool

func init() {
	Analyzer.Flags.BoolVar(
		&reportSubtest, "subtest", false, "whether to check if t.Run subtest is terminated correctly; experimental",
	)
}

// forbidden is each method of the testing package that ends the test it is called on, and must be called from that
// test's own goroutine.
var forbidden = []string{
	"FailNow",
	"Fatal",
	"Fatalf",
	"Skip",
	"Skipf",
	"SkipNow",
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("testing_goroutine: inspect's result is missing")
	}
	// A package that imports no testing package calls none of its methods by its own hand.
	if !imports(pass.Pkg, "testing") {
		return nil, nil
	}
	declarationOf := localFunctionDeclarations(pass.TypesInfo, pass.Files)
	// startedAt maps each region whose statements run concurrently with some test to the asynchronous calls that start
	// it. A test helper may be started from several places, so one region may have several. regions holds them in the
	// order they were first found, the order the original reports in.
	startedAt := map[ast.Node][]*asynchronousCall{}
	regions := []ast.Node{}
	add := func(started *asynchronousCall) {
		if started == nil {
			return
		}
		if startedAt[started.region] == nil {
			regions = append(regions, started.region)
		}
		startedAt[started.region] = append(startedAt[started.region], started)
	}
	// Every `go fun()` and `t.Run(name, fun)`, in preorder, as the original's Nodes visits them. A function declaration
	// whose parameters take no *testing.T or *testing.B is not descended into, so what it starts is never judged, and
	// that pruning is why the walk is the cursor's Inspect rather than inspector.All, which visits every node.
	inspected.Root().Inspect(
		[]ast.Node{(*ast.FuncDecl)(nil), (*ast.GoStmt)(nil), (*ast.CallExpr)(nil)},
		func(cursor inspector.Cursor) bool {
			switch node := cursor.Node().(type) {
			case *ast.FuncDecl:
				return hasBenchmarkOrTestParameters(node)
			case *ast.GoStmt:
				add(goAsynchronousCall(pass.TypesInfo, node, declarationOf))
			case *ast.CallExpr:
				add(runAsynchronousCall(pass.TypesInfo, node))
			}
			return true
		},
	)
	// Each forbidden call within a region, which includes the region being a `go t.Fatal()` itself, is judged once for
	// each call that starts the region.
	for _, region := range regions {
		judge(pass, region, startedAt)
	}
	return nil, nil
}

// report reports call, a call of the forbidden method, whose name is forbiddenName, on receiver, made in the region
// started. A function literal started directly is reported at the forbidden call; anything else, a variable, a
// function or a method value, at the go statement or t.Run that starts it, a name saying what it calls.
func report(
	pass *analysis.Pass,
	started *asynchronousCall,
	call *ast.CallExpr,
	receiver *types.Var,
	forbiddenName string,
) {
	calledThrough := ""
	var where analysis.Range = started.asynchronous
	if _, isLiteral := started.function.(*ast.FuncLit); isLiteral {
		where = call
	} else if name, isIdentifier := started.function.(*ast.Ident); isIdentifier {
		calledThrough = fmt.Sprintf(" (%s calls %s)", name.Name, forbiddenName)
	}
	if _, isGo := started.asynchronous.(*ast.GoStmt); isGo {
		pass.ReportRangef(where, "call to %s from a non-test goroutine%s", forbiddenName, calledThrough)
		return
	}
	if reportSubtest {
		pass.ReportRangef(
			where, "call to %s on %s defined outside of the subtest%s", forbiddenName, receiver.Name(), calledThrough,
		)
	}
}

// hasBenchmarkOrTestParameters reports whether declaration takes a *testing.T or a *testing.B, judged by how its
// parameter types are written. x/tools' hasBenchmarkOrTestParams.
func hasBenchmarkOrTestParameters(declaration *ast.FuncDecl) bool {
	return slices.ContainsFunc(declaration.Type.Params.List, func(parameter *ast.Field) bool {
		return isTestingTOrB(parameter.Type)
	})
}

// isTestingTOrB reports whether expression is written `*testing.T` or `*testing.B`, by the package's name `testing`, so
// a renamed or dot import of testing does not count. x/tools' typeIsTestingDotTOrB, less the name it also returns,
// which no caller reads.
func isTestingTOrB(expression ast.Expr) bool {
	star, isStar := expression.(*ast.StarExpr)
	if !isStar {
		return false
	}
	selector, isSelector := star.X.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	// The original asserts this unchecked. A type's selector is always a package's name, so only code that does not
	// type-check, which the checker never hands a pass, could make it anything else.
	packageName, isIdentifier := selector.X.(*ast.Ident)
	if !isIdentifier || packageName.Name != "testing" {
		return false
	}
	return selector.Sel.Name == "B" || selector.Sel.Name == "T"
}

// withinScope reports whether variable is declared within scope, from its start to its end inclusive, and false when
// there is no scope, or variable has no position.
func withinScope(scope ast.Node, variable *types.Var) bool {
	if scope == nil {
		return false
	}
	return variable.Pos() != token.NoPos && scope.Pos() <= variable.Pos() && variable.Pos() <= scope.End()
}

// goAsynchronousCall is the region statement starts: the function literal its callee is first assigned, the
// declaration in this package of the function or method it statically calls, or else the go statement itself, so
// `go t.Fatal()` and `go func() { t.Fatal() }()` are judged.
func goAsynchronousCall(
	information *types.Info,
	statement *ast.GoStmt,
	declarationOf func(*types.Func) *ast.FuncDecl,
) *asynchronousCall {
	function := ast.Unparen(statement.Call.Fun)
	if identifier := usedIdentifier(information, function); identifier != nil {
		if literal := functionLiteralInScope(identifier); literal != nil {
			return &asynchronousCall{region: literal, asynchronous: statement, scope: nil, function: function}
		}
	}
	// A static call of a function or method this package declares runs that declaration's body.
	if callee := typeutil.StaticCallee(information, statement.Call); callee != nil {
		if declaration := declarationOf(callee); declaration != nil {
			return &asynchronousCall{region: declaration, asynchronous: statement, scope: nil, function: function}
		}
	}
	return &asynchronousCall{region: statement, asynchronous: statement, scope: nil, function: function}
}

// runAsynchronousCall is the region a t.Run(name, fun) call starts, and nil when call is no testing Run: the function
// literal fun is, or the one it is first assigned, either one also the scope a test must be declared in, or else the
// call itself, with fun as the scope, so `t.Run(name, func(t *testing.T) { t.Forbidden() })` written within it is
// judged.
func runAsynchronousCall(information *types.Info, call *ast.CallExpr) *asynchronousCall {
	if len(call.Args) != 2 {
		return nil
	}
	method, isFunction := typeutil.Callee(information, call).(*types.Func)
	if !isFunction || !isMethodNamed(method, "testing", "Run") {
		return nil
	}
	function := ast.Unparen(call.Args[1])
	if literal, isLiteral := function.(*ast.FuncLit); isLiteral {
		return &asynchronousCall{region: literal, asynchronous: call, scope: literal, function: function}
	}
	if identifier := usedIdentifier(information, function); identifier != nil {
		if literal := functionLiteralInScope(identifier); literal != nil {
			return &asynchronousCall{region: literal, asynchronous: call, scope: literal, function: function}
		}
	}
	return &asynchronousCall{region: call, asynchronous: call, scope: function, function: function}
}

// forbiddenMethod takes apart a call `receiver.method()` into receiver, a variable, the selection receiver.method, and
// method, the testing package's forbidden method it statically calls, and gives three nils when call has another
// shape: a method expression, a method value called later, or a receiver that is no variable.
func forbiddenMethod(information *types.Info, call *ast.CallExpr) (*types.Var, *types.Selection, *types.Func) {
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return nil, nil, nil
	}
	selection := information.Selections[selector]
	if selection == nil {
		return nil, nil, nil
	}
	var receiver *types.Var
	if identifier, isIdentifier := ast.Unparen(selector.X).(*ast.Ident); isIdentifier {
		if variable, isVariable := information.Uses[identifier].(*types.Var); isVariable {
			receiver = variable
		}
	}
	if receiver == nil {
		return nil, nil, nil
	}
	method, isFunction := selection.Obj().(*types.Func)
	if !isFunction || !isMethodNamed(method, "testing", forbidden...) {
		return nil, nil, nil
	}
	return receiver, selection, method
}

// formatMethod is method named on the type it was selected from, `(*testing.T).Fatal`, a pointer seen through any
// alias of it.
func formatMethod(selection *types.Selection, method *types.Func) string {
	if pointer, isPointer := types.Unalias(selection.Recv()).(*types.Pointer); isPointer {
		return fmt.Sprintf("(*%s).%s", pointer.Elem().String(), method.Name())
	}
	return fmt.Sprintf("(%s).%s", selection.Recv().String(), method.Name())
}
