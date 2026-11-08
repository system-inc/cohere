// Package buffer_string_conversion is staticcheck's S1030, a bytes.Buffer's contents converted to the other form,
// `string(buffer.Bytes())` or `[]byte(buffer.String())`, which the buffer's other method returns already, ported as
// a check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1030/s1030.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the conversion. The original matches its one query,
// `(CallExpr _ [(CallExpr sel@(SelectorExpr recv _) [])])`, through staticcheck's pattern language against every
// call the inspector's cursor walks, and builds its fix from `(CallExpr (SelectorExpr recv (Ident "String")) [])` or
// its Bytes twin through edit.ReplaceWithPattern. The port asks the same of each call directly, reading
// pattern/match.go for what each construct of the query matches, builds the fix's syntax itself as
// pattern.NodeToAST builds it, around the receiver the query binds, and reports through staticcheck_report.Report
// where the original reports through report.Report. The registry still runs the original, which stays the oracle:
// the parity tests beside this file run both over the same packages, and the two differ in one way, where the
// original is wrong: the port drops a finding whose receiver's other method, the one the message asks for, is not
// the buffer's own, recorded in the differences fixture.
package buffer_string_conversion

import (
	"go/ast"
	"go/types"

	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, judged beside the
// node that holds it, as the original walks the inspector's cursor. The original matches each call through
// code.Match, which asks the type index nothing, and code.IsCallTo names the callee by its object, not through the
// index, so every package is walked and a package's own calls are judged like any other's.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, stack []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("buffer_string_conversion: the dispatcher handed over a node that is not a call")
		}
		if len(stack) == 0 {
			// Invariant: a call stands inside a file, which the walk holds among its ancestors.
			panic("buffer_string_conversion: the dispatcher handed over a call with no ancestor")
		}
		// The call's parent is the last of its ancestors, the node the cursor's Parent names.
		Check(context, call, stack[len(stack)-1])
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1030",
	"S1030",
	"Flags a bytes.Buffer's contents converted to what its other method gives: staticcheck's S1030, ported.",
	&Listener,
)

// Check judges one call, held by parent: a conversion to string of a call of (*bytes.Buffer).Bytes, or to a slice of
// byte of a call of (*bytes.Buffer).String, both with no arguments, asks for what the buffer's other method returns,
// and the fix calls that method on the same receiver. A conversion to string held by an index expression is left
// alone, since the compiler looks a map up by `m[string(buffer.Bytes())]` without copying the bytes.
func Check(context *rule.Context, call *ast.CallExpr, parent ast.Node) {
	// The bytes package is free to use its own buffer as it sees fit, and so are its external tests.
	if context.Package.Path() == "bytes" || context.Package.Path() == "bytes_test" {
		return
	}
	// The query's argument list `[...]` of one pattern is exactly one argument, its callee `_` anything at all.
	if len(call.Args) != 1 {
		return
	}
	// The pattern sees through parentheses around the argument, but code.IsCallTo, handed call.Args[0] as written,
	// asks a parenthesized argument whether it is a call and is told no, so `string((buffer.Bytes()))` matches the
	// query and is reported by neither branch: the argument must be the inner call itself.
	inner, isInner := call.Args[0].(*ast.CallExpr)
	if !isInner || len(inner.Args) != 0 {
		return
	}
	// `sel@(SelectorExpr recv _)` sees through parentheses around the callee, and the binding holds the selector
	// without them, while `recv` holds the receiver without its own: the message renders sel.X as written, its
	// parentheses included, and the fix places recv bare. A method expression, `(*bytes.Buffer).Bytes(buffer)`, has
	// an argument, and a method value called through a variable has no selector, so neither is the query's.
	selector, isSelector := ast.Unparen(inner.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return
	}
	receiver := ast.Unparen(selector.X)

	// The conversion's type is read through aliases alone, and compared by identity with the universe's string and
	// byte, so a defined type over either is not one, and nor is `[]uint8`, whose element is the universe's uint8,
	// a type distinct from byte's even as the two are identical.
	converted := types.Unalias(context.TypesInfo.TypeOf(call.Fun))
	if converted == types.Universe.Lookup("string").Type() &&
		staticcheck_code.IsCallTo(context, inner, "(*bytes.Buffer).Bytes") {
		// The parent is the walk's, the node that holds the conversion directly, of any of its fields: a
		// parenthesized index, `m[(string(buffer.Bytes()))]`, is held by its parentheses, and a conversion indexed
		// itself, `string(buffer.Bytes())[0]`, is held by the index too.
		if _, isIndex := parent.(*ast.IndexExpr); isIndex {
			return
		}
		if isBuffersOwn(context, receiver, "String") {
			report(context, call, selector, receiver, "String")
		}
		return
	}
	slice, isSlice := converted.(*types.Slice)
	if isSlice && types.Unalias(slice.Elem()) == types.Universe.Lookup("byte").Type() &&
		staticcheck_code.IsCallTo(context, inner, "(*bytes.Buffer).String") &&
		isBuffersOwn(context, receiver, "Bytes") {
		report(context, call, selector, receiver, "Bytes")
	}
}

// isBuffersOwn is whether method, selected on receiver, is the bytes.Buffer's own, the one the message asks for. The
// original asks this of the method called and never of the one it suggests, so a type that embeds a buffer and
// declares a String of its own, or embeds a second type with one beside it at the same depth, has
// `string(holder.Bytes())` flagged with a fix that calls another method or, ambiguous, fails to compile: the port
// drops that finding. The receiver's method was already found to be the buffer's, so the receiver is a value with a
// type, and it is addressable or a pointer, so its other method, of the same pointer receiver, is in reach alike.
func isBuffersOwn(context *rule.Context, receiver ast.Expr, method string) bool {
	receiverType := context.TypesInfo.TypeOf(receiver)
	if receiverType == nil {
		// Invariant: the receiver's Bytes or String was named a method of bytes.Buffer through its selection, which
		// the type checker records only on an operand it typed.
		panic("buffer_string_conversion: the receiver of a buffer's method has no type")
	}
	selected, _, _ := types.LookupFieldOrMethod(receiverType, true, context.Package, method)
	function, isFunction := selected.(*types.Func)
	return isFunction && typeutil.FuncName(function) == "(*bytes.Buffer)."+method
}

// report reports call, whose argument calls the buffer's method through selector, asking for method, the buffer's
// other one, and suggests the call of method on receiver in the conversion's place.
func report(context *rule.Context, call *ast.CallExpr, selector *ast.SelectorExpr, receiver ast.Expr, method string) {
	// The replacement is the call pattern.NodeToAST builds: the bound receiver, the same node, so format prints it
	// from its own positions, a fresh identifier with no position, and no arguments.
	replacement := &ast.CallExpr{Fun: &ast.SelectorExpr{X: receiver, Sel: &ast.Ident{Name: method}}}
	fix := edit.Fix("Simplify conversion", edit.ReplaceWithNode(context.Fset, call, replacement))
	staticcheck_report.Report(context, call,
		"should use "+staticcheck_report.Render(context, selector.X)+"."+method+"() instead of "+
			staticcheck_report.Render(context, call),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(fix))
}
