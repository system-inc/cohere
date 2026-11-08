// Package unconditional_loop_exit is staticcheck's SA4004, a loop whose body branches and then, at its top level,
// breaks out of the loop or returns, so the loop never runs a second time, ported as a check on syntax that reports
// through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4004/sa4004.go, whose logic it keeps and
// whose message it reports word for word, over the break or return statement, where staticcheck's report puts it,
// through staticcheck_report.Report, the port of the report the original calls, proven against it beside it. It reads
// the set of types a ranged expression may have with staticcheck's go/types/typeutil, as the original does. Like the
// original, it gives up on a loop holding a goto anywhere, or an unlabelled continue anywhere, even in a nested loop or
// a function literal, and it leaves alone a loop over a map, which is a way to take an arbitrary element, and over a
// function, whose semantics it cannot know. It keeps three of the original's quirks, so the two agree finding for
// finding: a loop whose top level has no exit or no branching statement is not descended into, so a loop nested in it
// goes unjudged (parity's nestedInLoopWithoutExit); a function literal is walked both as part of the function holding
// it and as itself, so a finding in a loop inside it is reported twice (parity's nestedLiteral); and only the last
// exit at the top level is reported. The registry still runs the original, which stays the oracle: the parity tests
// beside this file run both over the same modules, and the two differ nowhere they hold.
package unconditional_loop_exit

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every function declaration and function literal,
// in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		Check(context, node)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4004",
	"SA4004",
	"Flags a loop that exits unconditionally after one iteration: staticcheck's SA4004, ported as a check on "+
		"syntax.",
	&Listener,
)

// Check judges every loop in the body of one function declaration or function literal, those in function literals
// inside it included, as the original walks them.
func Check(context *rule.Context, function ast.Node) {
	var body *ast.BlockStmt
	switch function := function.(type) {
	case *ast.FuncDecl:
		body = function.Body
	case *ast.FuncLit:
		body = function.Body
	default:
		// Invariant: run hands over only the function declarations and literals it asked the inspector for.
		panic(fmt.Sprintf("unconditional_loop_exit: Check was handed a %T, not a function", function))
	}
	if body == nil {
		// A function declared without a body, implemented elsewhere, holds no loop.
		return
	}
	labels := map[types.Object]ast.Stmt{}
	ast.Inspect(body, func(node ast.Node) bool {
		if labeled, isLabeled := node.(*ast.LabeledStmt); isLabeled {
			labels[context.TypesInfo.ObjectOf(labeled.Label)] = labeled.Stmt
		}
		return true
	})
	ast.Inspect(body, func(node ast.Node) bool {
		return checkLoop(context, labels, node)
	})
}

// checkLoop judges node if it is a loop the check reads, and is whether the walk goes on into node's children.
func checkLoop(context *rule.Context, labels map[types.Object]ast.Stmt, node ast.Node) bool {
	var loop ast.Stmt
	var body *ast.BlockStmt
	switch node := node.(type) {
	case *ast.ForStmt:
		loop = node
		body = node.Body
	case *ast.RangeStmt:
		if !rangesOverSequence(context, node) {
			return true
		}
		loop = node
		body = node.Body
	default:
		return true
	}
	// A body of one statement cannot both branch and exit unconditionally, and it is the common way to take the first
	// element of a slice or the first rune of a string, so it is left alone and its children still walked.
	if len(body.List) < 2 {
		return true
	}
	var unconditionalExit ast.Stmt
	hasBranching := false
	for _, statement := range body.List {
		switch statement := statement.(type) {
		case *ast.BranchStmt:
			switch statement.Tok {
			case token.BREAK:
				if statement.Label == nil || targets(context, labels, statement.Label, loop) {
					unconditionalExit = statement
				}
			case token.CONTINUE:
				if statement.Label == nil || targets(context, labels, statement.Label, loop) {
					// A continue at the top level runs the loop again, and the original walks no further into it.
					return false
				}
			}
		case *ast.ReturnStmt:
			unconditionalExit = statement
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.SelectStmt:
			hasBranching = true
		}
	}
	// The original stops the walk here too, so a loop nested in this one is never judged; kept for parity.
	if unconditionalExit == nil || !hasBranching {
		return false
	}
	if mayRunAgain(context, labels, body, loop) {
		return true
	}
	staticcheck_report.Report(context, unconditionalExit, "the surrounding loop is unconditionally terminated")
	return true
}

// rangesOverSequence is whether every type the ranged expression of loop may have is a slice, a channel, a basic type
// (a string or an integer), a pointer to an array, or an array, the types a loop run once over is likely a mistake.
// A map, ranged once to take an arbitrary element, and a function, whose semantics are unknown, are not, and neither
// is a type parameter whose set of types is unconstrained.
func rangesOverSequence(context *rule.Context, loop *ast.RangeStmt) bool {
	return typeutil.All(context.TypesInfo.TypeOf(loop.X), func(term *types.Term) bool {
		switch underlying := term.Type().Underlying().(type) {
		case *types.Slice, *types.Chan, *types.Basic, *types.Pointer, *types.Array:
			return true
		case *types.Map, *types.Signature:
			return false
		default:
			// Invariant: a loop that type-checks ranges over one of the kinds above, so another is the type checker
			// accepting a range the language does not, or a kind the language added since; the original panics too.
			panic(fmt.Sprintf("unconditional_loop_exit: a range over a %T", underlying))
		}
	})
}

// mayRunAgain is whether the body of loop holds, at any depth, a goto, which might jump past the exit, or a continue
// that might run the loop again: an unlabelled one even in a nested loop or function literal, where it continues
// something else, or one labelled with this loop.
func mayRunAgain(context *rule.Context, labels map[types.Object]ast.Stmt, body *ast.BlockStmt, loop ast.Stmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		branch, isBranch := node.(*ast.BranchStmt)
		if !isBranch {
			return true
		}
		switch branch.Tok {
		case token.GOTO:
			found = true
		case token.CONTINUE:
			found = branch.Label == nil || targets(context, labels, branch.Label, loop)
		}
		return !found
	})
	return found
}

// targets is whether label names loop, from the labels of the function being judged.
func targets(context *rule.Context, labels map[types.Object]ast.Stmt, label *ast.Ident, loop ast.Stmt) bool {
	return labels[context.TypesInfo.ObjectOf(label)] == loop
}
