// Package loop_closure is vet's loopclosure pass, a function literal that references a variable its enclosing loop
// updates, run in a way that may outlive the iteration: by a goroutine, a deferred call or an errgroup.Group's Go in
// the loop's last statement, or after t.Parallel in a subtest the loop's body starts. Before Go 1.22 every iteration
// shares one variable, so such a literal may see a later iteration's value. From Go 1.22 each iteration declares its
// own, so only a file whose Go version is older than go1.22 is judged.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/loopclosure/loopclosure.go, with
// the version gate of internal/analysis/analyzerutil/version.go and the bootstrap table of internal/stdlib/deps.go it
// calls, whose logic it keeps and whose message it reports word for word, at the same position. The registry runs
// the port in the original's place, and the original stays the oracle: the parity tests beside this file run both
// over the same packages.
//
// The two differ only where the original crashes. It indexes the first argument of errgroup's Go and the first
// parameter of the literal testing's Run is given, which the real packages always supply, so a package at either path
// that leaves them out, a Go that takes any number of functions called with none or a Run whose function takes no
// parameter, panics the original. The port checks the lengths and reads nothing there, which is right: a Go given no
// function launches nothing, and a literal with no parameter has no *testing.T to call Parallel on.
// TestPortDiffersFromOriginalOnlyWhereItIsRight pins both.
package loop_closure

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "loopclosure",
	Doc: "check references to loop variables from within nested functions\n\n" +
		"This analyzer reports places where a function literal references the\n" +
		"iteration variable of an enclosing loop, and the loop calls the function\n" +
		"in such a way (e.g. with go or defer) that it may outlive the loop\n" +
		"iteration and possibly observe the wrong value of the variable.\n\n" +
		"Note: An iteration variable can only outlive a loop iteration in Go versions <=1.21.\n" +
		"In Go 1.22 and later, the loop variable lifetimes changed to create a new\n" +
		"iteration variable per loop iteration. (See go.dev/issue/60078.)\n\n" +
		"In this example, all the deferred functions run after the loop has\n" +
		"completed, so all observe the final value of v [<go1.22].\n\n" +
		"\tfor _, v := range list {\n" +
		"\t    defer func() {\n" +
		"\t        use(v) // incorrect\n" +
		"\t    }()\n" +
		"\t}\n\n" +
		"One fix is to create a new variable for each iteration of the loop:\n\n" +
		"\tfor _, v := range list {\n" +
		"\t    v := v // new var per iteration\n" +
		"\t    defer func() {\n" +
		"\t        use(v) // ok\n" +
		"\t    }()\n" +
		"\t}\n\n" +
		"After Go version 1.22, the previous two for loops are equivalent\n" +
		"and both are correct.\n\n" +
		"The next example uses a go statement and has a similar problem [<go1.22].\n" +
		"In addition, it has a data race because the loop updates v\n" +
		"concurrent with the goroutines accessing it.\n\n" +
		"\tfor _, v := range elem {\n" +
		"\t    go func() {\n" +
		"\t        use(v)  // incorrect, and a data race\n" +
		"\t    }()\n" +
		"\t}\n\n" +
		"A fix is the same as before. The checker also reports problems\n" +
		"in goroutines started by golang.org/x/sync/errgroup.Group.\n" +
		"A hard-to-spot variant of this form is common in parallel tests:\n\n" +
		"\tfunc Test(t *testing.T) {\n" +
		"\t    for _, test := range tests {\n" +
		"\t        t.Run(test.name, func(t *testing.T) {\n" +
		"\t            t.Parallel()\n" +
		"\t            use(test) // incorrect, and a data race\n" +
		"\t        })\n" +
		"\t    }\n" +
		"\t}\n\n" +
		"The t.Parallel() call causes the rest of the function to execute\n" +
		"concurrent with the loop [<go1.22].\n\n" +
		"The analyzer reports references only in the last statement,\n" +
		"as it is not deep enough to understand the effects of subsequent\n" +
		"statements that might render the reference benign.\n" +
		"(\"Last statement\" is defined recursively in compound\n" +
		"statements such as if, switch, and select.)\n\n" +
		"See: https://golang.org/doc/go_faq.html#closures_and_goroutines",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/loopclosure",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("loop_closure: inspect's result is missing")
	}
	// The root's children are the package's files, in the order the original's walk enters them. A file at go1.22 or
	// later is passed over whole, as the original prunes its walk there.
	for fileCursor := range inspected.Root().Children() {
		file, isFile := fileCursor.Node().(*ast.File)
		if !isFile {
			// Invariant: an inspector's root holds the files it was built from and nothing else.
			panic("loop_closure: the inspector's root holds something other than a file")
		}
		if fileUsesGoVersion(pass, file, perIterationVersion) {
			continue
		}
		// Preorder visits each loop, nested loops after the loops around them, in the original's order.
		for loopCursor := range fileCursor.Preorder((*ast.RangeStmt)(nil), (*ast.ForStmt)(nil)) {
			checkLoop(pass, loopCursor.Node())
		}
	}
	return nil, nil
}

// checkLoop reports each reference to a variable loop updates from a function literal that may run outside the
// iteration.
func checkLoop(pass *analysis.Pass, loop ast.Node) {
	// The variables the loop statement updates: a range's key and value, and what a three-clause loop's post statement
	// assigns or increments, so `for p = head; p != nil; p = p.next` and `for i := 0; i < n; i++` are both loops over
	// a variable.
	var variables []types.Object
	var body *ast.BlockStmt
	switch statement := loop.(type) {
	case *ast.RangeStmt:
		body = statement.Body
		variables = appendLoopVariable(pass.TypesInfo, variables, statement.Key)
		variables = appendLoopVariable(pass.TypesInfo, variables, statement.Value)
	case *ast.ForStmt:
		body = statement.Body
		switch post := statement.Post.(type) {
		case *ast.AssignStmt:
			for _, assigned := range post.Lhs {
				variables = appendLoopVariable(pass.TypesInfo, variables, assigned)
			}
		case *ast.IncDecStmt:
			variables = appendLoopVariable(pass.TypesInfo, variables, post.X)
		}
	}
	if len(variables) == 0 {
		return
	}
	// A go, defer or errgroup.Group.Go statement is judged only when it is the body's last statement, defined
	// recursively, since it is hard to prove a go is not followed by a wait, or a defer by a return.
	forEachLastStatement(body.List, func(last ast.Stmt) {
		var statements []ast.Stmt
		switch launched := last.(type) {
		case *ast.GoStmt:
			statements = functionLiteralStatements(launched.Call.Fun)
		case *ast.DeferStmt:
			statements = functionLiteralStatements(launched.Call.Fun)
		case *ast.ExprStmt:
			if call, isCall := launched.X.(*ast.CallExpr); isCall {
				statements = functionLiteralStatements(asynchronousFunction(pass.TypesInfo, call))
			}
		}
		for _, statement := range statements {
			reportCaptured(pass, variables, statement)
		}
	})
	// Every t.Run in the body is judged, not only the last, since there is no common way to wait for parallel
	// subtests: the test runner does it.
	for _, statement := range body.List {
		expressionStatement, isExpressionStatement := statement.(*ast.ExprStmt)
		if !isExpressionStatement {
			continue
		}
		call, isCall := expressionStatement.X.(*ast.CallExpr)
		if !isCall {
			continue
		}
		for _, concurrent := range parallelSubtest(pass.TypesInfo, call) {
			reportCaptured(pass, variables, concurrent)
		}
	}
}

// appendLoopVariable is variables with the object expression names appended, when expression is an identifier that
// names one, the original's addVar. An index, `for values[0] = range`, or a parenthesized name updates no variable
// the original tracks.
func appendLoopVariable(information *types.Info, variables []types.Object, expression ast.Expr) []types.Object {
	identifier, isIdentifier := expression.(*ast.Ident)
	if !isIdentifier {
		return variables
	}
	object := information.ObjectOf(identifier)
	if object == nil {
		return variables
	}
	return append(variables, object)
}

// reportCaptured reports every identifier in statement, a statement of a function literal in the loop, that uses one
// of variables. Each match is reported, so a variable a loop updates twice, `for value, value = range`, is reported
// twice, as the original reports it.
func reportCaptured(pass *analysis.Pass, variables []types.Object, statement ast.Stmt) {
	for node := range ast.Preorder(statement) {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier {
			continue
		}
		// An identifier Uses holds nothing for matches none of variables, which appendLoopVariable keeps free of nil,
		// so the original's skip of a nil use needs no line of its own.
		used := pass.TypesInfo.Uses[identifier]
		for _, variable := range variables {
			if variable == used {
				pass.ReportRangef(identifier, "loop variable %s captured by func literal", identifier.Name)
			}
		}
	}
}
