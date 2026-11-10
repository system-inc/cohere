// Package manual_trim is staticcheck's S1017, an if statement whose condition asks strings.HasPrefix,
// HasSuffix or Contains, or their bytes twins, and whose body only trims or replaces what was asked about, which an
// unconditional TrimPrefix, TrimSuffix or Replace says without the guard, ported as a check on types and syntax that
// reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1017/s1017.go, whose logic it keeps and whose
// messages it reports word for word, at the same ranges: the whole if statement for a guarded call, and its header for
// a guarded slice. The original queries no pattern; it walks every if statement with code.Preorder and judges it with
// code.CallName, code.IsCallTo, code.ExprToInt, code.ExprToString and astutil.Equal, and the port asks the same of the
// same nodes, the code helpers from the kit and astutil.Equal mirrored as syntaxEqual, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ in two places alone, adjudicated, both in sameNonDynamic: the port
// drops a guard on a selector or index that holds a call, and declines where astutil.Equal would panic.
package manual_trim

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"slices"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every if statement, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.IfStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		ifStatement, isIf := node.(*ast.IfStmt)
		if !isIf {
			// Invariant: the dispatcher hands Node only the if statements it was asked for.
			panic("manual_trim: the dispatcher handed over a node that is not an if statement")
		}
		// The check depends on the preorder's order, as the original's seen set does: an if statement comes before
		// its else, so an else if is in seen by the time the preorder reaches it, and is left alone. The set is the
		// rule's state over the package, made empty on its first if statement.
		seen := rule.State[Seen](context)
		if elseStatement := Check(context, ifStatement, seen); elseStatement != nil {
			seen.record(elseStatement)
		}
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1017",
	"S1017",
	"Flags an if statement that only trims what its condition checks for: staticcheck's S1017, ported.",
	&Listener,
)

// guard is what one function a condition may call stands for: the package and function the original's switch
// spells, the call that does its work unconditionally, and the trimming function a manual slice is replaced with.
type guard struct {
	// packageName is the package as the second message spells it, the import path's, whatever the file names it.
	packageName string
	// function is the condition's function, HasPrefix, HasSuffix or Contains.
	function string
	// unconditional is the CallName of the call a body may make in the guard's place.
	unconditional string
	// trim is the function a manual slice is replaced with, and empty for Contains, which no slice stands for.
	trim string
}

// guards is every function a condition may call, by the CallName the original's switch compares, which names a
// function by its package's import path, so a renamed import of strings is strings and another package called
// strings is not.
var guards = map[string]guard{
	"strings.HasPrefix": {
		packageName: "strings", function: "HasPrefix", unconditional: "strings.TrimPrefix", trim: "TrimPrefix",
	},
	"strings.HasSuffix": {
		packageName: "strings", function: "HasSuffix", unconditional: "strings.TrimSuffix", trim: "TrimSuffix",
	},
	"strings.Contains": {packageName: "strings", function: "Contains", unconditional: "strings.Replace"},
	"bytes.HasPrefix": {
		packageName: "bytes", function: "HasPrefix", unconditional: "bytes.TrimPrefix", trim: "TrimPrefix",
	},
	"bytes.HasSuffix": {
		packageName: "bytes", function: "HasSuffix", unconditional: "bytes.TrimSuffix", trim: "TrimSuffix",
	},
	"bytes.Contains": {packageName: "bytes", function: "Contains", unconditional: "bytes.Replace"},
}

// guardNames are guards' keys, sorted, which staticcheck_code.CallIndex compares a condition's name with in place, so
// the name is never built for a condition that calls none of them.
var guardNames = slices.Sorted(maps.Keys(guards))

// Check judges one if statement against seen, which it reads and never writes, and returns its else, for the caller
// to record in seen, or nil. An if statement with no initializer and no else, that seen does not hold, whose condition
// is a call of a guard and whose body is one plain assignment to the guard's first argument, is reported when the
// assignment's right side is the guard's unconditional call on the same two arguments, or a slice of the first
// argument that cuts off exactly the second.
func Check(context *rule.Context, ifStatement *ast.IfStmt, seen *Seen) ast.Stmt {
	// An initializer turns the statement away before its else is recorded, as the original returns first, so an else
	// if after an if with an initializer is judged on its own.
	if ifStatement.Init != nil {
		return nil
	}
	// An if with an else is never reported, and an else if in the chain is not either, since the guard is then one
	// branch of several (staticcheck's issue 1447). The else is returned whether a block or another if.
	if ifStatement.Else != nil {
		return ifStatement.Else
	}
	if seen.elses[ifStatement] || len(ifStatement.Body.List) != 1 {
		return nil
	}
	// The condition is taken as written, so a parenthesized call is no call here, as the original asserts the type.
	condition, isCall := ifStatement.Cond.(*ast.CallExpr)
	if !isCall {
		return nil
	}
	guardIndex := staticcheck_code.CallIndex(context, condition, guardNames...)
	if guardIndex < 0 {
		return nil
	}
	asked := guards[guardNames[guardIndex]]
	// A guard called on another call's two results has one argument, a call, which sameNonDynamic never finds the
	// same as an assignment's left side, so the original turns it away before it reads a second argument, as this does.
	if len(condition.Args) != 2 {
		return nil
	}
	assignment, isAssignment := ifStatement.Body.List[0].(*ast.AssignStmt)
	if !isAssignment || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil
	}
	if !sameNonDynamic(context, condition.Args[0], assignment.Lhs[0]) {
		return nil
	}
	switch right := assignment.Rhs[0].(type) {
	case *ast.CallExpr:
		checkUnconditionalCall(context, ifStatement, condition, asked, right)
	case *ast.SliceExpr:
		checkManualSlice(context, ifStatement, condition, asked, right)
	}
	return nil
}

// checkUnconditionalCall reports ifStatement when call, its body's right side, is the call asked does its work with,
// on the condition's two arguments as its first two, at the whole statement. The message names the call by the
// CallName it matched, asked's unconditional, the import path's spelling.
func checkUnconditionalCall(
	context *rule.Context, ifStatement *ast.IfStmt, condition *ast.CallExpr, asked guard, call *ast.CallExpr,
) {
	if len(call.Args) < 2 || !sameNonDynamic(context, condition.Args[0], call.Args[0]) ||
		!sameNonDynamic(context, condition.Args[1], call.Args[1]) {
		return
	}
	if staticcheck_code.CallIndex(context, call, asked.unconditional) < 0 {
		return
	}
	staticcheck_report.Report(context, ifStatement,
		fmt.Sprintf("should replace this if statement with an unconditional %s", asked.unconditional),
		staticcheck_report.FilterGenerated())
}

// checkManualSlice reports ifStatement when slice, its body's right side, slices the condition's first argument to
// cut off exactly its second, at the start for HasPrefix, `s[len(p):]`, and at the end for HasSuffix,
// `s[:len(s)-len(p)]` with a low index of none or a constant zero, at the statement's header. Contains has no slice
// that stands for it.
func checkManualSlice(
	context *rule.Context, ifStatement *ast.IfStmt, condition *ast.CallExpr, asked guard, slice *ast.SliceExpr,
) {
	if slice.Slice3 || !sameNonDynamic(context, slice.X, condition.Args[0]) {
		return
	}
	switch asked.function {
	case "HasPrefix":
		// A high index is never taken, even one that is the length, which S1010 already flags.
		if slice.High != nil || !isOffset(context, slice.Low, condition.Args[1], asked) {
			return
		}
	case "HasSuffix":
		if slice.Low != nil {
			low, isInteger := staticcheck_code.ExpressionToInt(context, slice.Low)
			if !isInteger || low != 0 {
				return
			}
		}
		// The high index is taken as written, a subtraction of the offset from the first argument's length, so a
		// parenthesized one is not it, and neither is a missing one.
		high, isBinary := slice.High.(*ast.BinaryExpr)
		if !isBinary || high.Op != token.SUB || !isLengthOf(context, high.X, condition.Args[0]) ||
			!isOffset(context, high.Y, condition.Args[1], asked) {
			return
		}
	default:
		return
	}
	staticcheck_report.Report(context, ifStatement,
		fmt.Sprintf("should replace this if statement with an unconditional %s.%s", asked.packageName, asked.trim),
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated())
}

// isOffset is whether offset, a slice's index as written, is the length of affix, the condition's second argument:
// the builtin len called on it, or, for strings alone, an integer literal whose value is the byte length of affix
// when affix is itself a literal, so a constant named elsewhere is not obviously connected to the number.
func isOffset(context *rule.Context, offset ast.Expr, affix ast.Expr, asked guard) bool {
	switch offset := offset.(type) {
	case *ast.CallExpr:
		return isLengthOf(context, offset, affix)
	case *ast.BasicLit:
		if asked.packageName != "strings" {
			return false
		}
		if _, isLiteral := affix.(*ast.BasicLit); !isLiteral {
			return false
		}
		text, isString := staticcheck_code.ExpressionToString(context, affix)
		length, isInteger := staticcheck_code.ExpressionToInt(context, offset)
		return isString && isInteger && length == int64(len(text))
	default:
		return false
	}
}

// isLengthOf is whether expression, as written, calls the builtin len, by any parentheses around its name as CallName
// looks through them, on one argument that sameNonDynamic finds the same as of.
func isLengthOf(context *rule.Context, expression ast.Expr, of ast.Expr) bool {
	call, isCall := expression.(*ast.CallExpr)
	if !isCall || !staticcheck_code.IsCallTo(context, call, "len") || len(call.Args) != 1 {
		return false
	}
	return sameNonDynamic(context, call.Args[knowledge.Arg("len.v")], of)
}

// sameNonDynamic is whether first and second are the same value as the original judges it, each as written, with no
// parentheses looked through: two identifiers of the same object, by identity, or two selectors, two index
// expressions or two literals syntaxEqual finds alike, as astutil.Equal does, which compares names by spelling and
// calls by their syntax. Anything else, a call among them, is never the same as anything, so a value computed twice is
// not taken for one.
//
// It parts from the original in two places, adjudicated (#zx3fz6w). A selector or index expression that holds a call
// is not the same twice, where the original's astutil.Equal finds it so, since the call is made anew each time and
// may answer otherwise, and the unconditional form makes it a different number of times. And where astutil.Equal
// panics, on a node kind it does not know, syntaxEqual answers false, so the port declines where the original
// crashes.
func sameNonDynamic(context *rule.Context, first ast.Expr, second ast.Expr) bool {
	switch first := first.(type) {
	case *ast.Ident:
		secondIdentifier, isIdentifier := second.(*ast.Ident)
		// The objects are compared as they are, two missing ones alike, as the original compares them.
		return isIdentifier && context.TypesInfo.ObjectOf(first) == context.TypesInfo.ObjectOf(secondIdentifier)
	case *ast.SelectorExpr:
		secondSelector, isSelector := second.(*ast.SelectorExpr)
		return isSelector && syntaxEqual(first, secondSelector) && !holdsCall(context, first) &&
			!holdsCall(context, secondSelector)
	case *ast.IndexExpr:
		secondIndex, isIndex := second.(*ast.IndexExpr)
		return isIndex && syntaxEqual(first, secondIndex) && !holdsCall(context, first) &&
			!holdsCall(context, secondIndex)
	case *ast.BasicLit:
		secondLiteral, isLiteral := second.(*ast.BasicLit)
		return isLiteral && syntaxEqual(first, secondLiteral)
	default:
		return false
	}
}

// holdsCall is whether expression, at any depth, makes a call that may answer otherwise when made again: a receive,
// or a call of anything but a conversion or the builtin len or cap, which read what they are handed and change
// nothing. Both sides are asked, since syntaxEqual compares a composite literal's elements by their count alone, so
// one side may hold a call where the other does not.
func holdsCall(context *rule.Context, expression ast.Expr) bool {
	holds := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if holds {
			return false
		}
		switch node := node.(type) {
		case *ast.UnaryExpr:
			if node.Op == token.ARROW {
				holds = true
			}
		case *ast.CallExpr:
			if !context.TypesInfo.Types[node.Fun].IsType() && !staticcheck_code.IsBuiltin(context, node.Fun, "len") &&
				!staticcheck_code.IsBuiltin(context, node.Fun, "cap") {
				holds = true
			}
		}
		return !holds
	})
	return holds
}
