// Package single_append_concatenation is staticcheck's S1011, a range loop whose body only appends each element of
// the slice it ranges over to another slice, which one append of the whole slice, `x = append(x, y...)`, says
// plainer, ported as a check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1011/s1011.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the loop's header. The original matches one query of
// three alternatives through staticcheck's pattern language, a loop over values,
// `(RangeStmt (Ident "_") val@(Object _) _ x [(AssignStmt [lhs] "=" [(CallExpr (Builtin "append") [lhs val])])])`,
// a loop over indexes appending `x[idx]`, and a loop over indexes that first defines `val := x[idx]` and appends val;
// the port asks the same of each range statement directly, reading pattern/match.go for what each construct of the
// query matches, then filters the matches as the original does, by the purity facts, function_purity's port of
// staticcheck's, and the two slices' types, and reports through staticcheck_report.Report where the original reports
// through report.Report.
//
// It parts from the original in two places, where the original is wrong. An append that spreads its element,
// `x = append(x, v...)`, appends what each element holds, not the element, and the pattern language ignores a call's
// spread, so the original suggests appending the elements themselves, a different program: the port drops it. And a
// loop that assigns its key or value with = to a variable declared outside the for statement leaves the last one
// there, which the one append does not: the port keeps the finding and withholds the fix. The differences fixture
// records each. The registry runs the port in the original's place, under its name, and the original stays only as the
// oracle: the parity tests beside this file run both over the same packages, and the two differ nowhere else they hold.
package single_append_concatenation

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher over the engine's own intermediate
// representation: every function, whose syntax it walks when the function is where syntax starts, so each range
// statement is judged once. It listens to that walk, not the syntax's, because it reads function_purity's facts,
// which say which calls in the loop's slices may have side effects and are built from our representation, so only on
// a package without errors, as the original runs: the syntax walk runs on every package, and so must everything a
// syntax listener requires.
var Listener = rule.Listener{
	FileLocal:                  true,
	Requires:                   []*analysis.Analyzer{function_purity.Analyzer},
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: judgeFunction},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1011",
	"S1011",
	"Flags a range loop that appends each element of one slice to another, which one append says: "+
		"staticcheck's S1011, ported.",
	&Listener,
)

// judgeFunction judges every range statement in the syntax of function, nested function literals included, when
// function is where that syntax starts: a declared function, the blank-named among them, which the representation
// builds too, or a function literal of the package's initializer, whose parent, the initializer itself, has no syntax.
// Every other function is a literal inside one of those and is judged in its walk. A range statement is written only
// inside a function's body, so these walks reach every one the original's walk over the files reaches, each once.
func judgeFunction(context *rule.Context, function *intermediate_representation.Function) {
	syntax := function.Syntax()
	if syntax == nil || function.Parent() != nil && function.Parent().Syntax() != nil {
		return
	}
	pure, isPurity := context.ResultOf(function_purity.Analyzer).(*function_purity.Result)
	if !isPurity || pure == nil {
		// Invariant: the listener requires function_purity, whose result is always a function_purity.Result, so
		// anything else is a mistake in building the rule, which the dispatcher's recover makes this rule's crash on
		// the package.
		panic("single_append_concatenation: the purity pass's result is missing")
	}
	// The purity pass's result is never nil, so the original's MayHaveSideEffects always asks it, and a function is
	// pure where it answers so.
	asking := pure.Asking(context.RuleName())
	ast.Inspect(syntax, func(node ast.Node) bool {
		if statement, isRange := node.(*ast.RangeStmt); isRange {
			Check(context, asking.IsPure, statement)
		}
		return true
	})
}

// loopAppend is what one range statement bound, as the original's query binds it: destination is lhs, the slice
// appended to, and source is x, the slice ranged over, each with its parentheses removed as the pattern language
// stores a binding; value is val, the object each element is held in, and index is idx, the loop's index variable,
// either nil where the alternative that matched binds none. call is the append.
type loopAppend struct {
	destination ast.Expr
	source      ast.Expr
	value       types.Object
	index       types.Object
	call        *ast.CallExpr
}

// Check judges one range statement, with isPure judging which functions the purity facts hold pure: a loop that
// matches one of the query's three alternatives, whose slice appended to refers to neither loop variable and may have
// no side effects, whose slice ranged over may have none either where an index loop reads it on every iteration, and
// whose two slices are of identical types, is reported at its header, with a fix that replaces the loop with the one
// append.
func Check(context *rule.Context, isPure func(function *types.Func) bool, statement *ast.RangeStmt) {
	matched, isMatched := matchLoop(context, statement)
	if !isMatched {
		return
	}
	// The port's one departure: the pattern's CallExpr has no field for a spread, so the original matches
	// `append(lhs, val...)`, which appends what each element holds, and its fix would append the elements instead.
	if matched.call.Ellipsis.IsValid() {
		return
	}
	if matched.value != nil && staticcheck_code.RefersTo(context, matched.destination, matched.value) {
		return
	}
	// An index loop evaluates the slice it ranges over again on every iteration, in `x[idx]`, so that slice must be
	// pure; a loop over values evaluates it once, as the one append would.
	if matched.index != nil && staticcheck_code.MayHaveSideEffects(context, matched.source, isPure) {
		return
	}
	// The slice appended to mustn't refer to the index loop variable.
	if matched.index != nil && staticcheck_code.RefersTo(context, matched.destination, matched.index) {
		return
	}
	// The slice appended to may be dynamic, a different slice on each iteration, which might also change the index's
	// value, so it must be pure.
	if staticcheck_code.MayHaveSideEffects(context, matched.destination, isPure) {
		return
	}
	if !types.Identical(context.TypesInfo.TypeOf(matched.source), context.TypesInfo.TypeOf(matched.destination)) {
		return
	}
	// The replacement is the original's own tree: the bound nodes as they are, a synthetic append, and a spread whose
	// position is 1, which the printer reads only as present, so the message and the fix print byte for byte alike.
	replacement := &ast.AssignStmt{
		Lhs: []ast.Expr{matched.destination},
		Tok: token.ASSIGN,
		Rhs: []ast.Expr{
			&ast.CallExpr{
				Fun:      &ast.Ident{Name: "append"},
				Args:     []ast.Expr{matched.destination, matched.source},
				Ellipsis: 1,
			},
		},
	}
	// A key or value the header assigns with = is a variable declared outside the for statement, which the loop
	// leaves holding the last element or index, and the one append leaves as it was, so the original's fix changes
	// what the code after the loop reads. Whether anything reads it is a question of flow the port does not ask: the
	// declaration's place is exact, so the port withholds the fix for every loop variable the statement does not
	// declare, and keeps the finding, since the loop is still one append. The value the third alternative defines in
	// the body is declared inside, and a blank key binds no object.
	fixes := []analysis.SuggestedFix{}
	if isDeclaredIn(matched.index, statement) && isDeclaredIn(matched.value, statement) {
		fixes = append(fixes, edit.Fix("Replace loop with call to append",
			edit.ReplaceWithNode(context.FileSet, statement, replacement)))
	}
	staticcheck_report.Report(context, statement,
		"should replace loop with "+staticcheck_report.Render(context, replacement),
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(fixes...))
}

// isDeclaredIn is whether object, a loop variable the match bound, is declared inside statement, as a key or value
// its header declares with := is, so it ends with the loop. A nil object, a variable the alternative that matched
// binds none of, is no variable that outlives the loop, and counts as declared inside.
func isDeclaredIn(object types.Object, statement *ast.RangeStmt) bool {
	return object == nil || object.Pos() >= statement.Pos() && object.Pos() < statement.End()
}

// matchLoop is the query's Or: the first of its three alternatives statement matches, with what that alternative
// bound, as the pattern language tries each in turn and keeps the bindings of the first that matches.
func matchLoop(context *rule.Context, statement *ast.RangeStmt) (loopAppend, bool) {
	if matched, isMatched := matchValueLoop(context, statement); isMatched {
		return matched, true
	}
	if matched, isMatched := matchIndexLoop(context, statement); isMatched {
		return matched, true
	}
	return matchDefiningIndexLoop(context, statement)
}

// matchValueLoop is the first alternative, `(RangeStmt (Ident "_") val@(Object _) _ x [append of val])`: a blank key,
// a value that is an identifier, defined or assigned, as `_` matches either token, and a body of one statement
// appending the value.
func matchValueLoop(context *rule.Context, statement *ast.RangeStmt) (loopAppend, bool) {
	// `(Ident "_")` is an identifier spelled `_`, parenthesized or not; a loop with no key never is one.
	key, isKey := ast.Unparen(statement.Key).(*ast.Ident)
	if !isKey || key.Name != "_" {
		return loopAppend{}, false
	}
	value, isValue := ast.Unparen(statement.Value).(*ast.Ident)
	if !isValue {
		return loopAppend{}, false
	}
	// `(Object _)` binds the identifier's object, which may be nil, and a nil object is one its recall never equals.
	valueObject := context.TypesInfo.ObjectOf(value)
	if len(statement.Body.List) != 1 {
		return loopAppend{}, false
	}
	destination, call, isAppend := appendAssignment(context, statement.Body.List[0])
	if !isAppend || !namesObject(context, call.Args[1], valueObject) {
		return loopAppend{}, false
	}
	return loopAppend{
		destination: destination, source: ast.Unparen(statement.X), value: valueObject, call: call,
	}, true
}

// matchIndexLoop is the second alternative, `(RangeStmt idx@(Object _) nil _ x [append of (IndexExpr x idx)])`: a key
// that is an identifier, no value, and a body of one statement appending the element at the key.
func matchIndexLoop(context *rule.Context, statement *ast.RangeStmt) (loopAppend, bool) {
	source, indexObject, isIndexLoop := indexLoop(context, statement)
	if !isIndexLoop || len(statement.Body.List) != 1 {
		return loopAppend{}, false
	}
	destination, call, isAppend := appendAssignment(context, statement.Body.List[0])
	if !isAppend || !isElement(context, call.Args[1], source, indexObject) {
		return loopAppend{}, false
	}
	return loopAppend{destination: destination, source: source, index: indexObject, call: call}, true
}

// matchDefiningIndexLoop is the third alternative, `(RangeStmt idx@(Object _) nil _ x [(AssignStmt val@(Object _)
// ":=" (IndexExpr x idx)) append of val])`: a loop over indexes whose body is two statements, one defining a
// variable as the element at the key, and one appending that variable.
func matchDefiningIndexLoop(context *rule.Context, statement *ast.RangeStmt) (loopAppend, bool) {
	source, indexObject, isIndexLoop := indexLoop(context, statement)
	if !isIndexLoop || len(statement.Body.List) != 2 {
		return loopAppend{}, false
	}
	definition, isDefinition := shedStatement(statement.Body.List[0]).(*ast.AssignStmt)
	// A lone pattern node against a list matches a list of one, so the definition defines one name from one element.
	if !isDefinition || definition.Tok != token.DEFINE || len(definition.Lhs) != 1 || len(definition.Rhs) != 1 {
		return loopAppend{}, false
	}
	value, isValue := ast.Unparen(definition.Lhs[0]).(*ast.Ident)
	if !isValue || !isElement(context, definition.Rhs[0], source, indexObject) {
		return loopAppend{}, false
	}
	valueObject := context.TypesInfo.ObjectOf(value)
	destination, call, isAppend := appendAssignment(context, statement.Body.List[1])
	if !isAppend || !namesObject(context, call.Args[1], valueObject) {
		return loopAppend{}, false
	}
	return loopAppend{
		destination: destination, source: source, value: valueObject, index: indexObject, call: call,
	}, true
}

// indexLoop is the head the second and third alternatives share, `(RangeStmt idx@(Object _) nil _ x ...)`: a key that
// is an identifier, parenthesized or not, whose object it returns, no value, and the slice ranged over with its
// parentheses removed.
func indexLoop(context *rule.Context, statement *ast.RangeStmt) (source ast.Expr, index types.Object, isIndex bool) {
	key, isKey := ast.Unparen(statement.Key).(*ast.Ident)
	if !isKey || statement.Value != nil {
		return nil, nil, false
	}
	return ast.Unparen(statement.X), context.TypesInfo.ObjectOf(key), true
}

// appendAssignment is the statement every alternative ends with, `(AssignStmt [lhs] "=" [(CallExpr (Builtin
// "append") [lhs _])])`: one plain assignment of one value, a call of the universe's append with two arguments, the
// first the very syntax assigned to. It returns what is assigned to, its parentheses removed as the binding stores
// it, and the call, whose second argument the alternative judges.
func appendAssignment(context *rule.Context, statement ast.Stmt) (destination ast.Expr, call *ast.CallExpr, is bool) {
	assignment, isAssignment := shedStatement(statement).(*ast.AssignStmt)
	// The String "=" against a token is the operator, so `:=` and `+=` are not it.
	if !isAssignment || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil, nil, false
	}
	call, isCall := ast.Unparen(assignment.Rhs[0]).(*ast.CallExpr)
	if !isCall || !staticcheck_code.IsBuiltin(context, call.Fun, "append") || len(call.Args) != 2 {
		return nil, nil, false
	}
	// lhs is bound to a node, so its recall as append's first argument compares syntax, not objects.
	destination = ast.Unparen(assignment.Lhs[0])
	if !staticcheck_code.SameSyntax(destination, call.Args[0]) {
		return nil, nil, false
	}
	return destination, call, true
}

// isElement is whether expression is `(IndexExpr x idx)` with x and idx recalled: an index expression, parenthesized
// or not, of the syntax source holds, x being a node, at an index naming index, idx being an object.
func isElement(context *rule.Context, expression ast.Expr, source ast.Expr, index types.Object) bool {
	element, isIndex := ast.Unparen(expression).(*ast.IndexExpr)
	return isIndex && staticcheck_code.SameSyntax(source, element.X) && namesObject(context, element.Index, index)
}

// namesObject is whether expression names object, as the pattern recalls the object a binding holds: an identifier
// whose object it is, or a selector whose selected name's object it is, parenthesized or not. The objects are
// compared by identity, so two variables spelled alike are two. A nil object, bound from an identifier the type
// checker recorded nothing for, is a recall of nothing, which match.go never matches.
func namesObject(context *rule.Context, expression ast.Expr, object types.Object) bool {
	if object == nil {
		return false
	}
	switch expression := ast.Unparen(expression).(type) {
	case *ast.Ident:
		return context.TypesInfo.ObjectOf(expression) == object
	case *ast.SelectorExpr:
		return context.TypesInfo.ObjectOf(expression.Sel) == object
	default:
		return false
	}
}

// shedStatement is statement as the pattern language reaches a statement pattern through it: a labeled statement is
// the statement it labels, and a block of one statement is that statement, at any depth, so `{ x = append(x, v) }`
// in a loop's body matches as the assignment does. Anything else is itself, which an assignment pattern then matches
// or refuses.
func shedStatement(statement ast.Stmt) ast.Stmt {
	reached := statement
	for {
		switch shed := reached.(type) {
		case *ast.LabeledStmt:
			reached = shed.Stmt
		case *ast.BlockStmt:
			if len(shed.List) != 1 {
				return shed
			}
			reached = shed.List[0]
		default:
			return reached
		}
	}
}
