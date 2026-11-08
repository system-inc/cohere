// Package loop_closure is vet's loopclosure pass, a function literal that references a variable its enclosing loop
// updates, run in a way that may outlive the iteration: by a goroutine, a deferred call or an errgroup.Group's Go in
// the loop's last statement, or after t.Parallel in a subtest the loop's body starts. Before Go 1.22 every iteration
// shares one variable, so such a literal may see a later iteration's value. From Go 1.22 each iteration declares its
// own, so only a file whose Go version is older than go1.22 is judged. It sits on the engine's syntax dispatcher,
// hearing each loop on the walk the dispatcher makes once for every syntax rule.
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

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "loopclosure"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check references to loop variables from within nested functions\n\n" +
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
	"See: https://golang.org/doc/go_faq.html#closures_and_goroutines"

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/loopclosure"

// Listener is what the port hears of each package from the dispatcher: each file as the walk enters it, in the order
// the original's walk enters them, and every range and three-clause loop inside it, in preorder, nested loops after
// the loops around them, of which it judges those in a file older than go1.22.
var Listener = rule.Listener{
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.RangeStmt)(nil), (*ast.ForStmt)(nil)},
	Node:      checkNode,
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

// walkState is what the port keeps over a package's walk, in the context's state.
type walkState struct {
	// skipped is whether the file being walked is at go1.22 or later, so its loops are passed over, as the original
	// prunes its walk there. The walk enters each file before anything inside it.
	skipped bool
}

// enterFile decides whether the loops of file, which the walk is entering, are judged.
func enterFile(context *rule.Context, file *ast.File) {
	rule.State[walkState](context).skipped = fileUsesGoVersion(context, file, perIterationVersion)
}

// checkNode judges loop, a range or three-clause loop, unless its file is passed over.
func checkNode(context *rule.Context, loop ast.Node, _ []ast.Node) {
	if rule.State[walkState](context).skipped {
		return
	}
	checkLoop(context, loop)
}

// checkLoop reports each reference to a variable loop updates from a function literal that may run outside the
// iteration.
func checkLoop(context *rule.Context, loop ast.Node) {
	// The variables the loop statement updates: a range's key and value, and what a three-clause loop's post statement
	// assigns or increments, so `for p = head; p != nil; p = p.next` and `for i := 0; i < n; i++` are both loops over
	// a variable.
	var variables []types.Object
	var body *ast.BlockStmt
	switch statement := loop.(type) {
	case *ast.RangeStmt:
		body = statement.Body
		variables = appendLoopVariable(context.TypesInfo, variables, statement.Key)
		variables = appendLoopVariable(context.TypesInfo, variables, statement.Value)
	case *ast.ForStmt:
		body = statement.Body
		switch post := statement.Post.(type) {
		case *ast.AssignStmt:
			for _, assigned := range post.Lhs {
				variables = appendLoopVariable(context.TypesInfo, variables, assigned)
			}
		case *ast.IncDecStmt:
			variables = appendLoopVariable(context.TypesInfo, variables, post.X)
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
				statements = functionLiteralStatements(asynchronousFunction(context.TypesInfo, call))
			}
		}
		for _, statement := range statements {
			reportCaptured(context, variables, statement)
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
		for _, concurrent := range parallelSubtest(context.TypesInfo, call) {
			reportCaptured(context, variables, concurrent)
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
func reportCaptured(context *rule.Context, variables []types.Object, statement ast.Stmt) {
	for node := range ast.Preorder(statement) {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier {
			continue
		}
		// An identifier Uses holds nothing for matches none of variables, which appendLoopVariable keeps free of nil,
		// so the original's skip of a nil use needs no line of its own.
		used := context.TypesInfo.Uses[identifier]
		for _, variable := range variables {
			if variable == used {
				context.ReportRangef(identifier, "loop variable %s captured by func literal", identifier.Name)
			}
		}
	}
}
