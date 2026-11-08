// Package assertion_to_own_type is staticcheck's S1040, a type assertion `x.(T)` to an interface type T that x
// already has, which can fail only when x is nil, ported as a check on types and syntax that reports through the kit
// every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1040/s1040.go, whose logic it keeps and whose
// message it reports word for word, at the same range, the whole assertion. The original uses no pattern: it walks
// the type assertions, skips a type switch's `x.(type)`, and asks go/types whether the asserted type is an interface
// identical to the asserted value's. The port asks the same of each type assertion, renders the message's code with
// go/format over the package's file set as staticcheck's report.Render does, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere they hold.
package assertion_to_own_type

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every type assertion, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.TypeAssertExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		assertion, isAssertion := node.(*ast.TypeAssertExpr)
		if !isAssertion {
			// Invariant: the dispatcher hands Node only the type assertions it was asked for.
			panic("assertion_to_own_type: the dispatcher handed over a node that is not a type assertion")
		}
		Check(context, assertion)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1040",
	"S1040",
	"Flags a type assertion to the interface type its value already has: staticcheck's S1040, ported.",
	&Listener,
)

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
