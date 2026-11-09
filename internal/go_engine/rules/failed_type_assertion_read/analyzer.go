// Package failed_type_assertion_read is staticcheck's SA9008, the else branch of an if statement whose initializer
// type-asserts a variable into a new variable of the same name, `if x, ok := x.(T); ok`, reading that new variable,
// which holds the zero value of the failed assertion and not the value asserted, ported onto the engine's own
// intermediate representation (#tf4cb8g).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa9008/sa9008.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the identifier read, with the two related
// places staticcheck's report.Related gives it, through staticcheck_report.Report and Related, and the identifier
// written as staticcheck's report.Render writes it, through staticcheck_report.Render. The original walks the
// package's if statements in its syntax and finds the function each one belongs to with ir.EnclosingFunction, handed
// the package buildir built; the port walks each function's own syntax in the engine's own intermediate
// representation, leaving out the function literals inside it, which are functions of their own, and asks its
// EnclosingFunction, go/ir's translated, the same question of the same package, which every function carries as its
// Package, judging an if statement only in the function it names.
// So each if statement is judged once, in the function the original finds for it, and one the original finds no
// function for, in a declaration named _, is judged nowhere. Neither keeps anything from one if statement to the
// next. The original matches the if statement with staticcheck's pattern language, through analysis/code; the port
// matches the same shape in the syntax directly: an initializer `:=` of two identifiers from one type assertion of an
// identifier spelled as the first, a condition that is the second identifier, and an else branch, parentheses
// stripped wherever the pattern strips them. The registry runs the port in the original's place, under its name, and
// the original stays only as the oracle: the parity tests beside this file run both over the same packages, related
// places included, and they differ nowhere.
package failed_type_assertion_read

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/ast/astutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{
		Function:                Check,
		ReadsValueForExpression: true,
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA9008",
	"SA9008",
	"Flags an else branch reading the variable a failed type assertion shadowed: staticcheck's SA9008, ported onto "+
		"the engine's own intermediate representation.",
	&Listener,
)

// messageFormat is the original's message, word for word, around the identifier read.
const messageFormat = "%s refers to the result of a failed type assertion and is a zero value, not the value that " +
	"was being type-asserted"

// Check judges one function: each if statement in its own syntax that type-asserts a variable into a new one of the
// same name and branches on the assertion's success, whose else branch reads the new variable while it still holds
// the assertion's zero value.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	var body *ast.BlockStmt
	switch syntax := function.Source().(type) {
	case *ast.FuncDecl:
		body = syntax.Body
	case *ast.FuncLit:
		body = syntax.Body
	}
	if body == nil {
		// A function with no syntax of its own, the package's initializer, holds no if statement but in the function
		// literals of its variables, which are functions of their own.
		return
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.IfStmt:
			checkIf(context, function, node)
		}
		return true
	})
}

// checkIf reports each read in statement's else branch of the variable its initializer's type assertion shadows,
// while the variable holds the value the failed assertion gave it.
func checkIf(context *rule.Context, function *intermediate_representation.Function, statement *ast.IfStmt) {
	shadowing, assertion, isShadowing := shadowingAssertion(statement)
	if !isShadowing || statement.Else == nil {
		return
	}
	shadow := context.TypesInfo.ObjectOf(shadowing)
	path, isExact := astutil.PathEnclosingInterval(staticcheck_report.File(context, shadow), shadow.Pos(),
		shadow.Pos())
	if !isExact {
		return
	}
	// The original judges the if statement in the function EnclosingFunction finds, and none when it finds none, as
	// for a declaration named _, which staticcheck builds but never lists among the package's members.
	if intermediate_representation.EnclosingFunction(function.Package, path) != function {
		return
	}
	shadowValue, isAddress := function.ValueForExpression(shadowing)
	if shadowValue == nil || isAddress {
		return
	}
	ast.Inspect(statement.Else, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier || context.TypesInfo.ObjectOf(identifier) != shadow {
			return true
		}
		value, isAddress := function.ValueForExpression(identifier)
		if value == nil || isAddress {
			return true
		}
		if intermediate_representation.Flatten(value) != shadowValue {
			// The same variable, but another value: assigned since the assertion, or moved to the heap. Either way,
			// the read is not of the failed assertion's zero value.
			return true
		}
		staticcheck_report.Report(context, identifier,
			fmt.Sprintf(messageFormat, staticcheck_report.Render(context, identifier)),
			staticcheck_report.Related(shadow, "this is the variable being read"),
			staticcheck_report.Related(assertion.X, "this is the variable being shadowed"))
		return true
	})
}

// shadowingAssertion is the new variable and the type assertion of an if statement initialized as `x, ok := x.(T)`
// and conditioned on `ok`, the shape the original's pattern matches: two identifiers defined from one type assertion
// whose operand is an identifier spelled as the first, the condition an identifier spelled as the second, each
// operand looked through its parentheses as the pattern looks through them. The identifiers are compared by spelling,
// as the pattern compares a binding it recalls.
func shadowingAssertion(statement *ast.IfStmt) (*ast.Ident, *ast.TypeAssertExpr, bool) {
	assignment, isAssignment := statement.Init.(*ast.AssignStmt)
	if !isAssignment || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 {
		return nil, nil, false
	}
	shadowing, isShadowingIdentifier := assignment.Lhs[0].(*ast.Ident)
	success, isSuccessIdentifier := assignment.Lhs[1].(*ast.Ident)
	if !isShadowingIdentifier || !isSuccessIdentifier {
		return nil, nil, false
	}
	assertion, isAssertion := ast.Unparen(assignment.Rhs[0]).(*ast.TypeAssertExpr)
	if !isAssertion {
		return nil, nil, false
	}
	asserted, isAssertedIdentifier := ast.Unparen(assertion.X).(*ast.Ident)
	if !isAssertedIdentifier || asserted.Name != shadowing.Name {
		return nil, nil, false
	}
	condition, isConditionIdentifier := ast.Unparen(statement.Cond).(*ast.Ident)
	if !isConditionIdentifier || condition.Name != success.Name {
		return nil, nil, false
	}
	return shadowing, assertion, true
}
