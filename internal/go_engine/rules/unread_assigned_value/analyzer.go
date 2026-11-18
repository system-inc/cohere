// Package unread_assigned_value is staticcheck's SA4006, a value assigned to a variable, or produced by an increment
// or decrement, that nothing ever reads, ported onto the engine's own intermediate representation, reporting through
// the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4006/sa4006.go, whose logic it keeps and
// whose message it reports word for word, over the whole statement, through staticcheck_report.Report with no option,
// where the original reports through report.Report. The original walks every source function of a package in one
// run, but keeps nothing from one function to the next: it walks each function's syntax, nested function literals
// included, and asks that function's representation for the value of each assignment, its own switch tags and its
// phis. So Check judges one function at a time and finds what the original finds. It walks no nested literal, where
// the enclosing function's representation holds no value and the literal's own function is judged in its turn. The
// message writes the assigned expression as the original's fmt verb writes an ast.Expr, a name for an identifier.
//
// It asks which tool generated a file, to leave alone what goyacc writes, and rule.Context carries only whether a file
// was generated, so its listener requires staticcheck's generated pass and reads the pass's map of generators through
// the context, keyed by a file's name as parsed, as code.Generator reads it, handing Check the lookup for every
// function.
//
// The registry runs the port in the original's place, under its name, and the original stays only as the oracle: the
// parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package unread_assigned_value

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher over the engine's own intermediate
// representation: every function, each judged on its own with the generator lookup of the package. It requires
// staticcheck's generated pass, whose map of generators it reads. Check reads the value of each assignment, increment
// and switch tag from the function's expression map, so the listener says so, and the package records that map
// wherever the rule runs.
var Listener = rule.Listener{
	FileLocal: true,
	Requires:  []*analysis.Analyzer{generated.Analyzer},
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{
		Function: func(context *rule.Context, function *intermediate_representation.Function) {
			Check(context, function, generatorLookup(context))
		},
		ReadsValueForExpression: true,
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4006",
	"SA4006",
	"Flags a value assigned to a variable that is never read: staticcheck's SA4006, ported onto the engine's own "+
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
		generator, isGenerated := generators[context.FileSet.PositionFor(position, false).Filename]
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
	function *intermediate_representation.Function,
	generatorOf func(position token.Pos) (generated.Generator, bool),
) {
	if intermediate_representation.IsExample(function) {
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
	// One walker, the one allocation Check makes for a clean function without a tagged switch, where a map of tags, a
	// closure for each of two walks and one for the use test cost four. No finding can change: ast.Inspect is ast.Walk
	// with a visitor that returns itself, so the walk visits the nodes in the order it did, and the slice of tags holds
	// what the map did but the nil of a switch with no tag, which hasUse is never asked about.
	//
	// The original gathers every tag in one walk and judges in a second, since a tag can follow the assignment it
	// reads. This walk judges each value against the tags gathered so far and keeps it when none of them reads it.
	// More tags can only find a read, never lose one: hasUse follows the same referrers with more tags until it meets
	// one of the tags, which is a read. So every value the original reports is kept, in the order it reports them,
	// and each is judged again here against every tag, as the original judges it.
	walker := &functionWalker{function: function}
	ast.Walk(walker, body)
	for _, unread := range walker.unread {
		if !hasUse(unread.value, walker.switchTags) {
			staticcheck_report.Report(context, unread.statement, fmt.Sprintf(messageFormat, unread.assigned))
		}
	}
}

// checkIncrement judges an increment or decrement by whether anything reads its result.
func checkIncrement(walker *functionWalker, statement *ast.IncDecStmt) {
	value, _ := walker.function.ValueForExpression(statement.X)
	if value == nil {
		return
	}
	if _, isConstant := value.(*intermediate_representation.Constant); isConstant {
		// A zero-valued constant, for example in `values := []string(nil)`.
		return
	}
	walker.judge(statement, statement.X, value)
}

// checkAssignment judges each value an assignment gives a named variable, not the blank identifier, by whether
// anything reads it.
func checkAssignment(walker *functionWalker, statement *ast.AssignStmt) {
	if len(statement.Lhs) > 1 && len(statement.Rhs) == 1 {
		// A call of several results, or a comma-ok form: each variable takes one extraction of the one value.
		value, _ := walker.function.ValueForExpression(statement.Rhs[0])
		if value == nil {
			return
		}
		referrers := value.Referrers()
		if referrers == nil {
			return
		}
		for _, referrer := range *referrers {
			extraction, isExtraction := referrer.(*intermediate_representation.Extract)
			if !isExtraction || !isNamedVariable(statement.Lhs[extraction.Index]) {
				continue
			}
			walker.judge(statement, statement.Lhs[extraction.Index], extraction)
		}
		return
	}
	for index, assigned := range statement.Lhs {
		if !isNamedVariable(assigned) {
			continue
		}
		value, _ := walker.function.ValueForExpression(statement.Rhs[index])
		if value == nil {
			// An assignment that operates, `+=` and the like, holds its value at the variable it assigns.
			if statement.Tok != token.ASSIGN {
				value, _ = walker.function.ValueForExpression(assigned)
			}
			if value == nil {
				continue
			}
		}
		if _, isConstant := value.(*intermediate_representation.Constant); isConstant {
			// A zero-valued constant, for example in `values := []string(nil)`.
			continue
		}
		walker.judge(statement, assigned, value)
	}
}

// isNamedVariable is whether assigned is an identifier other than the blank one.
func isNamedVariable(assigned ast.Expr) bool {
	identifier, isIdentifier := assigned.(*ast.Ident)
	return isIdentifier && identifier.Name != "_"
}

// hasUse is whether anything reads value: a switch whose tag it is, or any referrer but a phi, or a phi something
// reads in turn, each phi followed once. A value whose referrers the representation does not track counts as read.
func hasUse(value intermediate_representation.Value, switchTags []intermediate_representation.Value) bool {
	// One set of what the walk has followed from, shared by the whole walk as the original's is, so a loop of phis
	// that reach each other ends.
	seen := map[intermediate_representation.Value]bool{}
	var walk func(value intermediate_representation.Value) bool
	walk = func(value intermediate_representation.Value) bool {
		if seen[value] {
			return false
		}
		if slices.Contains(switchTags, value) {
			return true
		}
		referrers := value.Referrers()
		if referrers == nil {
			return true
		}
		for _, referrer := range *referrers {
			phi, isPhi := referrer.(*intermediate_representation.Phi)
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
