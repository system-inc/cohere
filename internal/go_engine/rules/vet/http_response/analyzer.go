// Package http_response is vet's httpresponse pass, a deferred call on an HTTP response, such as
// `defer response.Body.Close()`, placed straight after the call that returns the response and before the error that
// says whether there is one is checked. When the call fails the response is nil, so the deferred call dereferences
// nil.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/httpresponse/httpresponse.go,
// whose logic it keeps and whose messages it reports word for word, at the same position. The original walks with
// the inspector's stack to find the statement list a call sits in; the port climbs from the call with the inspector's
// public cursor instead, visiting the same calls in the same order and reading the same statements. Like the
// original, it holds the deferred call's root to the response's name by the parser's resolution of the two, not the
// type checker's, so it finds every match the original finds, two unresolved names among them. The registry runs the
// port in the original's place, and the original stays the oracle: the parity tests beside this file run both over
// the same packages. The two agree everywhere, so there is no differences fixture.
package http_response

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "httpresponse",
	Doc: `check for mistakes using HTTP responses

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
diagnostic for such mistakes.`,
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/httpresponse",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("http_response: inspect's result is missing")
	}
	// A package that does not import net/http itself is passed over without a walk, as the original passes it over,
	// even when a call in it returns a response through a package that does.
	if !imports(pass.Pkg, "net/http") {
		return nil, nil
	}
	// Preorder visits each call in the order the original's WithStack does, and its cursor can climb to the
	// statements around the call, which is all the original reads of the stack.
	for cursor := range inspected.Root().Preorder((*ast.CallExpr)(nil)) {
		call, isCall := cursor.Node().(*ast.CallExpr)
		if !isCall {
			// Invariant: Preorder filtered to call expressions yields only cursors at call expressions.
			panic("http_response: Preorder yielded a node that is not a call")
		}
		if !isHttpFunctionOrMethodOnClient(pass.TypesInfo, call) {
			continue
		}
		statements, calls := restOfBlock(cursor)
		// The call is in the last statement of its block, so no defer follows it.
		if len(statements) < 2 {
			continue
		}
		// A call wrapped in another, `response, err := checked(http.Get(url))`, has its error checked by the wrapper
		// (golang/go#52661).
		if calls > 1 {
			continue
		}
		assignment, isAssignment := statements[0].(*ast.AssignStmt)
		if !isAssignment {
			continue
		}
		response := rootIdentifier(assignment.Lhs[0])
		if response == nil {
			continue
		}
		deferred, isDefer := statements[1].(*ast.DeferStmt)
		if !isDefer {
			continue
		}
		receiver := rootIdentifier(deferred.Call.Fun)
		if receiver == nil {
			continue
		}
		// The two names are the same variable by the parser's resolution, as the original judges them, and not by
		// the type checker's: two names the parser left unresolved, such as a package-level variable declared in
		// another file and an imported package's name, both hold nil and count as the same, so judging by
		// TypesInfo's objects would miss what the original finds.
		if response.Obj == receiver.Obj {
			pass.ReportRangef(receiver, "using %s before checking for errors", response.Name)
		}
	}
	return nil, nil
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

// restOfBlock finds the innermost block enclosing the node at call and returns its statements from the one holding
// the call to the end, with the number of calls met on the climb to that block, the call itself among them. x/tools'
// httpresponse.restOfBlock, which reads the same nodes from the traversal's stack.
func restOfBlock(call inspector.Cursor) ([]ast.Stmt, int) {
	calls := 0
	// child is the node last climbed through, on the path from the block down to the call.
	child := call
	for enclosing := range call.Enclosing() {
		if block, isBlock := enclosing.Node().(*ast.BlockStmt); isBlock {
			for index, statement := range block.List {
				if statement == child.Node() {
					return block.List[index:], calls
				}
			}
			// Unreachable: a block's only children are its statements, so the node climbed through is among them.
			// Kept as the original keeps it, ending the climb at the innermost block either way.
			break
		}
		if _, isCall := enclosing.Node().(*ast.CallExpr); isCall {
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
