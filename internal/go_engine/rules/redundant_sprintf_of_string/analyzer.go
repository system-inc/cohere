// Package redundant_sprintf_of_string is staticcheck's S1025, a value formatted with `fmt.Sprintf("%s", x)` where the
// value is a string already, converts to one, or has a String method, ported as a check on types and syntax that
// reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1025/s1025.go, whose logic it keeps and whose
// four messages and fixes it reports word for word, at the same range, the call. The original matches its one query,
// `(CallExpr (Symbol "fmt.Sprintf") [format arg])`, through staticcheck's pattern language, walking the calls the type
// index holds of fmt.Sprintf; the port asks the same of each call directly, reading pattern/match.go for what each
// construct of the query matches, builds each fix's syntax itself as the original builds it, and reports through
// staticcheck_report.Report where the original reports through report.Report. The original reads one thing from
// staticcheck's intermediate representation, the program's method set cache, whose answer is types.NewMethodSet's,
// so the port asks types.NewMethodSet and requires no intermediate representation at all.
//
// The registry still runs the original, which stays the oracle: the parity tests beside this file run both over the
// same packages, and the two differ only where the original is wrong, each case recorded in the differences fixture.
// The port drops the finding on a value whose type is an alias of a type parameter, which the original skips when the
// type parameter is named as itself. It moves a fix that would regroup the expression, parenthesizing the argument
// where the call is indexed or sliced, or where a composite literal of a named type would land bare in a control
// clause, and withholds a fix that leaves a bare value as a statement, or writes `string(x)` where `string` is not the
// predeclared type.
package redundant_sprintf_of_string

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/go/types/typeutil"
	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, since three of its four findings filter them.
var Analyzer = &analysis.Analyzer{
	Name:     "S1025",
	Doc:      `Flags fmt.Sprintf("%s", x) where x is or has a string already: staticcheck's S1025, ported.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every call of the package, each where it stands in its file.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("redundant_sprintf_of_string: the inspect pass's result is missing")
	}
	for cursor := range inspected.Root().Preorder((*ast.CallExpr)(nil)) {
		Check(context, cursor)
	}
	return nil, nil
}

// Check judges the call at cursor: fmt.Sprintf of the format "%s" and one argument, whose type is no type parameter,
// no reflect.Value and no fmt.Formatter, is the argument's String method where it has one, the argument itself where
// it is a string, and a conversion to string where it converts to one. Where the call stands decides the fix's
// shape, which is why Check takes a cursor and not the call alone: an argument parenthesized where the call's context
// would regroup it, or no fix where the fix leaves a bare value as a statement or names no predeclared string.
func Check(context *rule.Context, cursor inspector.Cursor) {
	call, isCall := cursor.Node().(*ast.CallExpr)
	if !isCall {
		return
	}
	// The original walks the type index's calls of fmt.Sprintf, and the index keeps no symbol of the package it
	// indexes, so the fmt package's own calls are never walked.
	if context.Package.Path() == "fmt" {
		return
	}
	// `[format arg]` is a list of two, and the pattern's CallExpr has only Fun and Args, so a spread call,
	// `fmt.Sprintf("%s", values...)`, is the query's shape too. `(Symbol "fmt.Sprintf")` names the function through a
	// renamed and a dot import, parenthesized or not, and never a variable holding it.
	if len(call.Args) != 2 || !staticcheck_code.IsSymbol(context, call.Fun, "fmt.Sprintf") {
		return
	}
	// Each binding holds its argument without the parentheses the pattern looks through, and the fixes build on that.
	argument := ast.Unparen(call.Args[1])
	// A named constant whose value is "%s" is the format too, as code.ExprToString reads any constant.
	if format, isString := staticcheck_code.ExprToString(context, ast.Unparen(call.Args[0])); !isString ||
		format != "%s" {
		return
	}
	argumentType := context.TypesInfo.TypeOf(argument)
	if argumentType == nil {
		// The type checker records a type for every argument of a call it checks, so no package that type-checks
		// holds one without; the original would crash on it.
		return
	}
	// A type parameter's instantiation may be a formatter, and the original skips it as named, never through an alias,
	// `type A = T`, which is the same type, so the port looks through the alias and drops that finding.
	if _, isTypeParameter := types.Unalias(argumentType).(*types.TypeParam); isTypeParameter {
		return
	}
	// A reflect.Value prints the value it holds under %s, where its String method prints its type.
	if typeutil.IsTypeWithName(argumentType, "reflect.Value") {
		return
	}
	// A formatter handles %s however it chooses.
	if isFormatter(argumentType) {
		return
	}

	// The fixes replace the call, as written, not the parentheses around it.
	switch {
	case types.Implements(argumentType, knowledge.Interfaces["fmt.Stringer"]):
		// The method is called on the argument as bound, which the printer parenthesizes where an operator would
		// otherwise bind tighter, `(*value).String()`, and the port where a composite literal would land bare in a
		// control clause, `(T{}).String()`.
		receiver := argument
		if isInControlClause(cursor) && hasBareCompositeLiteral(argument) {
			receiver = &ast.ParenExpr{X: argument}
		}
		stringCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: receiver, Sel: &ast.Ident{Name: "String"}}}
		// The original filters no generated file for this finding, where it filters them for the other three.
		staticcheck_report.Report(context, call, "should use String() instead of fmt.Sprintf",
			staticcheck_report.Fixes(edit.Fix("Replace with call to String method",
				edit.ReplaceWithNode(context.Fset, call, stringCall))))
	case types.Unalias(argumentType) == types.Universe.Lookup("string").Type():
		fixes := []analysis.SuggestedFix{}
		// The argument alone, as a statement, is a value evaluated and not used, and after go or defer no call at
		// all, so no rewrite says what the call did.
		if !isDiscarded(cursor) {
			replacement := argument
			if (isIndexed(cursor) && isOperation(argument)) ||
				(isInControlClause(cursor) && hasBareCompositeLiteral(argument)) {
				replacement = &ast.ParenExpr{X: argument}
			}
			fixes = append(fixes, edit.Fix("Remove unnecessary call to fmt.Sprintf",
				edit.ReplaceWithNode(context.Fset, call, replacement)))
		}
		staticcheck_report.Report(context, call,
			"the argument is already a string, there's no need to use fmt.Sprintf",
			staticcheck_report.FilterGenerated(),
			staticcheck_report.Fixes(fixes...))
	case argumentType.Underlying() == types.Universe.Lookup("string").Type():
		staticcheck_report.Report(context, call,
			"the argument's underlying type is a string, should use a simple conversion instead of fmt.Sprintf",
			staticcheck_report.FilterGenerated(),
			staticcheck_report.Fixes(conversionFixes(context, cursor, call, argument)...))
	case staticcheck_code.IsOfStringConvertibleByteSlice(context, argument):
		staticcheck_report.Report(context, call,
			"the argument's underlying type is a slice of bytes, should use a simple conversion instead of fmt.Sprintf",
			staticcheck_report.FilterGenerated(),
			staticcheck_report.Fixes(conversionFixes(context, cursor, call, argument)...))
	}
}

// conversionFixes is the fix that converts argument to a string in call's place, `string(argument)`, and none where
// that conversion is a value evaluated and not used, or where string names something other than the predeclared type
// at the call, a declaration of the code's own in a block or its package, so no conversion spelled any way is one.
func conversionFixes(
	context *rule.Context, cursor inspector.Cursor, call *ast.CallExpr, argument ast.Expr,
) []analysis.SuggestedFix {
	if isDiscarded(cursor) {
		return nil
	}
	scope := context.Package.Scope().Innermost(call.Pos())
	if scope == nil {
		// Invariant: a call stands in a file, whose scope the type checker records around it.
		panic("redundant_sprintf_of_string: no scope holds a call")
	}
	// The call's position counts only what is declared before it in its own block, as the conversion's place does.
	if _, named := scope.LookupParent("string", call.Pos()); named != types.Universe.Lookup("string") {
		return nil
	}
	conversion := &ast.CallExpr{Fun: &ast.Ident{Name: "string"}, Args: []ast.Expr{argument}}
	return []analysis.SuggestedFix{
		edit.Fix("Replace with conversion to string", edit.ReplaceWithNode(context.Fset, call, conversion)),
	}
}

// isFormatter is whether a value of typ has a method Format of two parameters and no results, which fmt calls for
// every verb. It is the original's isFormatter, which reads typ's method set from the intermediate representation's
// cache, typeutil.MethodSetCache, whose answer for any type is types.NewMethodSet's, an alias looked through either
// way. The parameters' types go unchecked, as the original leaves them.
func isFormatter(typ types.Type) bool {
	selection := types.NewMethodSet(typ).Lookup(nil, "Format")
	if selection == nil {
		return false
	}
	function, isFunction := selection.Obj().(*types.Func)
	if !isFunction {
		// A method set holds methods alone, so this is unreachable, as the original says of its own.
		return false
	}
	signature, isSignature := function.Type().(*types.Signature)
	if !isSignature {
		// Invariant: go/types gives every function a signature as its type.
		panic("redundant_sprintf_of_string: a method's type is not a signature")
	}
	return signature.Params().Len() == 2 && signature.Results().Len() == 0
}

// isDiscarded is whether the call at cursor is a statement of its own, parenthesized or not, or the call a go or defer
// statement makes, where nothing reads the value it makes.
func isDiscarded(cursor inspector.Cursor) bool {
	// The outermost parentheses around the call, or the call itself where it has none.
	outermost := cursor
	for outermost.ParentEdgeKind() == edge.ParenExpr_X {
		outermost = outermost.Parent()
	}
	switch outermost.ParentEdgeKind() {
	case edge.ExprStmt_X, edge.GoStmt_Call, edge.DeferStmt_Call:
		return true
	}
	return false
}

// isIndexed is whether the call at cursor is, unparenthesized, what an index or slice expression indexes, where an
// operator in what replaces it would bind looser than the index: `fmt.Sprintf("%s", a+b)[0]` is `(a + b)[0]`, and
// `a + b[0]` indexes b alone.
func isIndexed(cursor inspector.Cursor) bool {
	switch cursor.ParentEdgeKind() {
	case edge.IndexExpr_X, edge.SliceExpr_X:
		return true
	}
	return false
}

// isOperation is whether expression is a binary or unary operation or an indirection, the expressions an index
// binds tighter than, `a + b`, `<-channel` and `*pointer` among the ones of type string.
func isOperation(expression ast.Expr) bool {
	switch expression.(type) {
	case *ast.BinaryExpr, *ast.UnaryExpr, *ast.StarExpr:
		return true
	}
	return false
}

// isInControlClause is whether the call at cursor stands in the clause of an if, for, switch or range statement
// outside every parenthesis, bracket and brace, where the parser reads a brace after a type's name as the start of
// the statement's block: through operators, selectors, indexed and sliced values, assertions and callees, and the
// simple statements a clause holds, to the clause.
func isInControlClause(cursor inspector.Cursor) bool {
	// The node the walk has climbed to, the call first and then each node that holds it at the clause's level.
	reached := cursor
	for {
		switch reached.ParentEdgeKind() {
		case edge.BinaryExpr_X, edge.BinaryExpr_Y, edge.UnaryExpr_X, edge.StarExpr_X, edge.SelectorExpr_X,
			edge.IndexExpr_X, edge.IndexListExpr_X, edge.SliceExpr_X, edge.TypeAssertExpr_X, edge.CallExpr_Fun,
			edge.AssignStmt_Lhs, edge.AssignStmt_Rhs, edge.ExprStmt_X, edge.SendStmt_Chan, edge.SendStmt_Value,
			edge.IncDecStmt_X:
			reached = reached.Parent()
		case edge.IfStmt_Init, edge.IfStmt_Cond, edge.ForStmt_Init, edge.ForStmt_Cond, edge.ForStmt_Post,
			edge.SwitchStmt_Init, edge.SwitchStmt_Tag, edge.TypeSwitchStmt_Init, edge.TypeSwitchStmt_Assign,
			edge.RangeStmt_Key, edge.RangeStmt_Value, edge.RangeStmt_X:
			return true
		default:
			return false
		}
	}
}

// hasBareCompositeLiteral is whether expression holds, outside every parenthesis, bracket and brace, a composite
// literal whose type is a name, `T{}`, `pkg.T{}` or `G[int]{}`, which go/parser takes for a type followed by a block
// in a control clause. A literal of a type written out, `[]byte{}` or `struct{}{}`, is read as one anywhere.
func hasBareCompositeLiteral(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.CompositeLit:
		switch ast.Unparen(expression.Type).(type) {
		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
			return true
		}
		return false
	case *ast.BinaryExpr:
		return hasBareCompositeLiteral(expression.X) || hasBareCompositeLiteral(expression.Y)
	case *ast.UnaryExpr:
		return hasBareCompositeLiteral(expression.X)
	case *ast.StarExpr:
		return hasBareCompositeLiteral(expression.X)
	case *ast.SelectorExpr:
		return hasBareCompositeLiteral(expression.X)
	case *ast.IndexExpr:
		return hasBareCompositeLiteral(expression.X)
	case *ast.IndexListExpr:
		return hasBareCompositeLiteral(expression.X)
	case *ast.SliceExpr:
		return hasBareCompositeLiteral(expression.X)
	case *ast.TypeAssertExpr:
		return hasBareCompositeLiteral(expression.X)
	case *ast.CallExpr:
		return hasBareCompositeLiteral(expression.Fun)
	}
	return false
}
