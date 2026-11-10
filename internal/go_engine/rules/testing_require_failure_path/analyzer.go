// Package testing_require_failure_path is the Go engine's rule that a test can fail on a wrong result.
//
// A test whose body computes a result and checks nothing fails only when the code under it crashes, so go test
// prints ok whatever result that code returns. Its silence reads as a proof it never gave, and the next reader
// trusts a pass that was never at risk.
package testing_require_failure_path

import (
	"go/ast"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-require-failure-path"

// listener reads whether each file holds tests as the syntax dispatcher's walk enters it, and hears every function
// declaration in it.
var listener = rule.Listener{
	FileLocal: true,
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.FuncDecl)(nil)},
	Node:      judgeFunction,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "testing_require_failure_path", "Flags a test in a _test.go file whose "+
	"body, its function literals and subtests included, cannot fail on a wrong result: no Error, Errorf, Fatal, "+
	"Fatalf, Fail or FailNow on a *testing.T, no panic, and no test or reporter handed to code that could fail it, so "+
	"only a crash in the code under test fails it. A test that skips unconditionally before doing anything is not "+
	"judged.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var cannotFailMessage = policy.MessageOf(Name, "cannotFail")

// failingMethods are testing's methods that mark a test failed. Skip and its siblings end a test without failing
// it, and Log only writes, so neither is a way to fail.
var failingMethods = map[string]bool{
	"Error":   true,
	"Errorf":  true,
	"Fatal":   true,
	"Fatalf":  true,
	"Fail":    true,
	"FailNow": true,
}

// skippingMethods are testing's methods that end a test as skipped.
var skippingMethods = map[string]bool{
	"Skip":    true,
	"Skipf":   true,
	"SkipNow": true,
}

// fileState is what the rule reads of the file being walked, held in its file state (rule.FileState).
type fileState struct {
	// isTest is whether the file is a _test.go file, the only kind whose tests are judged.
	isTest bool
}

// enterFile reads whether file is a _test.go file.
func enterFile(context *rule.Context, file *ast.File) {
	// The file's own name, not one a //line directive gives it, since only a _test.go file holds tests.
	rule.FileState[fileState](context).isTest = strings.HasSuffix(
		context.FileSet.PositionFor(file.Package, false).Filename, "_test.go")
}

// judgeFunction reports node when it is a test in a _test.go file that cannot fail on a wrong result.
func judgeFunction(context *rule.Context, node ast.Node, _ []ast.Node) {
	function, isFunction := node.(*ast.FuncDecl)
	if !isFunction || !rule.FileState[fileState](context).isTest {
		return
	}
	parameter := testParameter(context, function)
	if parameter == nil || skipsFirst(context, function.Body, parameter) || mayFail(context, function.Body) {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos:      function.Name.Pos(),
		End:      function.Name.End(),
		Category: cannotFailMessage.Id,
		Message:  cannotFailMessage.Render(map[string]string{}),
	})
}

// mayFail is whether anything in body, its function literals included, is a way to fail the test. The walk is
// generous, since a wrong finding costs more than a missed one: a reporter, anything with an Errorf method as the
// *testing.T and testing.TB both have, counts as handed on wherever it is used other than as the receiver of one of
// testing's own methods, so passing it to a helper, storing it in a struct or wrapping it in a composite literal all
// trust the code that receives it to fail the test when it should.
func mayFail(context *rule.Context, body *ast.BlockStmt) bool {
	// receivers are the expressions a testing method is called on, which hand the test nowhere.
	receivers := map[ast.Expr]bool{}
	// literalRuns are the Run selectors whose subtest is a function literal, whose body the walk reaches and judges.
	literalRuns := map[*ast.SelectorExpr]bool{}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		expression, isExpression := node.(ast.Expr)
		if !isExpression {
			return true
		}
		switch expression := expression.(type) {
		case *ast.ParenExpr:
			// Parentheses change nothing, so the expression inside is the one judged.
			return true
		case *ast.CallExpr:
			if isBuiltinPanic(context, expression) {
				found = true
				return false
			}
			if selector, isSelector := ast.Unparen(expression.Fun).(*ast.SelectorExpr); isSelector &&
				len(expression.Args) == 2 {
				if _, isLiteral := ast.Unparen(expression.Args[1]).(*ast.FuncLit); isLiteral {
					literalRuns[selector] = true
				}
			}
		case *ast.SelectorExpr:
			if selection, isSelection := context.TypesInfo.Selections[expression]; isSelection &&
				isTestingMethod(selection.Obj()) {
				name := selection.Obj().Name()
				// A failing method counts whether it is called, held as a method value, or named as a method
				// expression, since each is a way to call it. A Run whose subtest is no literal runs a body the walk
				// cannot reach, which may fail, so it is trusted the way a helper is.
				if failingMethods[name] || (name == "Run" && !literalRuns[expression]) {
					found = true
					return false
				}
				if selection.Kind() == types.MethodVal {
					receivers[ast.Unparen(expression.X)] = true
				}
			}
		}
		if !receivers[expression] && isReporterValue(context, expression) {
			found = true
			return false
		}
		return true
	})
	return found
}

// isReporterValue is whether expression is a value whose type has an Errorf method: a *testing.T, a testing.TB, or a
// reporter of its own such as the one analysistest.Run takes, any of which can fail the test it wraps.
func isReporterValue(context *rule.Context, expression ast.Expr) bool {
	typeAndValue, recorded := context.TypesInfo.Types[expression]
	if !recorded || !typeAndValue.IsValue() || typeAndValue.Type == nil {
		return false
	}
	// Addressable, so a value whose Errorf has a pointer receiver counts too: the code handed it may take its address.
	object, _, _ := types.LookupFieldOrMethod(typeAndValue.Type, true, nil, "Errorf")
	_, isFunction := object.(*types.Func)
	return isFunction
}

// isBuiltinPanic is whether call is the predeclared panic, which fails the test it runs in.
func isBuiltinPanic(context *rule.Context, call *ast.CallExpr) bool {
	identifier, isIdentifier := ast.Unparen(call.Fun).(*ast.Ident)
	if !isIdentifier {
		return false
	}
	builtin, isBuiltin := context.TypesInfo.Uses[identifier].(*types.Builtin)
	return isBuiltin && builtin.Name() == "panic"
}

// isTestingMethod is whether object is a method declared in package testing, on T, the common type T embeds, or the
// TB interface, so a method of the same name on any other type is never mistaken for it.
func isTestingMethod(object types.Object) bool {
	method, isFunction := object.(*types.Func)
	return isFunction && method.Pkg() != nil && method.Pkg().Path() == "testing" && method.Signature().Recv() != nil
}

// skipsFirst is whether body skips unconditionally on parameter before doing anything, after at most a leading
// Parallel. Such a test never runs, so whether it could fail is not yet a question.
func skipsFirst(context *rule.Context, body *ast.BlockStmt, parameter *types.Var) bool {
	statements := body.List
	if len(statements) > 0 && callsOnParameter(context, statements[0], parameter, map[string]bool{"Parallel": true}) {
		statements = statements[1:]
	}
	return len(statements) > 0 && callsOnParameter(context, statements[0], parameter, skippingMethods)
}

// callsOnParameter is whether statement is a call of one of testing's methods named in names on parameter itself.
func callsOnParameter(context *rule.Context, statement ast.Stmt, parameter *types.Var, names map[string]bool) bool {
	expressionStatement, isExpression := statement.(*ast.ExprStmt)
	if !isExpression {
		return false
	}
	call, isCall := ast.Unparen(expressionStatement.X).(*ast.CallExpr)
	if !isCall {
		return false
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	receiver, isIdentifier := ast.Unparen(selector.X).(*ast.Ident)
	if !isIdentifier || context.TypesInfo.Uses[receiver] != parameter {
		return false
	}
	selection, isSelection := context.TypesInfo.Selections[selector]
	return isSelection && selection.Kind() == types.MethodVal && isTestingMethod(selection.Obj()) &&
		names[selection.Obj().Name()]
}

// testParameter is the *testing.T parameter of a top-level test, or nil when function is no test. A test is what go
// test runs as one: named Test, or Test followed by anything but a lower-case letter, with no receiver, no type
// parameters, no results, and one *testing.T parameter. Benchmarks, fuzz targets, examples and TestMain take
// something else or are named otherwise, so none of them is a test here.
func testParameter(context *rule.Context, function *ast.FuncDecl) *types.Var {
	if function.Recv != nil || function.Body == nil || !isTestName(function.Name.Name) {
		return nil
	}
	object, isFunction := context.TypesInfo.Defs[function.Name].(*types.Func)
	if !isFunction {
		return nil
	}
	signature := object.Signature()
	if signature.TypeParams().Len() > 0 || signature.Results().Len() > 0 || signature.Params().Len() != 1 {
		return nil
	}
	parameter := signature.Params().At(0)
	if !isPointerToTestingT(parameter.Type()) {
		return nil
	}
	return parameter
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
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(pointer.Elem()).(*types.Named)
	if !isNamed {
		return false
	}
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "testing" && named.Obj().Name() == "T"
}
