// Package unread_assigned_value is staticcheck's SA4006, a value assigned to a variable, or produced by an increment
// or decrement, that nothing ever reads, ported onto staticcheck's intermediate representation, reporting through the
// kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4006/sa4006.go, whose logic it keeps and
// whose message it reports word for word, over the whole statement, through staticcheck_report.Report with no option,
// where the original reports through report.Report. The original walks every source function of a package in one
// run, but keeps nothing from one function to the next: it walks each function's syntax, nested function literals
// included, and asks that function's representation for the value of each assignment, its own switch tags and its
// phis. So Check judges one function at a time and finds what the original finds. The message writes the assigned
// expression as the original's fmt verb writes an ast.Expr, a name for an identifier.
//
// It asks which tool generated a file, to leave alone what goyacc writes, and rule.Context carries only whether a file
// was generated, so its listener requires staticcheck's generated pass and reads the pass's map of generators through
// the context, keyed by a file's name as parsed, as code.Generator reads it, handing Check the lookup for every
// function.
//
// The original stays the oracle: the parity tests beside this file run both over the same packages, and the two differ
// nowhere they hold.
package unread_assigned_value

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own with
// the generator lookup of the package. It requires staticcheck's generated pass, whose map of generators it reads.
var Listener = rule.Listener{
	Requires: []*analysis.Analyzer{generated.Analyzer},
	Function: func(context *rule.Context, function *ir.Function) {
		Check(context, function, generatorLookup(context))
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4006",
	"SA4006",
	"Flags a value assigned to a variable that is never read: staticcheck's SA4006, ported onto its "+
		"intermediate representation.",
	&Listener,
)

// generatorLookup is the tool that generated the file holding a position, and whether one did, read from the
// generated pass's map, which keys a file by its name as parsed, never as a `//line` directive renames it.
func generatorLookup(context *rule.Context) func(position token.Pos) (generated.Generator, bool) {
	generators, isGenerators := context.ResultOf(generated.Analyzer).(map[string]generated.Generator)
	if !isGenerators {
		// Invariant: the listener requires the generated pass, and the dispatcher hands over its result, of the type
		// it declares.
		panic("unread_assigned_value: the generated pass's result is missing")
	}
	return func(position token.Pos) (generated.Generator, bool) {
		generator, isGenerated := generators[context.Fset.PositionFor(position, false).Filename]
		return generator, isGenerated
	}
}

// messageFormat is the original's message, word for word, around the expression assigned to.
const messageFormat = "this value of %s is never used"

// Check judges one function: each assignment and each increment or decrement in its syntax whose value, as the
// function's representation holds it, nothing reads but phis that nothing reads either. An example function, and a
// function in a file goyacc generated, is left alone.
func Check(
	context *rule.Context,
	function *ir.Function,
	generatorOf func(position token.Pos) (generated.Generator, bool),
) {
	if irutil.IsExample(function) {
		return
	}
	body := function.Source()
	if body == nil {
		return
	}
	if generator, isGenerated := generatorOf(body.Pos()); isGenerated && generator == generated.Goyacc {
		// goyacc's state machine assigns hundreds of values it never reads.
		return
	}
	// A switch's tag is read by its cases, which the representation compares against it without a referrer the
	// walk below would see. A switch with no tag adds the nil value, which no value asked about is.
	switchTags := map[ir.Value]bool{}
	ast.Inspect(body, func(node ast.Node) bool {
		if statement, isSwitch := node.(*ast.SwitchStmt); isSwitch {
			tag, _ := function.ValueForExpr(statement.Tag)
			switchTags[tag] = true
		}
		return true
	})
	isUsed := func(value ir.Value) bool {
		return hasUse(value, switchTags)
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.IncDecStmt:
			checkIncrement(context, function, statement, isUsed)
		case *ast.AssignStmt:
			checkAssignment(context, function, statement, isUsed)
		}
		return true
	})
}

// checkIncrement reports an increment or decrement whose result nothing reads.
func checkIncrement(
	context *rule.Context,
	function *ir.Function,
	statement *ast.IncDecStmt,
	isUsed func(value ir.Value) bool,
) {
	value, _ := function.ValueForExpr(statement.X)
	if value == nil {
		return
	}
	if _, isConstant := value.(*ir.Const); isConstant {
		// A zero-valued constant, for example in `values := []string(nil)`.
		return
	}
	if !isUsed(value) {
		staticcheck_report.Report(context, statement, fmt.Sprintf(messageFormat, statement.X))
	}
}

// checkAssignment reports each value an assignment gives a named variable, not the blank identifier, that nothing
// reads.
func checkAssignment(
	context *rule.Context,
	function *ir.Function,
	statement *ast.AssignStmt,
	isUsed func(value ir.Value) bool,
) {
	if len(statement.Lhs) > 1 && len(statement.Rhs) == 1 {
		// A call of several results, or a comma-ok form: each variable takes one extraction of the one value.
		value, _ := function.ValueForExpr(statement.Rhs[0])
		if value == nil {
			return
		}
		referrers := value.Referrers()
		if referrers == nil {
			return
		}
		for _, referrer := range *referrers {
			extraction, isExtraction := referrer.(*ir.Extract)
			if !isExtraction || isUsed(extraction) {
				continue
			}
			assigned := statement.Lhs[extraction.Index]
			if !isNamedVariable(assigned) {
				continue
			}
			staticcheck_report.Report(context, statement, fmt.Sprintf(messageFormat, assigned))
		}
		return
	}
	for index, assigned := range statement.Lhs {
		if !isNamedVariable(assigned) {
			continue
		}
		value, _ := function.ValueForExpr(statement.Rhs[index])
		if value == nil {
			// An assignment that operates, `+=` and the like, holds its value at the variable it assigns.
			if statement.Tok != token.ASSIGN {
				value, _ = function.ValueForExpr(assigned)
			}
			if value == nil {
				continue
			}
		}
		if _, isConstant := value.(*ir.Const); isConstant {
			// A zero-valued constant, for example in `values := []string(nil)`.
			continue
		}
		if !isUsed(value) {
			staticcheck_report.Report(context, statement, fmt.Sprintf(messageFormat, assigned))
		}
	}
}

// isNamedVariable is whether assigned is an identifier other than the blank one.
func isNamedVariable(assigned ast.Expr) bool {
	identifier, isIdentifier := assigned.(*ast.Ident)
	return isIdentifier && identifier.Name != "_"
}

// hasUse is whether anything reads value: a switch whose tag it is, or any referrer but a phi, or a phi something
// reads in turn, each phi followed once. A value whose referrers the representation does not track counts as read.
func hasUse(value ir.Value, switchTags map[ir.Value]bool) bool {
	// One set of what the walk has followed from, shared by the whole walk as the original's is, so a loop of phis
	// that reach each other ends.
	seen := map[ir.Value]bool{}
	var walk func(value ir.Value) bool
	walk = func(value ir.Value) bool {
		if seen[value] {
			return false
		}
		if switchTags[value] {
			return true
		}
		referrers := value.Referrers()
		if referrers == nil {
			return true
		}
		for _, referrer := range *referrers {
			phi, isPhi := referrer.(*ir.Phi)
			if !isPhi {
				return true
			}
			seen[value] = true
			if walk(phi) {
				return true
			}
		}
		return false
	}
	return walk(value)
}
