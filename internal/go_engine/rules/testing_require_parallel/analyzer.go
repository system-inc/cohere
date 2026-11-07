// Package testing_require_parallel is the Go engine's rule that a test runs in parallel unless it says why.
//
// Parallel is the house default: cohere spreads its work over every core, and its tests are held to the same. A
// serial test runs alone while its siblings wait, and the shared state that makes it serial is what the next reader
// needs to know before touching it, so a test that cannot be parallel says why in a comment where a reader looks.
package testing_require_parallel

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-require-parallel"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "testing_require_parallel",
	Doc: "Flags a test or subtest in a _test.go file whose first statement is not Parallel on its own *testing.T, " +
		"unless a // Not parallel: <reason> comment directly above it says why. A subtest whose function is not a " +
		"literal or a function declared in the same package is not judged, because its body cannot be seen.",
	Requires: []*analysis.Analyzer{marker.Analyzer},
	Run:      run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var notParallelMessage = policy.MessageOf(Name, "notParallel")

var notParallelWithoutReasonMessage = policy.MessageOf(Name, "notParallelWithoutReason")

func run(pass *analysis.Pass) (any, error) {
	indexes, isIndexes := pass.ResultOf[marker.Analyzer].(*marker.Indexes)
	if !isIndexes {
		return nil, fmt.Errorf("reading the comment indexes: marker gave %T", pass.ResultOf[marker.Analyzer])
	}
	// A subtest may name a function rather than spell a literal, and a function declared in this package has a body
	// the rule can judge the same way, so every declaration is found by its object.
	declarations := map[*types.Func]*ast.FuncDecl{}
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
				if object, isFunc := pass.TypesInfo.Defs[function.Name].(*types.Func); isFunc {
					declarations[object] = function
				}
			}
		}
	}
	for _, file := range pass.Files {
		// The file's own name, not one a //line directive gives it, since only a _test.go file holds tests.
		if !strings.HasSuffix(pass.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
			continue
		}
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				continue
			}
			parameter := testParameter(pass, function)
			if parameter == nil || firstStatementCallsParallel(pass, function.Body, parameter) {
				continue
			}
			report(pass, function.Name, marker.Read(function.Doc, marker.NotParallel))
		}
		judgeSubtests(pass, file, indexes.ForFile(file), declarations)
	}
	return nil, nil
}

// judgeSubtests reports every (*testing.T).Run in file whose subtest is serial and does not say why.
func judgeSubtests(
	pass *analysis.Pass,
	file *ast.File,
	comments *marker.Index,
	declarations map[*types.Func]*ast.FuncDecl,
) {
	ast.Inspect(file, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall || len(call.Args) != 2 {
			return true
		}
		selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		// A method value only: a method expression, (*testing.T).Run(test, ...), shifts its arguments by one.
		selection, isSelection := pass.TypesInfo.Selections[selector]
		if !isSelection || selection.Kind() != types.MethodVal {
			return true
		}
		if method, isFunc := selection.Obj().(*types.Func); !isFunc || !isTestingTMethod(method, "Run") {
			return true
		}
		body, parameter, doc := subtestFunction(pass, call.Args[1], declarations)
		if body == nil || firstStatementCallsParallel(pass, body, parameter) {
			return true
		}
		// A named function may carry the reason on its own doc comment, which then excuses every Run that names
		// it; the comment directly above the call speaks for this one subtest.
		above := marker.Read(comments.Above(call), marker.NotParallel)
		report(pass, selector, max(above, marker.Read(doc, marker.NotParallel)))
		return true
	})
}

// subtestFunction is the body, *testing.T parameter, and doc comment of the function a Run call runs, when the rule
// can see it: a function literal, or a function or method declared in this package, an instantiation of a generic
// one included. Otherwise body is nil, since a closure held in a variable or built by a call has a body the rule
// would have to guess at.
func subtestFunction(
	pass *analysis.Pass,
	argument ast.Expr,
	declarations map[*types.Func]*ast.FuncDecl,
) (*ast.BlockStmt, *types.Var, *ast.CommentGroup) {
	unwrapped := ast.Unparen(argument)
	if literal, isLiteral := unwrapped.(*ast.FuncLit); isLiteral {
		signature, isSignature := pass.TypesInfo.TypeOf(literal).(*types.Signature)
		if !isSignature || signature.Params().Len() != 1 {
			return nil, nil, nil
		}
		return literal.Body, signature.Params().At(0), nil
	}
	// An instantiated generic function, helper[int] or helper[int, string], runs the generic function's body, so the
	// type arguments are set aside and the name they instantiate is resolved. An index into a slice or map of
	// functions unwraps to a variable, which is no function and so is never judged.
	switch instantiated := unwrapped.(type) {
	case *ast.IndexExpr:
		unwrapped = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		unwrapped = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch named := unwrapped.(type) {
	case *ast.Ident:
		object = pass.TypesInfo.Uses[named]
	case *ast.SelectorExpr:
		// A method value, suite.serial, or a qualified function, which is another package's and so never found.
		if selection, isSelection := pass.TypesInfo.Selections[named]; isSelection {
			object = selection.Obj()
		} else {
			object = pass.TypesInfo.Uses[named.Sel]
		}
	}
	function, isFunc := object.(*types.Func)
	if !isFunc {
		return nil, nil, nil
	}
	// A method of an instantiated generic type, suite.serial on a *Suite[int], is an object of its own that no
	// declaration defines. Its origin is the one the declaration defines, and whose parameter the body's identifiers
	// resolve to, so both the lookup and the parameter go through it.
	function = function.Origin()
	declaration, declared := declarations[function]
	if !declared || declaration.Body == nil || function.Signature().Params().Len() != 1 {
		return nil, nil, nil
	}
	return declaration.Body, function.Signature().Params().At(0), declaration.Doc
}

// testParameter is the *testing.T parameter of a top-level test, or nil when function is no test. A test is what go
// test runs as one: named Test, or Test followed by anything but a lower-case letter, with no receiver, no type
// parameters, no results, and one *testing.T parameter. Benchmarks, fuzz targets, examples and TestMain take
// something else or are named otherwise, so none of them is a test here.
func testParameter(pass *analysis.Pass, function *ast.FuncDecl) *types.Var {
	if function.Recv != nil || function.Body == nil || !isTestName(function.Name.Name) {
		return nil
	}
	object, isFunc := pass.TypesInfo.Defs[function.Name].(*types.Func)
	if !isFunc {
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
	// A blank or unnamed parameter is still the test's: it cannot be called, so the test is serial.
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

// firstStatementCallsParallel is whether body's first statement is parameter.Parallel(). A Parallel later in the
// body leaves the test serial until then, and one on another test's *testing.T, the enclosing test's inside a
// subtest, marks that other test, so neither counts.
func firstStatementCallsParallel(pass *analysis.Pass, body *ast.BlockStmt, parameter *types.Var) bool {
	if len(body.List) == 0 {
		return false
	}
	statement, isExpression := body.List[0].(*ast.ExprStmt)
	if !isExpression {
		return false
	}
	call, isCall := ast.Unparen(statement.X).(*ast.CallExpr)
	if !isCall {
		return false
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	receiver, isIdentifier := ast.Unparen(selector.X).(*ast.Ident)
	if !isIdentifier || pass.TypesInfo.Uses[receiver] != parameter {
		return false
	}
	selection, isSelection := pass.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal {
		return false
	}
	method, isFunc := selection.Obj().(*types.Func)
	return isFunc && isTestingTMethod(method, "Parallel")
}

// isTestingTMethod is whether method is testing's (*T).<name>, by its package and receiver, so a method of the same
// name on any other type is never mistaken for it.
func isTestingTMethod(method *types.Func, name string) bool {
	if method.Name() != name || method.Pkg() == nil || method.Pkg().Path() != "testing" {
		return false
	}
	receiver := method.Signature().Recv()
	return receiver != nil && isPointerToTestingT(receiver.Type())
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

// report flags node unless its excuse gives a reason, with the message that names what is missing.
func report(pass *analysis.Pass, node ast.Node, given marker.Excuse) {
	message := notParallelMessage
	switch given {
	case marker.WithReason:
		return
	case marker.WithoutReason:
		message = notParallelWithoutReasonMessage
	case marker.None:
	}
	pass.Report(analysis.Diagnostic{
		Pos:      node.Pos(),
		End:      node.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{}),
	})
}
