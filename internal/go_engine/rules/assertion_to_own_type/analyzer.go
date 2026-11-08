// Package assertion_to_own_type is staticcheck's S1040, a type assertion `x.(T)` to an interface type T that x
// already has, which can fail only when x is nil, ported as a check on types and syntax that reports through the kit
// every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1040/s1040.go, whose logic it keeps and whose
// message it reports word for word, at the same range, the whole assertion. The original uses no pattern: it walks
// the type assertions, skips a type switch's `x.(type)`, and asks go/types whether the asserted type is an interface
// identical to the asserted value's. The port asks the same of each type assertion, renders the message's code with
// go/format over the package's file set as staticcheck's report.Render does, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry still runs the original,
// which stays the oracle: the parity tests beside this file run both over the same packages, and the two differ
// nowhere they hold.
package assertion_to_own_type

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, since this check filters them.
var Analyzer = &analysis.Analyzer{
	Name:     "S1040",
	Doc:      "Flags a type assertion to the interface type its value already has: staticcheck's S1040, ported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every type assertion of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("assertion_to_own_type: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.TypeAssertExpr)(nil)}, func(node ast.Node) {
		assertion, isAssertion := node.(*ast.TypeAssertExpr)
		if !isAssertion {
			// Invariant: Preorder hands this function only the type assertions it was asked for.
			panic("assertion_to_own_type: the inspector handed over a node that is not a type assertion")
		}
		Check(context, assertion)
	})
	return nil, nil
}

// Check judges one type assertion: one whose asserted type is an interface identical to its value's static type can
// fail only on a nil value, and is reported at the whole assertion with both rendered as written.
func Check(context *rule.Context, assertion *ast.TypeAssertExpr) {
	// A type switch's `x.(type)` has no type, and the original leaves type switches alone.
	if assertion.Type == nil {
		return
	}
	asserted := context.TypesInfo.TypeOf(assertion.Type)
	value := context.TypesInfo.TypeOf(assertion.X)
	// A package that type-checks records both, and one that doesn't is no place to judge; the original would panic
	// in types.IsInterface on a missing asserted type.
	if asserted == nil || value == nil {
		return
	}
	// types.IsInterface reads the underlying type, so a type parameter, whose underlying type is its constraint,
	// counts as an interface, and types.Identical then holds it apart from any value's type but its own, which a
	// type-checked assertion never has. A named interface and an interface literal of the same methods are not
	// identical, an alias is the type it names, and two instantiations of a generic interface are identical when
	// their type arguments are.
	if !types.IsInterface(asserted) || !types.Identical(asserted, value) {
		return
	}
	staticcheck_report.Report(context, assertion,
		fmt.Sprintf("type assertion to the same type: %s already has type %s", rendered(context, assertion.X),
			rendered(context, assertion.Type)),
		staticcheck_report.FilterGenerated())
}

// rendered is node printed by go/format over the package's file set, as staticcheck's report.Render prints it: the
// source reformatted, over the lines it spans, a doubled parenthesis printed once, and of its comments only those the
// syntax holds itself, as an interface's method holds its doc and trailing comments.
func rendered(context *rule.Context, node ast.Node) string {
	text := &bytes.Buffer{}
	formatError := format.Node(text, context.Fset, node)
	if formatError != nil {
		// Invariant: go/format prints every expression the parser built, and report.Render panics the same way.
		panic(fmt.Sprintf("assertion_to_own_type: rendering the assertion's code: %v", formatError))
	}
	return text.String()
}
