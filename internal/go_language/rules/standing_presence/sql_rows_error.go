package standing_presence

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_language/build_reads"
	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// sqlQueryMethod is one method of database/sql that returns rows, by its receiver's type name and its own.
type sqlQueryMethod struct {
	typeName   string
	methodName string
}

// sqlQueryMethods are the methods of database/sql the rule judges (rules/vet/sql_rows_err/analyzer.go:115-123).
var sqlQueryMethods = [...]sqlQueryMethod{
	{typeName: "Conn", methodName: "QueryContext"},
	{typeName: "DB", methodName: "QueryContext"},
	{typeName: "DB", methodName: "Query"},
	{typeName: "Stmt", methodName: "QueryContext"},
	{typeName: "Stmt", methodName: "Query"},
	{typeName: "Tx", methodName: "QueryContext"},
	{typeName: "Tx", methodName: "Query"},
}

// sqlRowsErrorNamed names, with sqlrowserr's reason, each object the package's bodies use that the build finds as a
// package-level variable of the pass's own package that a query's rows are assigned to: the rule judges such a
// variable by every use of it in the package, so each body that names it must be present.
//
// The predicate mirrors rules/vet/sql_rows_err/analyzer.go, which exposes none of it: a call that is itself a value of
// a `var` specification or the right of an assignment, whose rows go to the specification's first name or the
// assignment's first left side (assignedRows, analyzer.go:232-250), to one of database/sql's seven query methods from
// outside database/sql (queryMethodIndex, analyzer.go:199-214). The rule finds each method by a recorded look into
// database/sql's scope (analyzer.go:220-227); this reads the method's own receiver instead, which is the same method,
// since each of the seven is declared on its type and none is promoted, and needs no look outside the package. As the
// census counter reads it, a variable only ever assigned with `=` is a candidate too.
//
// As scannererr's, the walk names every candidate the rule can report, made only in a package-level `var`
// specification the build always holds, since an `=` left side is a use that ends the judgment
// (analyzer.go:252-280); every package-level variable whose type is *sql.Rows is named besides, as cheap safety.
func sqlRowsErrorNamed(
	pass *analysis.Pass,
	used []fact_reach.UsedObject,
	find func(identity build_reads.Identity) (types.Object, bool),
) []fact_reach.Named {
	// candidates are read on the first package-level variable found, since most packages' bodies use none.
	var candidates map[*types.Var]bool
	var named []fact_reach.Named
	for _, object := range used {
		variable, isVariable := packageVariable(pass, find, object.Identity)
		if !isVariable {
			continue
		}
		if candidates == nil {
			candidates = rowsVariables(pass)
		}
		if isPointerToNamed(variable.Type(), "database/sql", "Rows") || candidates[variable] {
			named = append(named, fact_reach.Named{Object: object.Object, Reason: SqlRowsError.Reason()})
		}
	}
	return named
}

// rowsVariables are the variables of the pass's files a query's rows are assigned to, at any depth: a `var`
// specification's first name, or an assignment's first left side, where any value is a query call.
func rowsVariables(pass *analysis.Pass) map[*types.Var]bool {
	variables := map[*types.Var]bool{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			var first ast.Expr
			var values []ast.Expr
			switch heard := node.(type) {
			case *ast.ValueSpec:
				first, values = heard.Names[0], heard.Values
			case *ast.AssignStmt:
				first, values = heard.Lhs[0], heard.Rhs
			default:
				return true
			}
			identifier, isIdentifier := first.(*ast.Ident)
			if !isIdentifier {
				return true
			}
			for _, value := range values {
				if !queriesRows(pass, value) {
					continue
				}
				if variable, isVariable := pass.TypesInfo.ObjectOf(identifier).(*types.Var); isVariable {
					variables[variable] = true
				}
			}
			return true
		})
	}
	return variables
}

// queriesRows is whether value is itself a call of one of database/sql's query methods, never in parentheses.
func queriesRows(pass *analysis.Pass, value ast.Expr) bool {
	call, isCall := value.(*ast.CallExpr)
	if !isCall {
		return false
	}
	method, isFunction := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !isFunction || method.Pkg() == nil || method.Pkg().Path() != "database/sql" || method.Pkg() == pass.Pkg ||
		method.Signature().Recv() == nil {
		return false
	}
	receiver := types.Unalias(method.Signature().Recv().Type())
	if pointer, isPointer := receiver.(*types.Pointer); isPointer {
		receiver = types.Unalias(pointer.Elem())
	}
	named, isNamed := receiver.(*types.Named)
	if !isNamed {
		return false
	}
	for _, query := range sqlQueryMethods {
		if named.Obj().Name() == query.typeName && method.Name() == query.methodName {
			return true
		}
	}
	return false
}
