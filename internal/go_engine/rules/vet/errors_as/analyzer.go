// Package errors_as is vet's errorsas pass, a call to errors.As whose second argument is not a pointer to a type
// implementing error, or to an interface type: errors.As panics on such a target, and on a pointer to error, which
// every error matches, it says nothing a plain assignment would not. A target of type any is always allowed, since it
// is often a value handed on from somewhere else. It sits on the engine's syntax dispatcher, hearing each call on the
// walk the dispatcher makes once for every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/errorsas/errorsas.go, whose logic
// it keeps and whose messages it reports word for word, over the same range. The original finds its calls through
// x/tools' internal typeindex analyzer, which the port cannot import, so it hears every call instead, keeping those
// whose callee, by typeutil.Callee, is errors.As, as typeindex's Calls does; both visit the calls in preorder. The
// registry runs the port in the original's place, and the original stays the oracle: the parity tests beside this
// file run both over the same packages. The two agree everywhere, so there is no differences fixture.
package errors_as

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "errorsas"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "report passing non-pointer or non-error values to errors.As\n\n" +
	"The errorsas analyzer reports calls to errors.As where the type\n" +
	"of the second argument is not a pointer to a type implementing error.\n" +
	"For example:\n\n" +
	"\tvar unwrappedErr net.DNSError\n" +
	"\terrors.As(err, unwrappedErr) // should use &unwrappedErr, DNSError.Error has a pointer receiver\n"

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/errorsas"

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, the order typeindex's
// Calls yields them in, of which it judges the calls to errors.As, outside the packages the original leaves alone.
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

// packageGate is what the port asks once of a package, in the context's state: whether the package is one the
// original leaves alone, as it asks before it walks anything.
type packageGate struct {
	// decided is whether skipped has been asked of the package yet.
	decided bool
	// skipped is its answer, once decided.
	skipped bool
}

// checkCall reports node when it calls errors.As with a target the original refuses, in a package the original
// judges. The dispatcher hands over each call in preorder, the order typeindex's Calls yields them in, since the
// identifier naming errors.As sits in a call's function, before any call among its arguments.
func checkCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	gate := rule.State[packageGate](context)
	if !gate.decided {
		switch context.Package.Path() {
		case "errors", "errors_test":
			// These packages know how to use their own APIs. Sometimes they are testing what happens to incorrect
			// programs. A fixture cannot reach this case: a fixture package at the path errors is shadowed by the
			// standard library's, so the whole-tree run over the standard library is what holds it.
			gate.skipped = true
		}
		gate.decided = true
	}
	if gate.skipped {
		return
	}
	if !callsErrorsAs(context, call) {
		return
	}
	if len(call.Args) < 2 {
		// A spread call, `errors.As(pair())`, whose second argument is a result the call's one argument returns.
		return
	}
	message := targetProblem(context.TypesInfo, call.Args[1])
	if message == "" {
		return
	}
	context.Report(analysis.Diagnostic{Pos: call.Pos(), End: call.End(), Message: message})
}

// callsErrorsAs reports whether call calls the standard library's errors.As, through a qualified identifier, a dot
// import or parentheses, and not through a variable holding it. x/tools' typeindex Index.Calls of
// Index.Object("errors", "As"): the callee typeutil.Callee resolves is the object package errors declares as As in
// its scope, a look recorded in the context's reads (#b2t9xmf).
func callsErrorsAs(context *rule.Context, call *ast.CallExpr) bool {
	callee := typeutil.Callee(context.TypesInfo, call)
	// A builtin's object has no package, and a conversion has no callee.
	if callee == nil || callee.Pkg() == nil || callee.Pkg().Path() != "errors" {
		return false
	}
	return context.LookupIn(callee.Pkg(), "As") == callee
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
