// Package correctness_require_response_status_check is the Go engine's rule that the body of a response net/http
// returned is not read in a function that never asks whether the request succeeded.
//
// net/http returns a nil error for every response that arrives, whatever its status, so a 401, a 404 or a 500 comes
// back as a response like any other and its body (an error page, a JSON error envelope) reads as the answer. Code
// that decodes it, returns it or caches it treats the failure as success, far from where the request was made. The
// status is the protocol's own answer, so the body is trusted only in a function that reads it.
//
// It ports nexus/correctness-require-response-status-check and cohere-swift's twin, narrowed to what a reading of
// one function settles without its control-flow graph:
//
//   - Which responses it tracks. A local variable of type *net/http.Response, declared by a statement in a function
//     body, every value of which comes from a call known to return the response whatever its status: net/http's
//     Get, Post, Head and PostForm, and the same four plus Do on a *http.Client, resolved by the function the call
//     reaches, never by its spelling. A variable written anything else even once (another function's result, a
//     range value, a plain assignment) is not followed, nor is a parameter, a named result, a struct field or a
//     package variable, since a wrapper may have checked the status already. As in the twins, a wrapper is a
//     missed finding, never a false one.
//   - What counts as checking. Any read of the response's StatusCode or Status field, anywhere in the function and
//     its closures: in a condition, a log line or an error message alike, since reading more as a check can only
//     silence a site. The twins accept a check after the body read when every path from the read to the exit
//     passes one, and Go's usual shape reads the body first to put it in the error, so a check after the read
//     counts here too. Without the graph, a check on one branch covers the other, a missed finding.
//   - What counts as an escape, which counts as a check as it does in the twins: the response passed to a function,
//     returned, stored, assigned, sent, put in a literal, its address taken, a field of it written, or a method
//     called on it (Write, for one, writes the status line). The receiver may read the status, and proving that it
//     does would mean following it, so the rule assumes it does.
//   - What counts as reading the body. resp.Body passed to a call (io.ReadAll, json.NewDecoder, io.Copy, any other),
//     or a method other than Close called on it. Closing the body reads none of it, nor does draining it into
//     io.Discard, which is what a caller does to reuse the connection, nor does a deferred call, which is the
//     cleanup that runs on the way out. Any other use of Body (returning it, storing it, comparing it with nil) is
//     not judged, so the rule never reports a body it does not see being read.
//   - Everything else is neutral: the Header, ContentLength, Request and the other fields, and comparing the response
//     with nil.
//
// A finding is placed on resp.Body at each read.
package correctness_require_response_status_check

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-response-status-check"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_require_response_status_check",
	Doc: "Flags the body of a local *http.Response from net/http's Get, Post, Head, PostForm or a *http.Client's " +
		"Do, Get, Post, Head or PostForm read in a function that never reads its StatusCode or Status nor hands it " +
		"elsewhere, because net/http returns a nil error on a 4xx or 5xx and the error body then reads as data.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var bodyReadWithoutStatusCheckMessage = policy.MessageOf(Name, "bodyReadWithoutStatusCheck")

// producerFunctions are net/http's package functions that return the response whatever its status, and
// producerMethods the methods of *http.Client that do. Both were read in net/http's source: each ends in
// Client.Do, which returns a non-nil error only when no response arrived or a redirect failed.
var producerFunctions = map[string]bool{"Get": true, "Post": true, "Head": true, "PostForm": true}

var producerMethods = map[string]bool{"Do": true, "Get": true, "Post": true, "Head": true, "PostForm": true}

// statusFields are the fields of http.Response whose read asks whether the request succeeded.
var statusFields = map[string]bool{"StatusCode": true, "Status": true}

// response is what one file's walk learns about one *http.Response variable.
type response struct {
	// declared is whether a statement in a function body declares the variable, which rules out a parameter, a
	// named result and a package variable.
	declared bool
	// producerWritten is whether a tracked call writes it, and otherWritten whether anything else does.
	producerWritten bool
	otherWritten    bool
	// checked is whether its status is read or it escapes anywhere.
	checked bool
	// bodyReads are the resp.Body expressions read, each a finding if the variable is tracked and never checked.
	bodyReads []ast.Expr
}

func run(pass *analysis.Pass) (any, error) {
	// A local variable lives in one file, so every reference to it is met in that file's walk, but one map for the
	// package costs nothing and the order of findings follows the order variables are met.
	responses := map[*types.Var]*response{}
	var order []*types.Var
	for _, file := range pass.Files {
		var stack []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, node)
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier {
				return true
			}
			variable := responseVariable(pass, identifier)
			if variable == nil {
				return true
			}
			found, seen := responses[variable]
			if !seen {
				found = &response{}
				responses[variable] = found
				order = append(order, variable)
			}
			classify(pass, stack, identifier, found)
			return true
		})
	}
	for _, variable := range order {
		found := responses[variable]
		if !found.declared || !found.producerWritten || found.otherWritten || found.checked {
			continue
		}
		for _, body := range found.bodyReads {
			pass.Report(analysis.Diagnostic{
				Pos:      body.Pos(),
				End:      body.End(),
				Category: bodyReadWithoutStatusCheckMessage.Id,
				Message:  bodyReadWithoutStatusCheckMessage.Render(map[string]string{}),
			})
		}
	}
	return nil, nil
}

// responseVariable is the variable identifier defines or refers to when it is a *http.Response that is not a
// package variable or a struct field, or nil otherwise.
func responseVariable(pass *analysis.Pass, identifier *ast.Ident) *types.Var {
	object := pass.TypesInfo.Defs[identifier]
	if object == nil {
		object = pass.TypesInfo.Uses[identifier]
	}
	variable, isVariable := object.(*types.Var)
	if !isVariable || variable.IsField() || variable.Pkg() == nil || variable.Parent() == variable.Pkg().Scope() {
		return nil
	}
	if !isPointerToHttpResponse(variable.Type()) {
		return nil
	}
	return variable
}

// classify records what identifier, the node at the top of stack, does to its response. stack runs from the file
// down to the identifier.
//
// Mutates: found records what this use does to the response, gathered over every use the caller walks
func classify(pass *analysis.Pass, stack []ast.Node, identifier *ast.Ident, found *response) {
	switch parent := stack[len(stack)-2].(type) {
	case *ast.AssignStmt:
		for _, left := range parent.Lhs {
			if left != identifier {
				continue
			}
			// A := declares the names it is the first to define, and reuses any already declared in its scope, so
			// a := is a declaration only for a name it defines, and a write for every name.
			if parent.Tok == token.DEFINE && pass.TypesInfo.Defs[identifier] != nil {
				found.declared = true
			}
			if isProducerAssignment(pass, parent.Lhs, parent.Rhs, left) {
				found.producerWritten = true
			} else {
				found.otherWritten = true
			}
			return
		}
	case *ast.ValueSpec:
		if !slices.Contains(parent.Names, identifier) {
			break
		}
		// A var with no value starts the response at nil, which writes nothing a body could be read from.
		found.declared = true
		if len(parent.Values) == 0 {
			return
		}
		left := make([]ast.Expr, len(parent.Names))
		for index, declared := range parent.Names {
			left[index] = declared
		}
		if isProducerAssignment(pass, left, parent.Values, identifier) {
			found.producerWritten = true
		} else {
			found.otherWritten = true
		}
		return
	case *ast.RangeStmt:
		if parent.Key == identifier || parent.Value == identifier {
			found.otherWritten = true
			return
		}
	case *ast.Field:
		// A parameter or named result: neither declared by a statement, and a named result is returned by a bare
		// return no identifier marks, so the rule leaves it alone.
		return
	}
	// The response itself, looked at from outside any parentheses around it.
	index := outward(stack, len(stack)-1)
	if index == 0 {
		return
	}
	switch parent := stack[index-1].(type) {
	case *ast.SelectorExpr:
		if parent.X == stack[index] {
			classifySelection(pass, stack, index-1, parent, found)
			return
		}
	case *ast.BinaryExpr:
		// Comparing the response with nil asks whether one arrived, not whether the request succeeded.
		if (parent.Op == token.EQL || parent.Op == token.NEQ) && (isNil(pass, parent.X) || isNil(pass, parent.Y)) {
			return
		}
	}
	// Anything else hands the response somewhere this rule cannot follow: an argument, a return, an assignment, a
	// send, a composite literal, an address taken, a dereference copied.
	found.checked = true
}

// classifySelection records what resp.<selector>, the node at stack[index], does to its response.
//
// Mutates: found records a status check or a body read through the selection, for the caller's verdict
func classifySelection(pass *analysis.Pass, stack []ast.Node, index int, selector *ast.SelectorExpr, found *response) {
	selection, isSelection := pass.TypesInfo.Selections[selector]
	if !isSelection {
		return
	}
	if selection.Kind() != types.FieldVal {
		// A method of *http.Response may read the status (Write writes the status line), so calling one is
		// handing the response to code the rule cannot see.
		found.checked = true
		return
	}
	outer := outward(stack, index)
	// A field written, resp.Body = reader or resp.StatusCode = 200, changes what a later read finds, so the rule
	// stops judging the response.
	if outer > 0 {
		if assignment, isAssignment := stack[outer-1].(*ast.AssignStmt); isAssignment {
			for _, left := range assignment.Lhs {
				if left == stack[outer] {
					found.checked = true
					return
				}
			}
		}
	}
	if statusFields[selection.Obj().Name()] {
		found.checked = true
		return
	}
	if selection.Obj().Name() == "Body" && isBodyRead(pass, stack, outer) {
		found.bodyReads = append(found.bodyReads, selector)
	}
}

// isBodyRead is whether resp.Body, the node at stack[index] seen from outside its parentheses, is read there: passed
// to a call, or the receiver of a method other than Close. A deferred call is cleanup on the way out, and a copy into
// io.Discard drains the body unread, so neither reads it.
func isBodyRead(pass *analysis.Pass, stack []ast.Node, index int) bool {
	if index == 0 {
		return false
	}
	body := stack[index]
	switch parent := stack[index-1].(type) {
	case *ast.SelectorExpr:
		if parent.X != body || parent.Sel.Name == "Close" {
			return false
		}
		outer := outward(stack, index-1)
		if outer > 0 {
			if call, isCall := stack[outer-1].(*ast.CallExpr); isCall && call.Fun == stack[outer] {
				return !isDeferred(stack, outer-1)
			}
		}
		// A method value taken and not called here, reader := resp.Body.Read, reads when it is called later.
		return true
	case *ast.CallExpr:
		if parent.Fun == body {
			return false
		}
		// A conversion, io.Reader(resp.Body), is spelled as a call and reads nothing.
		if typeAndValue, isRecorded := pass.TypesInfo.Types[parent.Fun]; isRecorded && typeAndValue.IsType() {
			return false
		}
		return !isDeferred(stack, index-1) && !isDiscardCopy(pass, parent)
	}
	return false
}

// isDeferred is whether the call at stack[index] is the call a defer statement makes.
func isDeferred(stack []ast.Node, index int) bool {
	if index == 0 {
		return false
	}
	deferred, isDefer := stack[index-1].(*ast.DeferStmt)
	return isDefer && deferred.Call == stack[index]
}

// isDiscardCopy is whether call is io.Copy, io.CopyN or io.CopyBuffer into io.Discard, the drain a caller writes to
// let the connection be reused, which reads the body only to throw it away.
func isDiscardCopy(pass *analysis.Pass, call *ast.CallExpr) bool {
	function, isFunction := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !isFunction || function.Pkg() == nil || function.Pkg().Path() != "io" || len(call.Args) == 0 {
		return false
	}
	switch function.Name() {
	case "Copy", "CopyN", "CopyBuffer":
	default:
		return false
	}
	// io/ioutil's Discard is declared as io.Discard, a variable of its own whose value is the same, so either one
	// is the destination by its object.
	var destination types.Object
	switch written := ast.Unparen(call.Args[0]).(type) {
	case *ast.SelectorExpr:
		destination = pass.TypesInfo.Uses[written.Sel]
	case *ast.Ident:
		destination = pass.TypesInfo.Uses[written]
	}
	variable, isVariable := destination.(*types.Var)
	if !isVariable || variable.Pkg() == nil || variable.Name() != "Discard" {
		return false
	}
	return variable.Pkg().Path() == "io" || variable.Pkg().Path() == "io/ioutil"
}

// isProducerAssignment is whether target, one of left, takes the response a tracked call returns: right is that
// one call, its two results go to two names, and target is the first.
func isProducerAssignment(pass *analysis.Pass, left []ast.Expr, right []ast.Expr, target ast.Expr) bool {
	if len(right) != 1 || len(left) != 2 || left[0] != target {
		return false
	}
	call, isCall := ast.Unparen(right[0]).(*ast.CallExpr)
	return isCall && isProducer(pass, call)
}

// isProducer is whether call reaches one of net/http's package functions or *http.Client's methods that return the
// response whatever its status. A method of another type with the same name, or a function value, is never one.
func isProducer(pass *analysis.Pass, call *ast.CallExpr) bool {
	function, isFunction := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !isFunction || function.Pkg() == nil || function.Pkg().Path() != "net/http" {
		return false
	}
	receiver := function.Signature().Recv()
	if receiver == nil {
		return producerFunctions[function.Name()]
	}
	return producerMethods[function.Name()] && isPointerToHttpNamed(receiver.Type(), "Client")
}

func isPointerToHttpResponse(candidate types.Type) bool {
	return isPointerToHttpNamed(candidate, "Response")
}

// isPointerToHttpNamed is whether candidate is a pointer to net/http's type of that name, by package and name, so a
// type of the same name declared anywhere else is never mistaken for it.
func isPointerToHttpNamed(candidate types.Type, name string) bool {
	pointer, isPointer := types.Unalias(candidate).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(pointer.Elem()).(*types.Named)
	if !isNamed {
		return false
	}
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http" && named.Obj().Name() == name
}

// outward is the index in stack of the node at index seen from outside every pair of parentheses around it.
func outward(stack []ast.Node, index int) int {
	outermost := index
	for outermost > 0 {
		if _, isParenthesized := stack[outermost-1].(*ast.ParenExpr); !isParenthesized {
			break
		}
		outermost--
	}
	return outermost
}

// isNil is whether expression is the predeclared nil, by its object, so a variable a package names nil is not.
func isNil(pass *analysis.Pass, expression ast.Expr) bool {
	identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	if !isIdentifier {
		return false
	}
	_, isNilObject := pass.TypesInfo.Uses[identifier].(*types.Nil)
	return isNilObject
}
