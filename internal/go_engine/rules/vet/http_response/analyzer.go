// Package http_response is vet's httpresponse pass, a deferred call on an HTTP response, such as
// `defer response.Body.Close()`, placed straight after the call that returns the response and before the error that
// says whether there is one is checked. When the call fails the response is nil, so the deferred call dereferences
// nil.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/httpresponse/httpresponse.go,
// whose logic it keeps and whose messages it reports word for word, at the same position. It sits on the engine's
// syntax dispatcher, hearing each call on the walk the dispatcher makes once for every syntax rule, and like the
// original it finds the statement list a call sits in from the walk's stack of the call's ancestors. The registry
// runs the port in the original's place, and the original stays the oracle: the parity tests beside this file run
// both over the same packages.
//
// The two differ in one judgment only, each case in testdata/src/differences and held by
// TestPortDiffersFromOriginalOnlyWhereItIsRight. The original holds the deferred call's root to the response's name
// by the parser's resolution of the two, under which every name the parser leaves unresolved is nil and so the same
// as every other: `_` assigned with `=`, a package-level variable declared in another file, an imported package's
// name, a dot import's and a predeclared function's. It reports `defer log.Println()` after `_, err = http.Get(url)`
// as a use of `_`, a finding that is false. The port compares the objects the type checker gives the two names, so
// it drops those findings and keeps every one where the two are the same variable, resolved by the parser or not.
package http_response

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "httpresponse"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = `check for mistakes using HTTP responses

A common mistake when using the net/http package is to defer a function
call to close the http.Response Body before checking the error that
determines whether the response is valid:

	resp, err := http.Head(url)
	defer resp.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
	// (defer statement belongs here)

This checker helps uncover latent nil dereference bugs by reporting a
diagnostic for such mistakes.`

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/httpresponse"

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, as the original's
// WithStack visits them, with the call's ancestors, from which it reads the statements around the call. It judges
// only a package that imports net/http, which it asks once for the package, on the first call, and keeps in the
// context's state.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node:  checkCall,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	return analyzer
}

// packageGate is whether the package imports net/http, asked once, on its first call.
type packageGate struct {
	// decided is whether the package has been asked about yet.
	decided bool
	// importsHttp is whether the package imports net/http itself, without which nothing is judged.
	importsHttp bool
}

// checkCall reports a deferred call on the response call returns, placed straight after the statement holding call
// and before its error is checked, reading the statements around call from stack, its ancestors.
func checkCall(context *rule.Context, node ast.Node, stack []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	// A package that does not import net/http itself is passed over, as the original passes it over without a walk,
	// even when a call in it returns a response through a package that does.
	gate := rule.State[packageGate](context)
	if !gate.decided {
		gate.decided, gate.importsHttp = true, imports(context.Package, "net/http")
	}
	if !gate.importsHttp {
		return
	}
	if !isHttpFunctionOrMethodOnClient(context.TypesInfo, call) {
		return
	}
	statements, calls := restOfBlock(call, stack)
	// The call is in the last statement of its block, so no defer follows it.
	if len(statements) < 2 {
		return
	}
	// A call wrapped in another, `response, err := checked(http.Get(url))`, has its error checked by the wrapper
	// (golang/go#52661).
	if calls > 1 {
		return
	}
	assignment, isAssignment := statements[0].(*ast.AssignStmt)
	if !isAssignment {
		return
	}
	response := rootIdentifier(assignment.Lhs[0])
	if response == nil {
		return
	}
	deferred, isDefer := statements[1].(*ast.DeferStmt)
	if !isDefer {
		return
	}
	receiver := rootIdentifier(deferred.Call.Fun)
	if receiver == nil {
		return
	}
	// The two names are judged by the objects the type checker gives them, where the original compares the
	// parser's: two names the parser left unresolved, such as `_` assigned with `=`, a package-level variable
	// declared in another file, an imported package's name or a predeclared function, both hold nil there and
	// count as one, so `defer log.Println()` after `_, err = http.Get(url)` was reported as a use of `_`. A name
	// with no object, `_` assigned with `=`, is never the same as another. Two names without objects would match
	// without the nil check, but a deferred call's root always denotes something in a package that type-checks,
	// so no fixture can make that check fire; it is kept so a pair of unknowns is never taken for one variable.
	responseObject := context.TypesInfo.ObjectOf(response)
	if responseObject != nil && responseObject == context.TypesInfo.ObjectOf(receiver) {
		context.ReportRangef(receiver, "using %s before checking for errors", response.Name)
	}
}

// isHttpFunctionOrMethodOnClient is whether call is to a function of a package named http, or a method of
// http.Client or *http.Client, that returns (*http.Response, error). x/tools' httpresponse.isHTTPFuncOrMethodOnClient.
func isHttpFunctionOrMethodOnClient(information *types.Info, call *ast.CallExpr) bool {
	function, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		// The call is not of the form x.f(). The original reads Types under a nil selector here, finding no
		// signature, so it returns false too.
		return false
	}
	signature, isSignature := information.Types[function].Type.(*types.Signature)
	if !isSignature {
		return false
	}
	results := signature.Results()
	if results.Len() != 2 {
		return false
	}
	isPointer, named := receiverNamed(results.At(0))
	if !isPointer || named == nil || !isTypeNamed(named, "net/http", "Response") {
		return false
	}
	errorType := types.Universe.Lookup("error").Type()
	if !types.Identical(results.At(1).Type(), errorType) {
		return false
	}
	selectedType := information.Types[function.X].Type
	if selectedType == nil {
		// A package's name has no type, so this is a qualified identifier, judged by the name it is written with,
		// as the original judges it: `http.Get` under any import named http, and not under net/http imported as
		// another name.
		identifier, isIdentifier := function.X.(*ast.Ident)
		return isIdentifier && identifier.Name == "http"
	}
	if isTypeNamed(selectedType, "net/http", "Client") {
		return true
	}
	pointer, isPointerType := types.Unalias(selectedType).(*types.Pointer)
	return isPointerType && isTypeNamed(pointer.Elem(), "net/http", "Client")
}

// restOfBlock finds the innermost block enclosing call, whose ancestors stack holds, outermost first, and returns its
// statements from the one holding the call to the end, with the number of calls met on the climb to that block, the
// call itself among them. x/tools' httpresponse.restOfBlock, which reads the same nodes from a stack holding the call
// on its top. It stands in for the climb with the inspector's cursor, `cursor.Enclosing()`, which yields the call,
// then its ancestors innermost first: stack read from its end is that climb after its first step.
func restOfBlock(call *ast.CallExpr, stack []ast.Node) ([]ast.Stmt, int) {
	// The call itself is the first call met, the first node Enclosing yields and the top of the original's stack,
	// which the dispatcher's stack leaves out.
	calls := 1
	// child is the node last climbed through, on the path from the block down to the call.
	var child ast.Node = call
	for index := len(stack) - 1; index >= 0; index-- {
		enclosing := stack[index]
		if block, isBlock := enclosing.(*ast.BlockStmt); isBlock {
			for statementIndex, statement := range block.List {
				if statement == child {
					return block.List[statementIndex:], calls
				}
			}
			// Unreachable: a block's only children are its statements, so the node climbed through is among them.
			// Kept as the original keeps it, ending the climb at the innermost block either way.
			break
		}
		if _, isCall := enclosing.(*ast.CallExpr); isCall {
			calls++
		}
		child = enclosing
	}
	return nil, 0
}

// rootIdentifier is the identifier x at the root of a chain of selections x.y.z, and nil when the chain is rooted in
// anything else. x/tools' httpresponse.rootIdent.
func rootIdentifier(node ast.Node) *ast.Ident {
	switch rooted := node.(type) {
	case *ast.SelectorExpr:
		return rootIdentifier(rooted.X)
	case *ast.Ident:
		return rooted
	default:
		return nil
	}
}

// imports is whether importer imports the package at path directly. x/tools' typesinternal.Imports.
func imports(importer *types.Package, path string) bool {
	for _, imported := range importer.Imports() {
		if imported.Path() == path {
			return true
		}
	}
	return false
}

// receiverNamed is whether variable's type is a pointer, and the named type it is or points to, nil when neither,
// looking through aliases. x/tools' typesinternal.ReceiverNamed.
func receiverNamed(variable *types.Var) (bool, *types.Named) {
	variableType := variable.Type()
	isPointer := false
	if pointer, isPointerType := types.Unalias(variableType).(*types.Pointer); isPointerType {
		isPointer = true
		variableType = pointer.Elem()
	}
	named, _ := types.Unalias(variableType).(*types.Named) // A type that is not named leaves named nil, as wanted.
	return isPointer, named
}

// isTypeNamed is whether checked is, or is an alias of, a package-level defined type of the package at path with
// the given name. x/tools' typesinternal.IsTypeNamed, for one name.
func isTypeNamed(checked types.Type, path string, name string) bool {
	named, isNamed := types.Unalias(checked).(*types.Named)
	if !isNamed {
		return false
	}
	typeName := named.Obj()
	return typeName != nil && isPackageLevel(typeName) && typeName.Pkg().Path() == path && typeName.Name() == name
}

// isPackageLevel is whether object is declared at its package's scope. x/tools' typesinternal.IsPackageLevel.
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}
