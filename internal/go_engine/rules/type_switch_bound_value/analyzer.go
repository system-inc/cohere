// Package type_switch_bound_value is staticcheck's S1034, a type switch that binds nothing, `switch x.(type)`, whose
// cases assert the switched variable again to the case's type, `x.(int)` under `case int:`, which `switch x :=
// x.(type)` would hand them already asserted, ported as a check on types and syntax that reports through the kit
// every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1034/s1034.go, whose logic it keeps and whose
// message, related entries and fix it reports word for word, at the same range, the switch's type assertion. The
// original matches `(TypeSwitchStmt nil expr@(TypeAssertExpr ident@(Ident _) _) body)` through staticcheck's pattern
// language, and builds its fix's binding from `(AssignStmt ident ":=" expr)` through edit.ReplaceWithPattern; the
// port asks the same of each type switch directly, reading pattern/match.go for what each construct of the query
// matches, builds the same assignment as syntax for edit.ReplaceWithNode, and reports through
// staticcheck_report.Report where the original reports through report.Report. No binding of the query is recalled
// within it, so nothing is compared by syntax. The registry runs the port in the original's place, under its name, and
// the original stays only as the oracle: the parity tests beside this file run both over the same packages, and the two
// differ only where the original's fix would be wrong, which the differences fixture records. An assertion in the
// case's own type, `case [unsafe.Sizeof(x.([8]byte))]byte:`, is outside the scope the binding declares, so the port
// leaves it as it is, names it in no related entry, and finds nothing where it is all a switch would replace. A
// comma-ok assertion, `v, ok := x.(int)`, is replaced with `v, ok := x, true`, where the original's `v, ok := x`
// assigns one value to two. And where the switch writes to x or a clause declares its own x, the rebinding would not
// compile or would write to a copy, so the port keeps the finding and withholds the fix.
package type_switch_bound_value

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every type switch, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.TypeSwitchStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		typeSwitch, isTypeSwitch := node.(*ast.TypeSwitchStmt)
		if !isTypeSwitch {
			// Invariant: the dispatcher hands Node only the type switches it was asked for.
			panic("type_switch_bound_value: the dispatcher handed over a node that is not a type switch")
		}
		Check(context, typeSwitch)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1034",
	"S1034",
	"Flags a type switch whose cases assert the switched variable again instead of binding it: "+
		"staticcheck's S1034, ported.",
	&Listener,
)

// Check judges one type switch: one with no init statement that switches on a variable named by an identifier and
// binds nothing, where some case of one type asserts that variable to that type and to nothing else, could bind the
// asserted value once. Each such assertion is a related entry, and when no case of one type asserts anything else,
// the fix binds the switch's value under the variable's own name and replaces each assertion in a case's body with
// the name alone, or a comma-ok one with the name and true. A switch that writes to the variable, or a clause that
// declares its name in its own block, takes no fix.
func Check(context *rule.Context, typeSwitch *ast.TypeSwitchStmt) {
	// The query's nil Init: `switch y := f(); x.(type)` is never one.
	if typeSwitch.Init != nil {
		return
	}
	// The pattern looks through an ExprStmt, so `expr` is the guard of a switch that binds nothing; a guard that
	// binds, `switch y := x.(type)`, is an AssignStmt, which no TypeAssertExpr matches. Parentheses are looked
	// through too, as the pattern does everywhere, though the parser never leaves any around a type switch's guard.
	statement, isExpressionStatement := typeSwitch.Assign.(*ast.ExprStmt)
	if !isExpressionStatement {
		return
	}
	assertion, isAssertion := ast.Unparen(statement.X).(*ast.TypeAssertExpr)
	if !isAssertion {
		return
	}
	// `ident@(Ident _)` is an identifier of any name, parentheses looked through, and the binding holds the
	// identifier inside them: `switch (x).(type)` binds x, and its fix keeps the parentheses in what it assigns.
	switched, isIdentifier := ast.Unparen(assertion.X).(*ast.Ident)
	if !isIdentifier {
		return
	}
	switchedObject := context.TypesInfo.ObjectOf(switched)
	// The assertions the binding can replace: those in a case's body, where the binding is declared. The original
	// gathers every offender, those in a case's type too, which the port leaves as they are.
	var bodyOffenders []*ast.TypeAssertExpr
	canSuggestFix := true
	for _, entry := range typeSwitch.Body.List {
		clause, isClause := entry.(*ast.CaseClause)
		if !isClause {
			// Invariant: the parser fills a type switch's body with case clauses alone.
			panic("type_switch_bound_value: a type switch's body holds a statement that is not a case clause")
		}
		// A case of several types, or the default, binds the switched value at its own type, so an assertion
		// there would still be needed; neither is judged, and neither keeps the fix from being offered.
		if len(clause.List) != 1 {
			continue
		}
		offenders, hasUnrelatedAssertion := offendersOf(context, clause, switchedObject)
		if !hasUnrelatedAssertion {
			// A case that asserts something else as well is left alone: often both values are asserted to the
			// same type for symmetry, and the original says so.
			for _, offender := range offenders {
				// The case's type is read where the switch is, before the clause's binding is declared, so x there
				// is still the switched interface, and an assertion of it in the type is needed:
				// `[unsafe.Sizeof(x.([8]byte))]byte` with x in its place is `[16]byte`. The original replaces it
				// too, which changes the case's type, and the port leaves it as it is.
				if offender.Pos() >= clause.List[0].Pos() && offender.End() <= clause.List[0].End() {
					continue
				}
				bodyOffenders = append(bodyOffenders, offender)
			}
		}
		canSuggestFix = canSuggestFix && !hasUnrelatedAssertion
	}
	// Where every assertion the original finds is in a case's type, binding the value eliminates none, and the
	// binding the fix would write is used in no case, which does not compile: the port finds nothing.
	if len(bodyOffenders) == 0 {
		return
	}
	// The fix declares x anew in every clause, at the clause's type in a case of one type and as a copy in the rest.
	// A write to the switched variable then either does not compile, `x = nil` in `case int:`, or writes to the
	// copy, `&x` in the default clause, and a clause that declares x in its own block declares it twice.
	canSuggestFix = canSuggestFix && !isWrittenIn(context, typeSwitch, switchedObject) &&
		!isNameDeclaredInClause(context, typeSwitch, switched.Name)
	options := []staticcheck_report.Option{}
	// An assertion in a case's type is one the binding can't eliminate, so the original's related entry on it says
	// what isn't so, and the port names only the assertions its fix replaces.
	for _, offender := range bodyOffenders {
		options = append(options, staticcheck_report.Related(offender, "could eliminate this type assertion"))
	}
	options = append(options, staticcheck_report.FilterGenerated())
	message := fmt.Sprintf("assigning the result of this type assertion to a variable (switch %s := %s.(type)) "+
		"could eliminate type assertions in switch cases",
		staticcheck_report.Render(context, switched), staticcheck_report.Render(context, switched))
	if canSuggestFix {
		// `(AssignStmt ident ":=" expr)`: the identifier and the assertion as they were bound, under no position
		// of their own, which is the node the original's pattern builds for the same printer.
		binding := &ast.AssignStmt{Lhs: []ast.Expr{switched}, Tok: token.DEFINE, Rhs: []ast.Expr{assertion}}
		edits := []analysis.TextEdit{edit.ReplaceWithNode(context.FileSet, assertion, binding)}
		commaOkValues := commaOkValuesOf(typeSwitch)
		for _, offender := range bodyOffenders {
			// A comma-ok assertion in the case of its own type always holds, so its two values are the bound
			// variable and true, written over the value as it stands, parentheses and all, since `(x, true)` is no
			// expression. The original writes the name alone, which assigns one value to two and does not compile.
			if value, isCommaOk := commaOkValues[offender]; isCommaOk {
				edits = append(edits,
					edit.ReplaceWithString(value, staticcheck_report.Render(context, offender.X)+", true"))
				continue
			}
			edits = append(edits, edit.ReplaceWithNode(context.FileSet, offender, offender.X))
		}
		options = append(options, staticcheck_report.Fixes(edit.Fix("Simplify type switch", edits...)))
	}
	staticcheck_report.Report(context, assertion, message, options...)
}

// offendersOf is every type assertion in clause, its case list included, nested functions and switches too, that
// asserts the switched variable to the clause's one type, and whether the clause holds any other type assertion: one
// of something not an identifier (a parenthesized identifier included, as the original reads the operand bare), of
// another variable, or to another type, the guard of a type switch nested in the clause among them. Inspection does
// not descend into an unrelated assertion, and does into an offender.
func offendersOf(
	context *rule.Context, clause *ast.CaseClause, switchedObject types.Object,
) (offenders []*ast.TypeAssertExpr, hasUnrelatedAssertion bool) {
	ast.Inspect(clause, func(node ast.Node) bool {
		assertion, isAssertion := node.(*ast.TypeAssertExpr)
		if !isAssertion {
			return true
		}
		asserted, isIdentifier := assertion.X.(*ast.Ident)
		if !isIdentifier || context.TypesInfo.ObjectOf(asserted) != switchedObject {
			hasUnrelatedAssertion = true
			return false
		}
		// The types are compared by identity, so an alias of the case's type is the case's type. A nested type
		// switch's guard has no type expression, whose type is nil, which is identical to no case's.
		if !types.Identical(context.TypesInfo.TypeOf(clause.List[0]), context.TypesInfo.TypeOf(assertion.Type)) {
			hasUnrelatedAssertion = true
			return false
		}
		offenders = append(offenders, assertion)
		return true
	})
	return offenders, hasUnrelatedAssertion
}

// commaOkValuesOf is each type assertion in typeSwitch's body that is the one value assigned to two, `v, ok :=
// x.(T)`, `v, ok = x.(T)` or `var v, ok = x.(T)`, parentheses around it looked through, keyed to that value as
// written, which is what a fix replaces.
func commaOkValuesOf(typeSwitch *ast.TypeSwitchStmt) map[*ast.TypeAssertExpr]ast.Expr {
	values := map[*ast.TypeAssertExpr]ast.Expr{}
	record := func(targets int, assigned []ast.Expr) {
		if targets != 2 || len(assigned) != 1 {
			return
		}
		if assertion, isAssertion := ast.Unparen(assigned[0]).(*ast.TypeAssertExpr); isAssertion {
			values[assertion] = assigned[0]
		}
	}
	ast.Inspect(typeSwitch.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			record(len(node.Lhs), node.Rhs)
		case *ast.ValueSpec:
			record(len(node.Names), node.Values)
		}
		return true
	})
	return values
}

// isWrittenIn is whether typeSwitch's body writes to the variable switchedObject is, by object, parentheses looked
// through: assigned, `x = v`, `x, ok = v, true` or `x += v`, assigned by a range, `for x = range values`, or its
// address taken, `&x`. An increment or decrement of an interface doesn't type-check, so none is looked for.
func isWrittenIn(context *rule.Context, typeSwitch *ast.TypeSwitchStmt, switchedObject types.Object) bool {
	isSwitched := func(expression ast.Expr) bool {
		identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
		return isIdentifier && context.TypesInfo.ObjectOf(identifier) == switchedObject
	}
	isWritten := false
	ast.Inspect(typeSwitch.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			if slices.ContainsFunc(node.Lhs, isSwitched) {
				isWritten = true
			}
		case *ast.RangeStmt:
			if node.Tok == token.ASSIGN && (isSwitched(node.Key) || isSwitched(node.Value)) {
				isWritten = true
			}
		case *ast.UnaryExpr:
			if node.Op == token.AND && isSwitched(node.X) {
				isWritten = true
			}
		}
		return !isWritten
	})
	return isWritten
}

// isNameDeclaredInClause is whether a clause of typeSwitch declares name in its own block, `x := 0` among the
// clause's statements, where the fix's binding is declared too. A declaration in a block nested in the clause
// shadows the binding, which compiles.
func isNameDeclaredInClause(context *rule.Context, typeSwitch *ast.TypeSwitchStmt, name string) bool {
	for _, entry := range typeSwitch.Body.List {
		scope, hasScope := context.TypesInfo.Scopes[entry]
		if hasScope && scope.Lookup(name) != nil {
			return true
		}
	}
	return false
}
