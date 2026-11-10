// Package testing_no_timing_assertion is the Go engine's rule that a test does not fail on how long its work took.
//
// A test whose deadline is its assertion passes on a quiet machine and fails on a loaded one, so its verdict depends
// on who else is running: cohere's tests share a pool with other agents' tests, and a busy pool makes every
// measured duration longer. Load only ever makes the work slower, so the rule reads which way a comparison fails
// and flags the one a busy machine can trip, the "took too long" check, and leaves a "returned too early" check
// alone. When the duration is what is under test, a comment above the check says why it holds under load.
package testing_no_timing_assertion

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-no-timing-assertion"

// listener reads each _test.go file's comments as the syntax dispatcher's walk enters it, and hears every function
// declaration in it.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{marker.Analyzer},
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.FuncDecl)(nil)},
	Node:      judgeFunction,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "testing_no_timing_assertion", "Flags, in a test in a _test.go file, an "+
	"if whose condition fails the test when a duration measured with time.Since, or Time.Sub with a time.Now reading "+
	"on one side, directly or through a local assigned once, comes out too long, and whose body calls a *testing.T "+
	"failure method directly, unless a // Timing assertion: <reason> comment directly above the if says why it holds "+
	"under load. A busy machine makes such a test fail by chance. Date math between times not read from the clock, "+
	"context deadlines and benchmarks are not judged.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var timingAssertionMessage = policy.MessageOf(Name, "timingAssertion")

var timingAssertionWithoutReasonMessage = policy.MessageOf(Name, "timingAssertionWithoutReason")

// failureMethods are the *testing.T methods that fail the test. Skip and Log end or annotate a test without
// failing it, so a check that only reaches them asserts nothing.
var failureMethods = map[string]bool{
	"Error":   true,
	"Errorf":  true,
	"Fatal":   true,
	"Fatalf":  true,
	"Fail":    true,
	"FailNow": true,
}

// packageState is what the rule reads once for its package and once for each file, held in its context's state.
type packageState struct {
	// indexes is the package's comment indexes, read as the walk enters its first file.
	indexes *marker.Indexes
	// comments is the comment index of the file being walked, or nil when it is no _test.go file, the only kind
	// whose tests are judged.
	comments *marker.Index
}

// enterFile reads the package's comment indexes the first time, and file's comment index when it is a _test.go file.
func enterFile(context *rule.Context, file *ast.File) {
	state := rule.State[packageState](context)
	if state.indexes == nil {
		indexes, isIndexes := context.ResultOf(marker.Analyzer).(*marker.Indexes)
		if !isIndexes {
			// Invariant: marker's result is always its Indexes, so another is a mistake in the engine, and the
			// dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
			panic(fmt.Sprintf("reading the comment indexes: marker gave %T", context.ResultOf(marker.Analyzer)))
		}
		state.indexes = indexes
	}
	state.comments = nil
	// The file's own name, not one a //line directive gives it, since only a _test.go file holds tests.
	if strings.HasSuffix(context.FileSet.PositionFor(file.Package, false).Filename, "_test.go") {
		state.comments = state.indexes.ForFile(file)
	}
}

// judgeFunction judges node when it is a test in a _test.go file.
func judgeFunction(context *rule.Context, node ast.Node, _ []ast.Node) {
	state := rule.State[packageState](context)
	function, isFunction := node.(*ast.FuncDecl)
	if state.comments == nil || !isFunction || !isTest(context, function) {
		return
	}
	judgeTest(context, function.Body, state.comments)
}

// clockLocals is what one test's body holds of the clock, each a variable declared once and never written again:
// readings hold what time.Now returned, and measured hold a duration measured from the clock.
type clockLocals struct {
	readings map[*types.Var]bool
	measured map[*types.Var]bool
}

// judgeTest reports every timing assertion in one test's body, its subtests' function literals included, since a
// subtest's deadline is as much at the pool's mercy as its parent's.
func judgeTest(context *rule.Context, body *ast.BlockStmt, comments *marker.Index) {
	written := writtenVariables(context, body)
	// The readings are found first, since a Sub measures the clock only when one of its sides is a reading.
	readings := localsDeclaredWith(context, body, written, func(value ast.Expr) bool {
		return isClockReading(context, value, nil)
	})
	clock := clockLocals{
		readings: readings,
		measured: localsDeclaredWith(context, body, written, func(value ast.Expr) bool {
			return isMeasurement(context, value, readings)
		}),
	}
	ast.Inspect(body, func(node ast.Node) bool {
		statement, isIf := node.(*ast.IfStmt)
		if !isIf || !failsTestDirectly(context, statement.Body) {
			return true
		}
		comparisons := slowFailingComparisons(context, statement.Cond, true, clock)
		if len(comparisons) == 0 {
			return true
		}
		// Only the comment directly above the if, on lines of its own, speaks for it. One trailing code above the if,
		// `work() // Timing assertion: ...`, speaks for that code.
		given := marker.Read(comments.Above(statement), marker.TimingAssertion)
		for _, comparison := range comparisons {
			report(context, comparison, given)
		}
		return true
	})
}

// localsDeclaredWith is every variable body declares with a value holds accepts, `elapsed := time.Since(start)` or
// `var finished = time.Now()`, that nothing writes again, so reading the variable reads that value.
func localsDeclaredWith(
	context *rule.Context,
	body *ast.BlockStmt,
	written map[*types.Var]bool,
	holds func(value ast.Expr) bool,
) map[*types.Var]bool {
	declared := map[*types.Var]bool{}
	declare := func(names []*ast.Ident, values []ast.Expr) {
		// Only one value per name pairs them; `first, second := pair()` assigns from one call's results.
		if len(names) != len(values) {
			return
		}
		for index, name := range names {
			variable, isVariable := context.TypesInfo.Defs[name].(*types.Var)
			if isVariable && !written[variable] && holds(values[index]) {
				declared[variable] = true
			}
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.AssignStmt:
			if declaration.Tok != token.DEFINE {
				return true
			}
			names := make([]*ast.Ident, 0, len(declaration.Lhs))
			for _, left := range declaration.Lhs {
				// A define's left side is identifiers only; the compiler refuses anything else.
				if name, isIdentifier := left.(*ast.Ident); isIdentifier {
					names = append(names, name)
				}
			}
			declare(names, declaration.Rhs)
		case *ast.ValueSpec:
			declare(declaration.Names, declaration.Values)
		}
		return true
	})
	return declared
}

// writtenVariables is every variable body writes after declaring it: assigned, redeclared by a define, stepped,
// ranged into with `=`, or given away by its address. Such a variable may hold something other than the measurement
// it was declared with when the comparison reads it.
func writtenVariables(context *rule.Context, body *ast.BlockStmt) map[*types.Var]bool {
	written := map[*types.Var]bool{}
	mark := func(expression ast.Expr) {
		// A declaration's own name is in Defs, never Uses, so only a later write is marked. A define that redeclares
		// a variable records it in Uses, which marks it too.
		if name, isIdentifier := ast.Unparen(expression).(*ast.Ident); isIdentifier {
			if variable, isVariable := context.TypesInfo.Uses[name].(*types.Var); isVariable {
				written[variable] = true
			}
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch write := node.(type) {
		case *ast.AssignStmt:
			for _, left := range write.Lhs {
				mark(left)
			}
		case *ast.IncDecStmt:
			mark(write.X)
		case *ast.RangeStmt:
			if write.Tok == token.ASSIGN {
				mark(write.Key)
				mark(write.Value)
			}
		case *ast.UnaryExpr:
			if write.Op == token.AND {
				mark(write.X)
			}
		}
		return true
	})
	return written
}

// isMeasurement is whether expression measures the clock: a call to time.Since, or to time.Time's Sub with a reading
// of the clock on one side, found by the function object, so a Since or Sub of any other package or type is never
// mistaken for one. A Sub between two times that were not read from the clock, `expiry.Sub(issued)`, is date math,
// whose result a busy machine cannot change.
func isMeasurement(context *rule.Context, expression ast.Expr, readings map[*types.Var]bool) bool {
	call, isCall := ast.Unparen(expression).(*ast.CallExpr)
	if !isCall {
		return false
	}
	function, isFunction := typeutil.Callee(context.TypesInfo, call).(*types.Func)
	if !isFunction || function.Pkg() == nil || function.Pkg().Path() != "time" {
		return false
	}
	receiver := function.Signature().Recv()
	switch function.Name() {
	case "Since":
		return receiver == nil
	case "Sub":
		if receiver == nil || !isNamed(receiver.Type(), "time", "Time") {
			return false
		}
		// A method value's receiver is the selector's left side; a method expression, time.Time.Sub(finished,
		// start), passes it as the first argument, which the arguments already hold.
		operands := call.Args
		if selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSelector {
			if selection, isSelection := context.TypesInfo.Selections[selector]; isSelection &&
				selection.Kind() == types.MethodVal {
				operands = append([]ast.Expr{selector.X}, call.Args...)
			}
		}
		for _, operand := range operands {
			if isClockReading(context, operand, readings) {
				return true
			}
		}
	}
	return false
}

// isClockReading is whether expression is what time.Now returned: the call itself, found by the function object, or
// a local in readings holding one.
func isClockReading(context *rule.Context, expression ast.Expr, readings map[*types.Var]bool) bool {
	switch value := ast.Unparen(expression).(type) {
	case *ast.Ident:
		variable, isVariable := context.TypesInfo.Uses[value].(*types.Var)
		return isVariable && readings[variable]
	case *ast.CallExpr:
		function, isFunction := typeutil.Callee(context.TypesInfo, value).(*types.Func)
		return isFunction && function.Pkg() != nil && function.Pkg().Path() == "time" && function.Name() == "Now" &&
			function.Signature().Recv() == nil
	}
	return false
}

// isMeasured is whether operand is a measured duration: the measuring call itself, or a local holding one.
func isMeasured(context *rule.Context, operand ast.Expr, clock clockLocals) bool {
	if name, isIdentifier := ast.Unparen(operand).(*ast.Ident); isIdentifier {
		variable, isVariable := context.TypesInfo.Uses[name].(*types.Var)
		return isVariable && clock.measured[variable]
	}
	return isMeasurement(context, operand, clock.readings)
}

// slowFailingComparisons is every comparison in condition that makes it true when a measured duration is long, so
// the if's body runs, and the test fails, on a busy machine. positive is whether condition being true runs the
// body: a ! turns it over, and && and || keep it, since either side of them being true moves the whole the same
// way. A comparison inside a call's argument or anything else is not read, since what it decides is out of sight.
func slowFailingComparisons(
	context *rule.Context,
	condition ast.Expr,
	positive bool,
	clock clockLocals,
) []*ast.BinaryExpr {
	switch expression := ast.Unparen(condition).(type) {
	case *ast.UnaryExpr:
		if expression.Op == token.NOT {
			return slowFailingComparisons(context, expression.X, !positive, clock)
		}
	case *ast.BinaryExpr:
		switch expression.Op {
		case token.LAND, token.LOR:
			return append(
				slowFailingComparisons(context, expression.X, positive, clock),
				slowFailingComparisons(context, expression.Y, positive, clock)...,
			)
		case token.LSS, token.GTR, token.LEQ, token.GEQ, token.EQL, token.NEQ:
			leftFails := isMeasured(context, expression.X, clock) && failsWhenLong(expression.Op, true, positive)
			rightFails := isMeasured(context, expression.Y, clock) && failsWhenLong(expression.Op, false, positive)
			if leftFails || rightFails {
				return []*ast.BinaryExpr{expression}
			}
		}
	}
	return nil
}

// mirroredComparison is each comparison with its operands swapped, `limit < elapsed` read as `elapsed > limit`.
var mirroredComparison = map[token.Token]token.Token{
	token.LSS: token.GTR, token.GTR: token.LSS, token.LEQ: token.GEQ, token.GEQ: token.LEQ,
	token.EQL: token.EQL, token.NEQ: token.NEQ,
}

// negatedComparison is each comparison under a !, `!(elapsed <= limit)` read as `elapsed > limit`.
var negatedComparison = map[token.Token]token.Token{
	token.LSS: token.GEQ, token.GEQ: token.LSS, token.GTR: token.LEQ, token.LEQ: token.GTR,
	token.EQL: token.NEQ, token.NEQ: token.EQL,
}

// failsWhenLong is whether a comparison by operator, with the measured duration on the left or the right, runs the
// if's body as the duration grows. The comparison is first read with the duration on the left, then turned over when
// the condition is negated, and what remains fails a slow run when it is >, >= or !=: `elapsed > limit` fails a slow
// run, and `elapsed < delay`, a check that the work did not return early, fails only a fast one, which load cannot
// cause. An == fails only the one exact duration, which a slower run moves away from.
func failsWhenLong(operator token.Token, measuredOnLeft bool, positive bool) bool {
	effective := operator
	if !measuredOnLeft {
		effective = mirroredComparison[effective]
	}
	if !positive {
		effective = negatedComparison[effective]
	}
	return effective == token.GTR || effective == token.GEQ || effective == token.NEQ
}

// failsTestDirectly is whether body calls a failure method on a *testing.T, in it or in a block nested in it. A
// function literal there runs whenever it is called, and a helper handed the test is out of sight, so neither counts.
func failsTestDirectly(context *rule.Context, body *ast.BlockStmt) bool {
	fails := false
	ast.Inspect(body, func(node ast.Node) bool {
		if fails {
			return false
		}
		if _, isLiteral := node.(*ast.FuncLit); isLiteral {
			return false
		}
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		// A method value only, read by the type it is called on: Error and its siblings are declared on testing's
		// unexported common and promoted to *testing.T, so the receiver is what says it is a test's.
		selection, isSelection := context.TypesInfo.Selections[selector]
		if !isSelection || selection.Kind() != types.MethodVal || !isPointerToTestingT(selection.Recv()) {
			return true
		}
		method := selection.Obj()
		fails = method.Pkg() != nil && method.Pkg().Path() == "testing" && failureMethods[method.Name()]
		return !fails
	})
	return fails
}

// isTest is whether function is what go test runs as a test: named Test, or Test followed by anything but a
// lower-case letter, with no receiver, no type parameters, no results, and one *testing.T parameter. Benchmarks,
// fuzz targets, examples and TestMain take something else or are named otherwise, so none of them is judged.
func isTest(context *rule.Context, function *ast.FuncDecl) bool {
	if function.Recv != nil || function.Body == nil || !isTestName(function.Name.Name) {
		return false
	}
	object, isFunction := context.TypesInfo.Defs[function.Name].(*types.Func)
	if !isFunction {
		return false
	}
	signature := object.Signature()
	if signature.TypeParams().Len() > 0 || signature.Results().Len() > 0 || signature.Params().Len() != 1 {
		return false
	}
	return isPointerToTestingT(signature.Params().At(0).Type())
}

// isTestName is go test's own reading of a name, so Testify is a helper and Test_parse a test.
func isTestName(name string) bool {
	rest, found := strings.CutPrefix(name, "Test")
	if !found {
		return false
	}
	if rest == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(first)
}

func isPointerToTestingT(candidate types.Type) bool {
	pointer, isPointer := types.Unalias(candidate).(*types.Pointer)
	return isPointer && isNamed(pointer.Elem(), "testing", "T")
}

func isNamed(candidate types.Type, packagePath string, name string) bool {
	named, isNamedType := types.Unalias(candidate).(*types.Named)
	if !isNamedType {
		return false
	}
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == packagePath && named.Obj().Name() == name
}

// report flags comparison unless its excuse gives a reason, with the message that names what is missing.
func report(context *rule.Context, comparison *ast.BinaryExpr, given marker.Excuse) {
	message := timingAssertionMessage
	switch given {
	case marker.WithReason:
		return
	case marker.WithoutReason:
		message = timingAssertionWithoutReasonMessage
	case marker.None:
	}
	context.Report(analysis.Diagnostic{
		Pos:      comparison.Pos(),
		End:      comparison.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{}),
	})
}
