// Package errors_as is vet's errorsas pass, a call to errors.As whose second argument is not a pointer to a type
// implementing error, or to an interface type: errors.As panics on such a target, and on a pointer to error, which
// every error matches, it says nothing a plain assignment would not. A target of type any is always allowed, since it
// is often a value handed on from somewhere else.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/errorsas/errorsas.go, whose logic
// it keeps and whose messages it reports word for word, over the same range. The original finds its calls through
// x/tools' internal typeindex analyzer, which the port cannot import, so it requires inspect instead and walks every
// call, keeping those whose callee, by typeutil.Callee, is errors.As, as typeindex's Calls does; both visit the calls
// in preorder. The registry runs the port in the original's place, and the original stays the oracle: the parity
// tests beside this file run both over the same packages. The two agree everywhere, so there is no differences
// fixture.
package errors_as

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "errorsas",
	Doc: "report passing non-pointer or non-error values to errors.As\n\n" +
		"The errorsas analyzer reports calls to errors.As where the type\n" +
		"of the second argument is not a pointer to a type implementing error.\n" +
		"For example:\n\n" +
		"\tvar unwrappedErr net.DNSError\n" +
		"\terrors.As(err, unwrappedErr) // should use &unwrappedErr, DNSError.Error has a pointer receiver\n",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/errorsas",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// notAPointerMessage is the original's message for a target that is not a pointer, or points at a type that neither
// implements error nor is an interface.
const notAPointerMessage = "second argument to errors.As must be a non-nil pointer to either a type that implements " +
	"error, or to any interface type"

// pointerToErrorMessage is the original's message for a target of type *error.
const pointerToErrorMessage = "second argument to errors.As should not be *error"

var (
	anyType   = types.Universe.Lookup("any").Type()
	errorType = types.Universe.Lookup("error").Type()
)

func run(pass *analysis.Pass) (any, error) {
	switch pass.Pkg.Path() {
	case "errors", "errors_test":
		// These packages know how to use their own APIs. Sometimes they are testing what happens to incorrect
		// programs. A fixture cannot reach this case: a fixture package at the path errors is shadowed by the
		// standard library's, so the whole-tree run over the standard library is what holds it.
		return nil, nil
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("errors_as: inspect's result is missing")
	}
	// All visits each call in preorder, the order typeindex's Calls yields them in, since the identifier naming
	// errors.As sits in a call's function, before any call among its arguments.
	for call := range inspector.All[*ast.CallExpr](inspected) {
		if !callsErrorsAs(pass.TypesInfo, call) {
			continue
		}
		if len(call.Args) < 2 {
			// A spread call, `errors.As(pair())`, whose second argument is a result the call's one argument returns.
			continue
		}
		message := targetProblem(pass.TypesInfo, call.Args[1])
		if message == "" {
			continue
		}
		pass.Report(analysis.Diagnostic{Pos: call.Pos(), End: call.End(), Message: message})
	}
	return nil, nil
}

// callsErrorsAs reports whether call calls the standard library's errors.As, through a qualified identifier, a dot
// import or parentheses, and not through a variable holding it. x/tools' typeindex Index.Calls of
// Index.Object("errors", "As"): the callee typeutil.Callee resolves is the object package errors declares as As in
// its scope.
func callsErrorsAs(information *types.Info, call *ast.CallExpr) bool {
	callee := typeutil.Callee(information, call)
	// A builtin's object has no package, and a conversion has no callee.
	if callee == nil || callee.Pkg() == nil || callee.Pkg().Path() != "errors" {
		return false
	}
	return callee.Pkg().Scope().Lookup("As") == callee
}

// targetProblem is the original's message for target, the second argument to errors.As, and empty when target is
// allowed. x/tools' checkAsTarget, which returns the message as an error.
func targetProblem(information *types.Info, target ast.Expr) string {
	targetType := information.Types[target].Type
	if targetType == nil {
		// The type checker records a type for every argument of a call in a package that type-checks, the only kind
		// an analyzer without RunDespiteErrors is run on, so this cannot fire. The original would panic here.
		return ""
	}
	if types.Identical(targetType.Underlying(), anyType) {
		// A target of any is always allowed, since it often indicates a value forwarded from another source.
		return ""
	}
	pointer, isPointer := targetType.Underlying().(*types.Pointer)
	if !isPointer {
		return notAPointerMessage
	}
	if types.Identical(pointer.Elem(), errorType) {
		return pointerToErrorMessage
	}
	if !types.IsInterface(pointer.Elem()) && !types.AssignableTo(pointer.Elem(), errorType) {
		return notAPointerMessage
	}
	return ""
}
