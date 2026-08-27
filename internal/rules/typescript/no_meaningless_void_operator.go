package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utilities/type_checking"
)

// NoMeaninglessVoidOperatorOptions is the rule's option surface.
type NoMeaninglessVoidOperatorOptions struct {
	// CheckNever extends the rule to an argument of type `never`, as a SUGGESTION rather than a fix.
	//
	// It defaults to FALSE, so unlike the sibling rule in this package the generic decoder happens
	// to produce the right zero value. The decoder below is still hand-rolled, because relying on
	// that coincidence means the next person to add a default-true key inherits a decoder that
	// silently does the wrong thing.
	CheckNever bool
}

// DefaultNoMeaninglessVoidOperatorSettings is upstream's `defaultOptions`.
func DefaultNoMeaninglessVoidOperatorSettings() NoMeaninglessVoidOperatorOptions {
	return NoMeaninglessVoidOperatorOptions{CheckNever: false}
}

// noMeaninglessVoidOperatorRawOptions is the wire shape, with a pointer so an absent key stays
// distinguishable from an explicit false.
type noMeaninglessVoidOperatorRawOptions struct {
	CheckNever *bool `json:"checkNever"`
}

// DecodeNoMeaninglessVoidOperatorOptions reads the rule's configuration.
//
// verify's config layer strips ESLint's `[severity, options]` tuple before dispatch, so what
// arrives is the bare object rather than upstream's one-element array.
func DecodeNoMeaninglessVoidOperatorOptions(raw []byte) (any, error) {
	decoded, err := rule.DecodeOptionsInto[noMeaninglessVoidOperatorRawOptions]()(raw)
	if err != nil {
		return DefaultNoMeaninglessVoidOperatorSettings(), err
	}

	wire, _ := decoded.(noMeaninglessVoidOperatorRawOptions)
	options := DefaultNoMeaninglessVoidOperatorSettings()
	if wire.CheckNever != nil {
		options.CheckNever = *wire.CheckNever
	}
	return options, nil
}

// NoMeaninglessVoidOperator flags `void` applied to something that is already void or undefined.
//
//	valid:   function bar(x: number) { void x; }        the value discarded is a number
//	valid:   const a = void 0;                          `0` is a number, so this discards something
//	valid:   function bar(x: never) { void x; }         unless checkNever is on
//	invalid: function foo() {} void foo();              foo() is already void
//	invalid: void (() => {})();
//
// `void` exists to say "this expression returns something and I am deliberately throwing it away".
// Applied to an expression that already evaluates to `void` or `undefined` it says nothing, so the
// reader is left looking for a discarded value that was never there.
//
// # Two branches, and the SAME repair is a fix in one and a suggestion in the other
//
// That distinction is the most important thing in this rule and it is easy to flatten.
//
// When every constituent of the argument's type is `void` or `undefined`, the repair is a FIX:
// removing the operator cannot change what the program does, because the expression already
// evaluated to the value `void` would have produced.
//
// When `checkNever` is on and every constituent is `void`, `undefined` or `never`, the repair is a
// SUGGESTION. `never` means control does not return from that expression at all, so removing the
// operator is a judgment about code the type system says is unreachable, and upstream declines to
// make it unattended. Shipping that as a fix would have the edit engine rewriting it silently.
//
// The two tests are nested rather than exclusive, and the order matters: `void | never` under
// `checkNever` satisfies the FIRST predicate, because a union's `every` is over its constituents and
// `never` is absorbed out of a union by the checker before the rule sees it. Measured on the
// installed build: `declare const x: void | never; void x;` reports with a fix, not a suggestion.
//
// # The message interpolates the type, and the corpus never checks it
//
// `{{type}}` is `checker.typeToString(argType)`, so the same rule reports "on void", "on undefined",
// "on never" and "on void | undefined" for different inputs. Upstream's corpus carries five cases
// and not one of them asserts the `data` field, so nothing there can see the interpolation at all.
// Every fixture in this package asserts the whole rendered string, because a message-id assertion
// cannot see a format slot that is filled with the wrong thing.
//
// # What the fix removes, including a comment
//
// Upstream removes the range from the start of the first token to the start of the SECOND, which is
// the `void` keyword plus everything between it and the argument. That is whitespace usually, and
// it is not always:
//
//	void  foo()          removes "void  "        two spaces
//	void /* c */ foo()   removes "void /* c */ " the comment goes with it
//	void(foo())          removes "void"          no separator to take
//
// The comment case is upstream's behavior rather than an accident of this port, measured directly,
// and it is reproduced. A repair that kept the comment would be a difference the differential
// harness can see, and the comment is attached to an operator that is being deleted anyway.
//
// # Cost
//
// `KindVoidExpression` is a rare anchor and the checker is consulted only when one is found.
var NoMeaninglessVoidOperator = rule.Rule{
	Name: "@typescript-eslint/no-meaningless-void-operator",

	// The argument's type decides every finding, so the checker is required.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings, isSettings := options.(NoMeaninglessVoidOperatorOptions)
		if !isSettings {
			settings = DefaultNoMeaninglessVoidOperatorSettings()
		}

		// everyConstituentHasFlag is upstream's `unionConstituents(...).every(isTypeFlagSet(...))`.
		// A non-union is its own single constituent, so this reads the same for both shapes.
		everyConstituentHasFlag := func(argumentType *checker.Type, flags checker.TypeFlags) bool {
			for _, constituent := range type_checking.UnionTypeParts(argumentType) {
				if !type_checking.IsTypeFlagSet(constituent, flags) {
					return false
				}
			}
			return true
		}

		return rule.Listeners{
			ast.KindVoidExpression: func(node *ast.Node) {
				if ctx.TypeChecker == nil {
					return
				}

				argument := node.AsVoidExpression().Expression
				if argument == nil {
					return
				}

				argumentType := ctx.TypeChecker.GetTypeAtLocation(argument)
				if argumentType == nil {
					return
				}

				// The removal range: from the `void` keyword's own start to the argument's start,
				// which is upstream's "first token start to second token start".
				//
				// `rule.TokenRange` supplies the argument's start rather than `Pos()`, because
				// `Pos()` includes leading trivia and would put the range's END inside the
				// whitespace, leaving a stray fragment behind. That is the fix hazard the span
				// hazard is usually described as, arriving through the other side of the range.
				voidKeyword := scanner.GetRangeOfTokenAtPosition(ctx.SourceFile, node.Pos())
				removal := core.NewTextRange(voidKeyword.Pos(), rule.TokenRange(ctx.SourceFile, argument).Pos())

				// Upstream interpolates `checker.typeToString(argType)`, which is this method
				// rather than the shelf's `GetTypeName`.
				//
				// Swapping the two is an EQUIVALENT mutation here, and the sweep says so: no input
				// this rule reports on can distinguish them. `GetTypeName` differs only by
				// collapsing string-like types to the word "string", and it delegates to
				// `TypeToString` for everything else, while this rule reports only when every
				// constituent is void, undefined or never. Measured over the six argument types
				// that reach a report, including the union: both spellings agree on all six.
				//
				// `TypeToString` is kept because it is what upstream calls and because the
				// equivalence is a property of which types get here rather than of the two
				// functions. A later widening of the type test would make the difference real, and
				// `GetTypeName` would then start naming a type the reader never wrote.
				typeName := ctx.TypeChecker.TypeToString(argumentType)

				if everyConstituentHasFlag(argumentType, checker.TypeFlagsVoid|checker.TypeFlagsUndefined) {
					// Removing the operator cannot change behavior here, so it is applied
					// unattended.
					ctx.ReportNodeWithFixes(node, buildMeaninglessVoidOperatorMessage(typeName),
						rule.RemoveRange(removal))
					return
				}

				if settings.CheckNever && everyConstituentHasFlag(argumentType,
					checker.TypeFlagsVoid|checker.TypeFlagsUndefined|checker.TypeFlagsNever) {
					// `never` means this expression does not return, so the same edit becomes a
					// judgment rather than a rewrite and a person chooses it.
					ctx.ReportNodeWithSuggestions(node, buildMeaninglessVoidOperatorMessage(typeName),
						rule.Suggestion{
							Message: buildRemoveVoidMessage(),
							Fixes:   []rule.Fix{rule.RemoveRange(removal)},
						})
				}
			},
		}
	},
}

func buildMeaninglessVoidOperatorMessage(typeName string) rule.Message {
	return rule.Message{
		Id: "meaninglessVoidOperator",
		Description: "void operator shouldn't be used on " + typeName +
			"; it should convey that a return value is being ignored",
	}
}

func buildRemoveVoidMessage() rule.Message {
	return rule.Message{
		Id:          "removeVoid",
		Description: "Remove 'void'",
	}
}
