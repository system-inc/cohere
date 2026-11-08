// Package argument_overwritten_before_use is staticcheck's SA4009, a parameter assigned in its function's body before
// anything reads the value the caller passed, ported onto staticcheck's intermediate representation as a
// rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4009/sa4009.go, whose logic it keeps and
// whose messages it reports word for word: the finding from the start to the end of the parameter's name, and beside
// it, as related information, the first assignment to the parameter, from its start to its end, where staticcheck's
// report and its Related option put them. The port sets the related information by hand, since staticcheck's report
// cannot be imported; the shared report kit (#wtvf02h) carries Related later. The original walks every source function
// of a package in one run, but keeps nothing from one function to the next: it walks the syntax of each function and
// judges each parameter by that same function's intermediate representation. So the port judges one function at a
// time and finds what the original finds. The syntax of a function holds the syntax of the function literals inside
// it, whose parameters are none of the enclosing function's, so each literal's parameters are judged only by the
// literal's own function, and each is reported once. The original stays the oracle: the parity tests beside this file
// run both over the same packages, comparing the related information too, and they differ nowhere, since the port
// walks the same syntax and asks the same function the same questions of it.
package argument_overwritten_before_use

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4009",
	"SA4009",
	"Flags a function argument overwritten before its first use: staticcheck's SA4009, ported onto its "+
		"intermediate representation.",
	&Listener,
)

// Check judges one function: each named parameter nothing in the function reads, reported when the body assigns to
// it, so the value the caller passed is lost unread. The intermediate representation lifts a parameter into the
// values it flows to, so one with no readers is one whose passed value nothing reads, not even a closure capturing it
// or a pointer taken to it, which would read it into memory.
func Check(context *rule.Context, function *ir.Function) {
	source := function.Source()
	if source == nil {
		// A function with no syntax, a wrapper or a synthesized init, holds no parameter to judge.
		return
	}
	ast.Inspect(source, func(node ast.Node) bool {
		var functionType *ast.FuncType
		var body *ast.BlockStmt
		switch node := node.(type) {
		case *ast.FuncDecl:
			functionType = node.Type
			body = node.Body
		case *ast.FuncLit:
			functionType = node.Type
			body = node.Body
		}
		// A declaration without a body is implemented elsewhere, and any other node is no function.
		if body == nil {
			return true
		}
		for _, field := range functionType.Params.List {
			for _, name := range field.Names {
				object := context.TypesInfo.ObjectOf(name)
				parameter := parameterOf(function, object)
				// A literal's parameters are none of the enclosing function's, and are judged by the literal.
				if parameter == nil {
					continue
				}
				referrers := parameter.Referrers()
				if referrers == nil || len(*referrers) != 0 {
					continue
				}
				assignment := firstAssignment(context, body, object)
				if assignment == nil {
					continue
				}
				context.Report(analysis.Diagnostic{
					Pos:     name.Pos(),
					End:     name.End(),
					Message: fmt.Sprintf("argument %s is overwritten before first use", name.Name),
					Related: []analysis.RelatedInformation{{
						Pos:     assignment.Pos(),
						End:     assignment.End(),
						Message: fmt.Sprintf("assignment to %s", name.Name),
					}},
				})
			}
		}
		return true
	})
}

// parameterOf is the parameter of function declared as object, or nil when function has none.
func parameterOf(function *ir.Function, object types.Object) *ir.Parameter {
	for _, parameter := range function.Params {
		if parameter.Object() == object {
			return parameter
		}
	}
	return nil
}

// firstAssignment is the first assignment in body, in the order the syntax reads, with object as a bare name among its
// left sides, or nil when there is none. An assignment through a pointer or to a field is no overwrite of the
// parameter, and an assignment inside a function literal in body counts as much as one in body itself.
func firstAssignment(context *rule.Context, body *ast.BlockStmt, object types.Object) *ast.AssignStmt {
	var assignment *ast.AssignStmt
	ast.Inspect(body, func(node ast.Node) bool {
		if assignment != nil {
			return false
		}
		statement, isAssign := node.(*ast.AssignStmt)
		if !isAssign {
			return true
		}
		for _, left := range statement.Lhs {
			name, isName := left.(*ast.Ident)
			if isName && context.TypesInfo.ObjectOf(name) == object {
				assignment = statement
				return false
			}
		}
		return true
	})
	return assignment
}
