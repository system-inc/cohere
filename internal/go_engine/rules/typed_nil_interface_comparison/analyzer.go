// Package typed_nil_interface_comparison is staticcheck's SA4023, an interface value compared with untyped nil where
// the comparison can never come out true or always does: the value was made from a concrete value, or it is a result
// of a function that never returns a nil interface but may return one holding a typed nil. It is ported onto the
// engine's own intermediate representation and reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4023/sa4023.go, whose logic it keeps and
// whose messages and related places it reports word for word, through staticcheck_report.Report and Related at the
// comparison's syntax, asking staticcheck_report.HasRange and Ordinal and staticcheck_code.IsInTest where the original
// asks report.HasRange, report.Ordinal and code.IsInTest, each the port of the one the original calls, proven against
// it beside it. It reads what each function returns from return_nilness, the port of staticcheck's nilness facts,
// which cross from every dependency, through the context, its listener requiring return_nilness, asks Flatten and
// IsTrivial of the engine's intermediate representation where the original asks irutil's, and asks
// golang.org/x/exp/typeparams and honnef.co/go/tools/go/types/typeutil what the original asks them, public packages and
// not framework.
//
// The original walks every source function of a package in one run but keeps nothing from one comparison to the next,
// so the port judges one function at a time and finds what the original finds. Like the original, it judges only a
// comparison whose left side is the interface and whose right side is nil, so `nil == value` is never flagged, a gap
// the original names as work left (parity's `nil == typedNil()`), and generic code is judged without its
// instantiations, so `any(value)` of a type parameter is flagged only when every type the parameter allows is
// concrete (parity's genericAny and genericConcrete), and a generic function returning it is judged the same for
// every instantiation (parity's generic). The original's second message, for a value made into the interface that
// has no syntax to point a related place at, is kept, but no fixture reaches it: the engine's intermediate
// representation, like staticcheck's, gives every value it builds from source code a source, zero values, comma-ok
// forms and range variables included (parity's zeroValues and madeFromOtherValues). The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, related places included, and the two differ nowhere they hold.
package typed_nil_interface_comparison

import (
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/exp/typeparams"
	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nilness"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher over the engine's own intermediate
// representation: every function, each judged on its own with the nilness facts of the package. It requires
// return_nilness, whose result it reads.
var Listener = rule.Listener{
	Requires: []*analysis.Analyzer{return_nilness.Analyzer},
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{
		Function: func(context *rule.Context, function *intermediate_representation.Function) {
			nilnesses, isNilnesses := context.ResultOf(return_nilness.Analyzer).(*return_nilness.Result)
			if !isNilnesses {
				// Invariant: the listener requires return_nilness, and the dispatcher hands over its result, of
				// the type it declares.
				panic("typed_nil_interface_comparison: return_nilness's result is missing")
			}
			Check(context, nilnesses, function)
		},
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4023",
	"SA4023",
	"Flags a comparison of an interface value with untyped nil that can never be true or is always true: "+
		"staticcheck's SA4023, ported onto the engine's own intermediate representation.",
	&Listener,
)

// Check judges one function: each comparison by == or != of an interface value with untyped nil, where the value was
// made from a concrete value, or is a result of a function nilnesses says never returns a nil interface but may
// return one holding a typed nil, is reported.
func Check(
	context *rule.Context,
	nilnesses *return_nilness.Result,
	function *intermediate_representation.Function,
) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			comparison, isBinary := instruction.(*intermediate_representation.BinaryOperation)
			if !isBinary || comparison.Operator != token.EQL && comparison.Operator != token.NEQ {
				continue
			}
			checkComparison(context, nilnesses, comparison)
		}
	}
}

// checkComparison reports comparison when its left side is an interface value that can never be nil, as Check says.
func checkComparison(
	context *rule.Context,
	nilnesses *return_nilness.Result,
	comparison *intermediate_representation.BinaryOperation,
) {
	// A type parameter's core may be an interface, but a value of it is no interface value.
	if !types.IsInterface(comparison.X.Type()) || typeparams.IsTypeParam(comparison.X.Type()) {
		return
	}
	// With an interface on the left, a constant on the right is untyped: a typed nil would first have been made into
	// an interface value, which is no constant.
	constant, isConstant := comparison.Y.(*intermediate_representation.Constant)
	if !isConstant || !constant.IsNil() {
		return
	}
	qualifier := "always"
	if comparison.Operator == token.EQL {
		qualifier = "never"
	}

	var callee *types.Func
	resultIndex := 0
	switch value := intermediate_representation.Flatten(comparison.X).(type) {
	case *intermediate_representation.Call:
		callee = staticCalleeObject(value)
	case *intermediate_representation.Extract:
		call, isCall := intermediate_representation.Flatten(value.Tuple).(*intermediate_representation.Call)
		if !isCall {
			return
		}
		callee = staticCalleeObject(call)
		resultIndex = value.Index
	case *intermediate_representation.MakeInterface:
		terms, termsError := typeparams.NormalTerms(value.X.Type())
		if len(terms) == 0 || termsError != nil {
			// A type parameter with no terms, or whose terms cannot be told, may be an interface type, whose nil
			// value makes a nil interface.
			return
		}
		if staticcheck_report.HasRange(value.X) {
			staticcheck_report.Report(context, comparison, fmt.Sprintf("this comparison is %s true", qualifier),
				staticcheck_report.Related(value.X,
					"the lhs of the comparison gets its value from here and has a concrete type"))
			return
		}
		// With no syntax to point the related place at, the message itself says where the value came from.
		staticcheck_report.Report(context, comparison, fmt.Sprintf(
			"this comparison is %s true; the lhs of the comparison has been assigned a concretely typed value",
			qualifier))
		return
	}
	if callee == nil {
		return
	}

	returned := nilnesses.Nilness(callee, resultIndex)
	// Flagged: an interface value never nil itself that may hold, or always holds, a typed nil, which points to a bug
	// in the function returning it. A value never nil that holds no nil either, as errors.New returns, is left alone:
	// comparing it is more likely defensive, guarding against a future untyped nil.
	if returned.Outer != return_nilness.NeverNil || returned.Inner != return_nilness.MaybeNil &&
		returned.Inner != return_nilness.AlwaysNil {
		return
	}
	// A test may be enforcing on purpose that a value is not nil.
	if staticcheck_code.IsInTest(context, comparison) {
		return
	}
	if intermediate_representation.IsTrivial(comparison.Parent().Program.FunctionValue(callee)) {
		return
	}
	staticcheck_report.Report(context, comparison, fmt.Sprintf("this comparison is %s true", qualifier),
		staticcheck_report.Related(comparison.X, fmt.Sprintf(
			"the lhs of the comparison is the %s return value of this function call",
			staticcheck_report.Ordinal(resultIndex+1))),
		staticcheck_report.Related(callee,
			fmt.Sprintf("%s never returns a nil interface value", typeutil.FuncName(callee))))
}

// staticCalleeObject is the function call statically calls, or nil for a dynamic call or a callee with no function
// object, such as a function literal or a wrapper staticcheck synthesizes.
func staticCalleeObject(call *intermediate_representation.Call) *types.Func {
	callee := call.Call.StaticCallee()
	if callee == nil {
		return nil
	}
	object, _ := callee.Object().(*types.Func) // Ignored: a callee whose object is no function is judged as none.
	return object
}
