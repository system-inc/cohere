// Package redundant_slice_length is staticcheck's S1010, a slice expression whose high index is the length of the
// value it slices, `s[a:len(s)]`, which is `s[a:]` written longer, ported as a check on types and syntax that reports
// through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1010/s1010.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the high index. The original matches its one query,
// `(SliceExpr x@(Object _) low (CallExpr (Builtin "len") [x]) nil)`, through staticcheck's pattern language; the port
// asks the same of each slice expression directly, reading pattern/match.go for what each construct of the query
// matches, and reports through staticcheck_report.Report where the original reports through report.Report. The
// registry still runs the original, which stays the oracle: the parity tests beside this file run both over the same
// packages, and the two differ nowhere they hold.
package redundant_slice_length

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, since this check filters them.
var Analyzer = &analysis.Analyzer{
	Name:     "S1010",
	Doc:      "Flags a slice expression whose high index is the length of what it slices: staticcheck's S1010, ported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every slice expression of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("redundant_slice_length: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.SliceExpr)(nil)}, func(node ast.Node) {
		slice, isSlice := node.(*ast.SliceExpr)
		if !isSlice {
			// Invariant: Preorder hands this function only the slice expressions it was asked for.
			panic("redundant_slice_length: the inspector handed over a node that is not a slice expression")
		}
		Check(context, slice)
	})
	return nil, nil
}

// Check judges one slice expression: one of two indexes whose high index calls the builtin len on the very variable
// it slices, named by an identifier, says what the default already is, and the fix deletes the high index.
func Check(context *rule.Context, slice *ast.SliceExpr) {
	// A third index sets the capacity, and the high index of a three-index slice can't be left out.
	if slice.Max != nil {
		return
	}
	// `(Object _)` matches an identifier alone, parenthesized or not, as the pattern sees through parentheses: a
	// selector, an index or a call is never one, even where its length would be the default.
	sliced, isIdentifier := ast.Unparen(slice.X).(*ast.Ident)
	if !isIdentifier {
		return
	}
	// The binding holds the identifier's object, and a nil object is one the pattern's second x never equals.
	slicedObject := context.TypesInfo.ObjectOf(sliced)
	if slicedObject == nil {
		return
	}
	// The low index is bound to nothing the query reads again, so any low index, or none, is the same here.
	call, isCall := ast.Unparen(slice.High).(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 || !isBuiltinLength(context, call.Fun) {
		return
	}
	if !namesObject(context, call.Args[0], slicedObject) {
		return
	}
	// The finding and the deletion span the high index as written, its parentheses included.
	staticcheck_report.Report(context, slice.High,
		"should omit second index in slice, s[a:len(s)] is identical to s[a:]",
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(edit.Fix("Simplify slice expression", edit.Delete(slice.High))))
}

// isBuiltinLength is whether function names the predeclared len, as the pattern's `(Builtin "len")` matches it: an
// identifier spelled len, parenthesized or not, whose object is the universe's, so a len the code declares itself,
// in a package or a function, is not it.
func isBuiltinLength(context *rule.Context, function ast.Expr) bool {
	identifier, isIdentifier := ast.Unparen(function).(*ast.Ident)
	if !isIdentifier || identifier.Name != "len" {
		return false
	}
	return context.TypesInfo.ObjectOf(identifier) == types.Universe.Lookup("len")
}

// namesObject is whether argument names object, as the pattern recalls the object a binding holds: an identifier
// whose object it is, or a selector whose selected name's object it is, parenthesized or not. The objects are compared
// by identity, so two variables spelled alike are two, and a package's variable named through a dot import and
// through its package name is one.
func namesObject(context *rule.Context, argument ast.Expr, object types.Object) bool {
	switch argument := ast.Unparen(argument).(type) {
	case *ast.Ident:
		return context.TypesInfo.ObjectOf(argument) == object
	case *ast.SelectorExpr:
		return context.TypesInfo.ObjectOf(argument.Sel) == object
	default:
		return false
	}
}
