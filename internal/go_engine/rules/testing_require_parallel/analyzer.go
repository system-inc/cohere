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
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-require-parallel"

// listener judges each _test.go file's tests as the syntax dispatcher's walk enters it, and hears every call in it
// for the subtests.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{marker.Analyzer},
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.CallExpr)(nil)},
	Node:      judgeSubtest,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "testing_require_parallel", "Flags a test or subtest in a _test.go file "+
	"whose first statement is not Parallel on its own *testing.T, unless a // Not parallel: <reason> comment "+
	"directly above it says why. A subtest whose function is not a literal or a function declared in the same "+
	"package is not judged, because its body cannot be seen.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var notParallelMessage = policy.MessageOf(Name, "notParallel")

var notParallelWithoutReasonMessage = policy.MessageOf(Name, "notParallelWithoutReason")

// packageState is what the rule reads once for its package and once for each file, held in its context's state.
type packageState struct {
	// indexes is the package's comment indexes, read as the walk enters its first file.
	indexes *marker.Indexes
	// declarations is every function declared in the package, by its object, read with indexes. A subtest may name
	// a function rather than spell a literal, and a function declared in this package has a body the rule can judge
	// the same way, so every declaration is found by its object.
	declarations map[*types.Func]*ast.FuncDecl
	// isTest is whether the file being walked is a _test.go file, the only kind whose tests are judged.
	isTest bool
	// comments is the comment index of the file being walked, when it is a _test.go file.
	comments *marker.Index
}

// enterFile reads the package's comment indexes and declarations the first time, and reports every top-level test in
// file, when it is a _test.go file, that is serial and does not say why.
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
		state.declarations = map[*types.Func]*ast.FuncDecl{}
		for _, packageFile := range context.Files {
			for _, declaration := range packageFile.Decls {
				if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
					if object, isFunc := context.TypesInfo.Defs[function.Name].(*types.Func); isFunc {
						state.declarations[object] = function
					}
				}
			}
		}
	}
	// The file's own name, not one a //line directive gives it, since only a _test.go file holds tests.
	state.isTest = strings.HasSuffix(context.Fset.PositionFor(file.Package, false).Filename, "_test.go")
	state.comments = nil
	if !state.isTest {
		return
	}
	state.comments = state.indexes.ForFile(file)
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction {
			continue
		}
		parameter := testParameter(context, function)
		if parameter == nil || firstStatementCallsParallel(context, function.Body, parameter) {
			continue
		}
		report(context, function.Name, marker.Read(function.Doc, marker.NotParallel))
	}
}

// judgeSubtest reports node when it is a (*testing.T).Run in a _test.go file whose subtest is serial and does not say
// why.
func judgeSubtest(context *rule.Context, node ast.Node, _ []ast.Node) {
	state := rule.State[packageState](context)
	call, isCall := node.(*ast.CallExpr)
	if !state.isTest || !isCall || len(call.Args) != 2 {
		return
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return
	}
	// A method value only: a method expression, (*testing.T).Run(test, ...), shifts its arguments by one.
	selection, isSelection := context.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal {
		return
	}
	if method, isFunc := selection.Obj().(*types.Func); !isFunc || !isTestingTMethod(method, "Run") {
		return
	}
	body, parameter, doc := subtestFunction(context, call.Args[1], state.declarations)
	if body == nil || firstStatementCallsParallel(context, body, parameter) {
		return
	}
	// A named function may carry the reason on its own doc comment, which then excuses every Run that names it; the
	// comment directly above the call speaks for this one subtest.
	above := marker.Read(state.comments.Above(call), marker.NotParallel)
	report(context, selector, max(above, marker.Read(doc, marker.NotParallel)))
}

// subtestFunction is the body, *testing.T parameter, and doc comment of the function a Run call runs, when the rule
// can see it: a function literal, or a function or method declared in this package, an instantiation of a generic
// one included. Otherwise body is nil, since a closure held in a variable or built by a call has a body the rule
// would have to guess at.
func subtestFunction(
	context *rule.Context,
	argument ast.Expr,
	declarations map[*types.Func]*ast.FuncDecl,
) (*ast.BlockStmt, *types.Var, *ast.CommentGroup) {
	unwrapped := ast.Unparen(argument)
	if literal, isLiteral := unwrapped.(*ast.FuncLit); isLiteral {
		signature, isSignature := context.TypesInfo.TypeOf(literal).(*types.Signature)
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
		object = context.TypesInfo.Uses[named]
	case *ast.SelectorExpr:
		// A method value, suite.serial, or a qualified function, which is another package's and so never found.
		if selection, isSelection := context.TypesInfo.Selections[named]; isSelection {
			object = selection.Obj()
		} else {
			object = context.TypesInfo.Uses[named.Sel]
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
func testParameter(context *rule.Context, function *ast.FuncDecl) *types.Var {
	if function.Recv != nil || function.Body == nil || !isTestName(function.Name.Name) {
		return nil
	}
	object, isFunc := context.TypesInfo.Defs[function.Name].(*types.Func)
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
func firstStatementCallsParallel(context *rule.Context, body *ast.BlockStmt, parameter *types.Var) bool {
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
	if !isIdentifier || context.TypesInfo.Uses[receiver] != parameter {
		return false
	}
	selection, isSelection := context.TypesInfo.Selections[selector]
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
func report(context *rule.Context, node ast.Node, given marker.Excuse) {
	message := notParallelMessage
	switch given {
	case marker.WithReason:
		return
	case marker.WithoutReason:
		message = notParallelWithoutReasonMessage
	case marker.None:
	}
	context.Report(analysis.Diagnostic{
		Pos:      node.Pos(),
		End:      node.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{}),
	})
}
