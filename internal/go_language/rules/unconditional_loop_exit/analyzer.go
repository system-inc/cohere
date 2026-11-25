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
// exit at the top level is reported. The registry runs the port in the original's place, under its name, and the
// original stays only as the oracle: the parity tests beside this file run both over the same modules, and the two
// differ nowhere they hold.
package unconditional_loop_exit

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_language/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every function declaration and function literal,
// in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)},
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
		// Invariant: the dispatcher hands Node only the function declarations and literals it was asked for, and Node
		// hands each on to Check.
		panic(fmt.Sprintf("unconditional_loop_exit: Check was handed a %T, not a function", function))
	}
	if body == nil {
		// A function declared without a body, implemented elsewhere, holds no loop.
		return
	}
	// The walks read the inspector's cursor of the body rather than ast.Inspect, so a subtree holding no loop, label or
	// branch statement is skipped by the inspector's type masks. The findings stay the same: the cursor visits the
	// nodes of the kinds it is asked for in ast.Inspect's preorder, descends into every node of another kind, which
	// ast.Inspect's callback here always descended into, and skips a loop's subtree when checkLoop returns false, as
	// ast.Inspect's pruning did.
	bodyCursor := context.Cursor().Child(body)
	labels := &labelTargets{body: bodyCursor}
	bodyCursor.Inspect([]ast.Node{(*ast.ForStmt)(nil), (*ast.RangeStmt)(nil)}, func(loopCursor inspector.Cursor) bool {
		return checkLoop(context, labels, loopCursor)
	})
}

// checkLoop judges the loop at loopCursor if it is one the check reads, and is whether the walk goes on into the
// loop's children.
func checkLoop(context *rule.Context, labels *labelTargets, loopCursor inspector.Cursor) bool {
	var loop ast.Stmt
	var body *ast.BlockStmt
	switch node := loopCursor.Node().(type) {
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
		// Invariant: Check's walk hands checkLoop only the for and range statements it filters for.
		panic(fmt.Sprintf("unconditional_loop_exit: checkLoop was handed a %T, not a loop", node))
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
	if mayRunAgain(context, labels, loopCursor.Child(body), loop) {
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
//
// body is the cursor of the loop's body, never of the loop, so a branch statement in a function literal in the loop's
// header is not read, as the original's walk of the body alone never reads it. It answers at the first such statement,
// where the original's walk reads on, but nothing it reads after that changes the original's verdict.
func mayRunAgain(context *rule.Context, labels *labelTargets, body inspector.Cursor, loop ast.Stmt) bool {
	for branchCursor := range body.Preorder((*ast.BranchStmt)(nil)) {
		branch, isBranch := branchCursor.Node().(*ast.BranchStmt)
		if !isBranch {
			// The walk hands only the branch statements it filters for, so this is never taken.
			continue
		}
		switch branch.Tok {
		case token.GOTO:
			return true
		case token.CONTINUE:
			if branch.Label == nil || targets(context, labels, branch.Label, loop) {
				return true
			}
		}
	}
	return false
}

// labelTargets is the statement each label in the body of the function being judged names, by the label's object, the
// map the original builds by walking the whole body before it judges a loop. It is built only on the first lookup,
// since a labelled break or continue is the only thing that reads it and most functions hold none. The findings stay
// the same: targets is its only reader, and the build walks the whole body, a function literal's labels included, as
// the original's does, so the first lookup reads the map the original would have read, and nothing changes it after.
type labelTargets struct {
	// body is the cursor of the body of the function being judged, whose labels the map holds, read only during the
	// Node call that judges the function, where the cursor holds.
	body inspector.Cursor
	// statements is the statement each label names, by its object, and nil until the first lookup builds it.
	statements map[types.Object]ast.Stmt
}

// targets is whether label names loop, from the labels of the function being judged, built on the first call.
//
// Mutates: labels gains its map of the body's labels on the first call, read again on every call after
func targets(context *rule.Context, labels *labelTargets, label *ast.Ident, loop ast.Stmt) bool {
	if labels.statements == nil {
		labels.statements = map[types.Object]ast.Stmt{}
		for labeledCursor := range labels.body.Preorder((*ast.LabeledStmt)(nil)) {
			if labeled, isLabeled := labeledCursor.Node().(*ast.LabeledStmt); isLabeled {
				labels.statements[context.TypesInfo.ObjectOf(labeled.Label)] = labeled.Stmt
			}
		}
	}
	return labels.statements[context.TypesInfo.ObjectOf(label)] == loop
}
