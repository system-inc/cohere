// Package testing_no_timing_assertion is the Go engine's rule that a test does not fail on how long its work took.
//
// A test whose deadline is its assertion passes on a quiet machine and fails on a loaded one, so its verdict depends
// on who else is running: cohere's tests share a pool with other agents' tests, and a busy pool makes every
// measured duration longer. Load only ever makes the work slower, so the rule reads which way a comparison fails
// and flags the one a busy machine can trip, the "took too long" check, and leaves a "returned too early" check
// alone. When the duration is what is under test, a comment above the check says why it holds under load.
package testing_no_timing_assertion

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-no-timing-assertion"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "testing_no_timing_assertion",
	Doc: "Flags, in a test in a _test.go file, an if whose condition fails the test when a duration measured with " +
		"time.Since or Time.Sub, directly or through a local assigned once, comes out too long, and whose body " +
		"calls a *testing.T failure method directly, unless a // Timing assertion: <reason> comment directly above " +
		"the if says why it holds under load. A busy machine makes such a test fail by chance. Context deadlines " +
		"and benchmarks are not judged.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var timingAssertionMessage = policy.MessageOf(Name, "timingAssertion")

var timingAssertionWithoutReasonMessage = policy.MessageOf(Name, "timingAssertionWithoutReason")

// timingAssertionMarker opens the comment that excuses a timing assertion, matched as written.
const timingAssertionMarker = "Timing assertion:"

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

// excuse is what the comment above an if says about why its deadline holds.
type excuse int

const (
	excuseNone excuse = iota
	// excuseWithoutReason is a marker with nothing after its colon, which tells the next reader nothing.
	excuseWithoutReason
	excuseWithReason
)

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// The file's own name, not one a //line directive gives it, since only a _test.go file holds tests.
		if !strings.HasSuffix(pass.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
			continue
		}
		commentAbove := commentsOnTheirOwnLines(pass, file)
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || !isTest(pass, function) {
				continue
			}
			judgeTest(pass, function.Body, commentAbove)
		}
	}
	return nil, nil
}

// judgeTest reports every timing assertion in one test's body, its subtests' function literals included, since a
// subtest's deadline is as much at the pool's mercy as its parent's.
func judgeTest(pass *analysis.Pass, body *ast.BlockStmt, commentAbove map[int]*ast.CommentGroup) {
	measured := measuredLocals(pass, body)
	ast.Inspect(body, func(node ast.Node) bool {
		statement, isIf := node.(*ast.IfStmt)
		if !isIf || !failsTestDirectly(pass, statement.Body) {
			return true
		}
		comparisons := slowFailingComparisons(pass, statement.Cond, true, measured)
		if len(comparisons) == 0 {
			return true
		}
		given := excuseOf(commentAbove[pass.Fset.PositionFor(statement.Pos(), false).Line-1])
		for _, comparison := range comparisons {
			report(pass, comparison, given)
		}
		return true
	})
}

// measuredLocals is every variable body declares with a measured duration as its value, `elapsed :=
// time.Since(start)` or `var elapsed = end.Sub(start)`, that nothing writes again, so reading it reads the
// measurement.
func measuredLocals(pass *analysis.Pass, body *ast.BlockStmt) map[*types.Var]bool {
	written := writtenVariables(pass, body)
	measured := map[*types.Var]bool{}
	declare := func(names []*ast.Ident, values []ast.Expr) {
		// Only one value per name pairs them; `first, second := pair()` assigns from one call's results.
		if len(names) != len(values) {
			return
		}
		for index, name := range names {
			variable, isVariable := pass.TypesInfo.Defs[name].(*types.Var)
			if isVariable && !written[variable] && isMeasurement(pass, values[index]) {
				measured[variable] = true
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
	return measured
}

// writtenVariables is every variable body writes after declaring it: assigned, redeclared by a define, stepped,
// ranged into with `=`, or given away by its address. Such a variable may hold something other than the measurement
// it was declared with when the comparison reads it.
func writtenVariables(pass *analysis.Pass, body *ast.BlockStmt) map[*types.Var]bool {
	written := map[*types.Var]bool{}
	mark := func(expression ast.Expr) {
		// A declaration's own name is in Defs, never Uses, so only a later write is marked. A define that redeclares
		// a variable records it in Uses, which marks it too.
		if name, isIdentifier := ast.Unparen(expression).(*ast.Ident); isIdentifier {
			if variable, isVariable := pass.TypesInfo.Uses[name].(*types.Var); isVariable {
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

// isMeasurement is whether expression is a call to time.Since or to time.Time's Sub, found by the function object,
// so a Since or Sub of any other package or type is never mistaken for one.
func isMeasurement(pass *analysis.Pass, expression ast.Expr) bool {
	call, isCall := ast.Unparen(expression).(*ast.CallExpr)
	if !isCall {
		return false
	}
	function, isFunc := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !isFunc || function.Pkg() == nil || function.Pkg().Path() != "time" {
		return false
	}
	receiver := function.Signature().Recv()
	switch function.Name() {
	case "Since":
		return receiver == nil
	case "Sub":
		return receiver != nil && isNamed(receiver.Type(), "time", "Time")
	}
	return false
}

// isMeasured is whether operand is a measured duration: the measuring call itself, or a local holding one.
func isMeasured(pass *analysis.Pass, operand ast.Expr, measured map[*types.Var]bool) bool {
	if name, isIdentifier := ast.Unparen(operand).(*ast.Ident); isIdentifier {
		variable, isVariable := pass.TypesInfo.Uses[name].(*types.Var)
		return isVariable && measured[variable]
	}
	return isMeasurement(pass, operand)
}

// slowFailingComparisons is every comparison in condition that makes it true when a measured duration is long, so
// the if's body runs, and the test fails, on a busy machine. positive is whether condition being true runs the
// body: a ! turns it over, and && and || keep it, since either side of them being true moves the whole the same
// way. A comparison inside a call's argument or anything else is not read, since what it decides is out of sight.
func slowFailingComparisons(
	pass *analysis.Pass,
	condition ast.Expr,
	positive bool,
	measured map[*types.Var]bool,
) []*ast.BinaryExpr {
	switch expression := ast.Unparen(condition).(type) {
	case *ast.UnaryExpr:
		if expression.Op == token.NOT {
			return slowFailingComparisons(pass, expression.X, !positive, measured)
		}
	case *ast.BinaryExpr:
		switch expression.Op {
		case token.LAND, token.LOR:
			return append(
				slowFailingComparisons(pass, expression.X, positive, measured),
				slowFailingComparisons(pass, expression.Y, positive, measured)...,
			)
		case token.LSS, token.GTR, token.LEQ, token.GEQ, token.EQL, token.NEQ:
			leftFails := isMeasured(pass, expression.X, measured) && failsWhenLong(expression.Op, true, positive)
			rightFails := isMeasured(pass, expression.Y, measured) && failsWhenLong(expression.Op, false, positive)
			if leftFails || rightFails {
				return []*ast.BinaryExpr{expression}
			}
		}
	}
	return nil
}

// failsWhenLong is whether a comparison by operator, with the measured duration on the left or the right, runs the
// if's body as the duration grows. The comparison is first read with the duration on the left, then turned over when
// the condition is negated, and what remains fails a slow run when it is >, >= or !=: `elapsed > limit` fails a slow
// run, and `elapsed < delay`, a check that the work did not return early, fails only a fast one, which load cannot
// cause. An == fails only the one exact duration, which a slower run moves away from.
func failsWhenLong(operator token.Token, measuredOnLeft bool, positive bool) bool {
	if !measuredOnLeft {
		operator = mirroredComparison[operator]
	}
	if !positive {
		operator = negatedComparison[operator]
	}
	return operator == token.GTR || operator == token.GEQ || operator == token.NEQ
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

// failsTestDirectly is whether body calls a failure method on a *testing.T, in it or in a block nested in it. A
// function literal there runs whenever it is called, and a helper handed the test is out of sight, so neither counts.
func failsTestDirectly(pass *analysis.Pass, body *ast.BlockStmt) bool {
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
		selection, isSelection := pass.TypesInfo.Selections[selector]
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
func isTest(pass *analysis.Pass, function *ast.FuncDecl) bool {
	if function.Recv != nil || function.Body == nil || !isTestName(function.Name.Name) {
		return false
	}
	object, isFunc := pass.TypesInfo.Defs[function.Name].(*types.Func)
	if !isFunc {
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

// commentsOnTheirOwnLines is file's comment groups that have their lines to themselves, by the line each ends on, so
// the group ending on the line above an if is the comment directly above it. A group that trails code, `work() //
// Timing assertion: ...`, speaks for that code, and one that code follows on its last line speaks for what follows,
// so neither is found. Code is found by where the file's nodes begin and end, read from the AST.
func commentsOnTheirOwnLines(pass *analysis.Pass, file *ast.File) map[int]*ast.CommentGroup {
	firstCode := map[int]token.Pos{}
	lastCode := map[int]token.Pos{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node.(type) {
		case nil:
			return true
		case *ast.Comment, *ast.CommentGroup:
			return false
		}
		for _, position := range []token.Pos{node.Pos(), node.End()} {
			line := pass.Fset.PositionFor(position, false).Line
			if earliest, seen := firstCode[line]; !seen || position < earliest {
				firstCode[line] = position
			}
			if latest, seen := lastCode[line]; !seen || position > latest {
				lastCode[line] = position
			}
		}
		return true
	})
	ending := map[int]*ast.CommentGroup{}
	for _, group := range file.Comments {
		startLine := pass.Fset.PositionFor(group.Pos(), false).Line
		endLine := pass.Fset.PositionFor(group.End(), false).Line
		// A node ending where the comment starts, `work()// note`, is code before it as much as one ending earlier.
		if earliest, seen := firstCode[startLine]; seen && earliest <= group.Pos() {
			continue
		}
		if latest, seen := lastCode[endLine]; seen && latest > group.End() {
			continue
		}
		ending[endLine] = group
	}
	return ending
}

// excuseOf reads a comment group for the marker: opening any line of any comment in it, with the */ that closes a block
// comment no part of the reason. A nil group says nothing.
func excuseOf(group *ast.CommentGroup) excuse {
	if group == nil {
		return excuseNone
	}
	found := excuseNone
	for _, comment := range group.List {
		for _, line := range strings.Split(comment.Text, "\n") {
			// The marker opens the line, after the comment's own slashes, stars and spaces, so a sentence that only
			// mentions it, "this was a Timing assertion: once", excuses nothing.
			opening := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "/*"))
			reason, hasMarker := strings.CutPrefix(opening, timingAssertionMarker)
			if !hasMarker {
				continue
			}
			if strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(reason), "*/")) != "" {
				return excuseWithReason
			}
			found = excuseWithoutReason
		}
	}
	return found
}

// report flags comparison unless its excuse gives a reason, with the message that names what is missing.
func report(pass *analysis.Pass, comparison *ast.BinaryExpr, given excuse) {
	message := timingAssertionMessage
	switch given {
	case excuseWithReason:
		return
	case excuseWithoutReason:
		message = timingAssertionWithoutReasonMessage
	case excuseNone:
	}
	pass.Report(analysis.Diagnostic{
		Pos:      comparison.Pos(),
		End:      comparison.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{}),
	})
}
