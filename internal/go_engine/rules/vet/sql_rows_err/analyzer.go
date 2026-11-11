// Package sql_rows_err is vet's sqlrowserr pass: a *sql.Rows a query returns into a local variable, iterated by
// Rows.Next in a loop, and never asked Rows.Err, so an error that ends the iteration early reads as a shorter result.
// The pass is silent wherever the rows may escape, when the variable is used other than as the receiver of a method
// call, since the iteration's error may then be checked somewhere it cannot see.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/sqlrowserr/sqlrowserr.go, whose
// logic it keeps and whose messages it reports word for word, at the same position. The original finds the query
// calls and the uses of each variable through x/tools' internal typeindex, which the port cannot import, so it
// requires inspect alone and builds what it reads from typeindex by walking: the calls whose callee, by
// typeutil.Callee, is one of database/sql's seven query methods, judged method by method in the original's order and
// within a method in preorder, as typeindex.Index.Calls yields them, and the identifiers that use each candidate
// variable, in preorder, as typeindex.Index.Uses yields them. It reads cursors rather than a stack, since the
// judgment reads each node's parent edge and its enclosing loop. It sits on the engine's syntax dispatcher, hearing
// each call on the walk the dispatcher makes once for every syntax rule with the call's cursor, and walks the
// package's identifiers itself only where a call was a candidate. The registry runs the port in the original's place,
// and the original stays the oracle: the parity tests beside this file run both over the same packages. The two agree
// everywhere, so there is no differences fixture.
package sql_rows_err

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"iter"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "sqlrowserr"

// passDoc is the original's documentation, byte for byte.
const passDoc = `sqlrowserr: report failure to check sql.Rows.Err

This analyzer reports uses of sql.Rows in which the result of a query
such as db.Query() is assigned to a local variable that is then used
in a loop that calls Rows.Next, but lacks a final check of Rows.Err.
This causes row iteration errors to be discarded.

For example:

	rows, err := db.Query("select ...") // error: "sql.Rows rows is used in Next loop without final check of rows.Err()"
	if err != nil {
		return err
	}
	defer rows.Close() // ignore error
	for rows.Next() {
		var x int
		if err := rows.Scan(&x); err != nil {
			return err
		}
		use(x)
	}
	/* ...no use of rows.Err()... */

Correct usage of sql.Rows demands both a call to Rows.Close to release
resources and a call to Rows.Err to report iteration errors. It is
not critical to report resource cleanup errors, but it is crucial to
report iteration errors as they would otherwise be indistinguishable
from a smaller result.

To avoid false positives, the analyzer is silent if the Rows is passed
into or out of the function or assigned somewhere other than a local
variable.

It is not this analyzer's goal to ensure proper handling of errors in
all cases, but merely the simple mistakes where the user may have been
oblivious to the existence of the Rows.Err method.
`

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/sqlrowserr"

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, as the original's
// typeindex yields them, each with its cursor, of which it gathers the query calls whose rows go to a variable, and
// the package's end, where it judges them. The original requires x/tools' internal typeindex beside inspect; the
// port requires nothing beyond the dispatcher's walk, and walks for what typeindex holds.
var Listener = rule.Listener{
	Nodes:       []ast.Node{(*ast.CallExpr)(nil)},
	Node:        gatherCall,
	PackageExit: judgePackage,
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

// sqlPath is the import path of the package whose query methods the pass judges.
const sqlPath = "database/sql"

// queryMethod is one method of database/sql that returns `(*Rows, error)`, named by its receiver's type and its own
// name.
type queryMethod struct {
	typeName   string
	methodName string
}

// queryMethods are the query methods the pass judges, in the original's order, which is the order it reports in.
var queryMethods = [...]queryMethod{
	{typeName: "Conn", methodName: "QueryContext"},
	{typeName: "DB", methodName: "QueryContext"},
	{typeName: "DB", methodName: "Query"},
	{typeName: "Stmt", methodName: "QueryContext"},
	{typeName: "Stmt", methodName: "Query"},
	{typeName: "Tx", methodName: "QueryContext"},
	{typeName: "Tx", methodName: "Query"},
}

// candidate is one query call whose rows are assigned to a variable, the original's "Have: rows, err :=
// db.Query(...)".
type candidate struct {
	call *ast.CallExpr
	rows *types.Var
}

// packageState is what the port gathers over a package's walk, in the context's state.
type packageState struct {
	// candidatesByMethod is the candidates of each query method, at that method's index in queryMethods, each in
	// preorder.
	candidatesByMethod [len(queryMethods)][]candidate
	// candidateVariables is every candidate's rows variable, nil until the first candidate.
	candidateVariables map[*types.Var]bool
	// inspected is the package's inspector, read from a candidate's cursor, which judgePackage walks for the
	// identifiers that use the candidates' variables. A query can assign rows to a variable used before it, so the
	// uses are walked for once every candidate is known, as the original does.
	inspected *inspector.Inspector
}

// gatherCall keeps node when it is a call to one of the query methods whose rows go to a variable.
func gatherCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	methodIndex, isQuery := queryMethodIndex(context, call)
	if !isQuery {
		return
	}
	callCursor := context.Cursor()
	rows, isAssigned := assignedRows(context.TypesInfo, callCursor)
	if !isAssigned {
		return
	}
	state := rule.State[packageState](context)
	state.candidatesByMethod[methodIndex] = append(
		state.candidatesByMethod[methodIndex], candidate{call: call, rows: rows},
	)
	if state.candidateVariables == nil {
		state.candidateVariables = map[*types.Var]bool{}
	}
	state.candidateVariables[rows] = true
	state.inspected = callCursor.Inspector()
}

// judgePackage reports each candidate whose rows are iterated by Next and never asked Err, method by method in the
// original's order and within a method in preorder.
func judgePackage(context *rule.Context) {
	state := rule.State[packageState](context)
	if len(state.candidateVariables) == 0 {
		return
	}
	uses := variableUses(context.TypesInfo, state.inspected, state.candidateVariables)
	for _, candidates := range state.candidatesByMethod {
		for _, judged := range candidates {
			nextLoop := uncheckedNextLoop(judged.rows, uses[judged.rows])
			if !nextLoop.IsValid() {
				continue
			}
			context.Report(analysis.Diagnostic{
				Pos: judged.call.Pos(),
				End: judged.call.End(),
				Message: fmt.Sprintf("sql.Rows %q is used in Next loop at line %d without final check of %s.Err()",
					judged.rows.Name(), context.FileSet.Position(nextLoop).Line, judged.rows.Name()),
			})
		}
	}
}

// queryMethodIndex is the index in queryMethods of the method call calls, and false when it calls none of them. The
// original reads the calls of each method from typeindex.Index.Calls, which yields a call whose function, looking
// through parentheses, a selector and type arguments, is an identifier using the method, and whose typeutil.Callee is
// the method, so typeutil.Callee alone picks the same calls.
func queryMethodIndex(context *rule.Context, call *ast.CallExpr) (int, bool) {
	method, isFunction := typeutil.Callee(context.TypesInfo, call).(*types.Func)
	// typeindex indexes the packages a package refers to, never the package itself, so typeindex.Index.Selection
	// finds no method of database/sql while database/sql itself is analyzed, and the original is silent there. No
	// fixture can reach this skip, since a fixture package cannot stand in for the standard library's database/sql,
	// so the whole-tree tier over the standard library holds it.
	if !isFunction || method.Pkg() == nil || method.Pkg().Path() != sqlPath || method.Pkg() == context.Package {
		return 0, false
	}
	for index, query := range queryMethods {
		if selection(context, method.Pkg(), query.typeName, query.methodName) == method {
			return index, true
		}
	}
	return 0, false
}

// selection is the method or field named member of the type typeName declares in declaring, and nil when there is
// none. x/tools' typeindex.Index.Selection, given the package typeindex.Index.Object finds by its path. The look into
// declaring's scope is recorded in the context's reads, its answer a type whose members the answer's own rendering
// holds (#b2t9xmf).
func selection(context *rule.Context, declaring *types.Package, typeName string, member string) types.Object {
	declared, isTypeName := context.LookupIn(declaring, typeName).(*types.TypeName)
	if !isTypeName {
		return nil
	}
	selected, _, _ := types.LookupFieldOrMethod(declared.Type(), true, declared.Pkg(), member)
	return selected
}

// assignedRows is the variable callCursor's call assigns its rows to, as the first name of `var rows, err =
// db.Query(...)` or the first operand of `rows, err := db.Query(...)` or `rows, err = db.Query(...)`, and false
// when the call is anywhere else or its rows go to anything but a named variable, a field or an element, say.
func assignedRows(information *types.Info, callCursor inspector.Cursor) (*types.Var, bool) {
	var assigned ast.Expr
	switch callCursor.ParentEdgeKind() {
	case edge.ValueSpec_Values:
		if specification, isSpecification := callCursor.Parent().Node().(*ast.ValueSpec); isSpecification {
			assigned = specification.Names[0]
		}
	case edge.AssignStmt_Rhs:
		if assignment, isAssignment := callCursor.Parent().Node().(*ast.AssignStmt); isAssignment {
			assigned = assignment.Lhs[0]
		}
	}
	identifier, isIdentifier := assigned.(*ast.Ident)
	if !isIdentifier {
		return nil, false
	}
	rows, isVariable := information.ObjectOf(identifier).(*types.Var)
	return rows, isVariable
}

// variableUses is the cursor of every identifier in the package that uses one of variables, each variable's in
// preorder, the order typeindex.Index.Uses yields them in. typeindex also records a use of a generic type's field or
// method through each of its instances, which no local variable is, so the port records Uses alone.
func variableUses(
	information *types.Info, inspected *inspector.Inspector, variables map[*types.Var]bool,
) map[*types.Var][]inspector.Cursor {
	uses := map[*types.Var][]inspector.Cursor{}
	for identifierCursor := range inspected.Root().Preorder((*ast.Ident)(nil)) {
		identifier, isIdentifier := identifierCursor.Node().(*ast.Ident)
		if !isIdentifier {
			// Invariant: Preorder yields only the node types it is given.
			panic("sql_rows_err: an identifier's cursor holds no identifier")
		}
		if used, isVariable := information.Uses[identifier].(*types.Var); isVariable && variables[used] {
			uses[used] = append(uses[used], identifierCursor)
		}
	}
	return uses
}

// uncheckedNextLoop is the position of the last use of rows in a call to rows.Next inside a loop that begins after
// rows is declared, and token.NoPos when there is none, when rows.Err is called, or when rows may escape, since any use
// but as the receiver of a method call might hand the rows to code that checks them.
func uncheckedNextLoop(rows *types.Var, uses []inspector.Cursor) token.Pos {
	nextLoop := token.NoPos
	for _, use := range uses {
		if use.ParentEdgeKind() != edge.SelectorExpr_X || use.Parent().ParentEdgeKind() != edge.CallExpr_Fun {
			return token.NoPos
		}
		selector, isSelector := use.Parent().Node().(*ast.SelectorExpr)
		if !isSelector {
			// Invariant: a node reached by its SelectorExpr_X edge has a selector as its parent.
			panic("sql_rows_err: a use's parent holds no selector")
		}
		switch selector.Sel.Name {
		case "Err":
			return token.NoPos
		case "Next":
			// Only the innermost loop around the call counts, the first Enclosing yields, and only when it begins
			// after rows is declared, so a loop that declares rows afresh on each iteration is not the rows' loop.
			loop, isInLoop := first(use.Enclosing((*ast.RangeStmt)(nil), (*ast.ForStmt)(nil)))
			if isInLoop && loop.Node().Pos() > rows.Pos() {
				nextLoop = use.Node().Pos()
			}
		}
	}
	return nextLoop
}

// first is the first of cursors, and false when there is none. x/tools' moreiters.First.
func first(cursors iter.Seq[inspector.Cursor]) (inspector.Cursor, bool) {
	for cursor := range cursors {
		return cursor, true
	}
	return inspector.Cursor{}, false
}
