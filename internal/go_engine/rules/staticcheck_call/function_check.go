// Package staticcheck_call judges the arguments of calls to named functions, the framework that nineteen of
// staticcheck's SA1 checks and five others ride: a check is a map from a function's name to a Rule, the Rule reads
// each call to that function in staticcheck's intermediate representation and marks an argument or the call itself
// invalid, and the kit reports each mark where staticcheck would.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 analysis/callcheck/callcheck.go, whose walk, argument
// list and report positions it keeps. It differs in three places. A Call carries the rule.Context the check reads in
// place of the analysis pass, so a Rule reads the package, its type information and its file set there. An Argument
// holds its ir.Value directly, where the original wraps it in a struct of that one field. And where the original
// panics, on a call whose syntax is of a shape it does not know or a mark it has nowhere to report, the kit reports
// at the next position it has, or drops the mark when there is none (report), since a panic loses every other
// finding of the package along with the one it could not place.
package staticcheck_call

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Rule judges one call to the function it is registered under, marking what is invalid through Call.Invalid and
// Argument.Invalid.
type Rule func(call *Call)

// FunctionCheck is the check that hands every call in a function to the Rule rules holds under the called function's
// name, as typeutil.FuncName spells it ("regexp.Compile", "(*bytes.Buffer).Write"), and reports what the Rule marks.
// Only a call whose callee is known without running the program, a function or a method called on a concrete type,
// is judged.
func FunctionCheck(rules map[string]Rule) rule.FunctionCheck {
	return func(context *rule.Context, function *ir.Function) {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instrs {
				site, isCall := instruction.(ir.CallInstruction)
				if !isCall {
					continue
				}
				if callee := site.Common().StaticCallee(); callee != nil {
					checkCall(context, rules, site, callee)
				}
			}
		}
	}
}

// checkCall hands site, a call to callee, to its Rule, if rules holds one, and reports what the Rule marks.
func checkCall(context *rule.Context, rules map[string]Rule, site ir.CallInstruction, callee *ir.Function) {
	object, isFunction := callee.Object().(*types.Func)
	if !isFunction {
		return
	}
	check, isRuled := rules[typeutil.FuncName(object)]
	if !isRuled {
		return
	}
	values := site.Common().Args
	if callee.Signature.Recv() != nil {
		values = values[1:]
	}
	arguments := []*Argument{}
	for _, value := range values {
		// A value passed as an interface is judged as what was converted, the constant and not the interface
		// holding it.
		if conversion, isConversion := value.(*ir.MakeInterface); isConversion {
			arguments = append(arguments, &Argument{Value: conversion.X})
			continue
		}
		arguments = append(arguments, &Argument{Value: value})
	}
	call := &Call{Context: context, Instruction: site, Arguments: arguments, Parent: site.Parent()}
	check(call)

	syntax := callSyntax(site)
	for index, argument := range call.Arguments {
		for _, message := range argument.invalids {
			switch {
			case syntax == nil || len(syntax.Args) == 0:
				// A call with no syntax, or one with none for its arguments, a variadic function called with
				// nothing, has no argument to point at, so the mark sits on the call. The original indexes the
				// first argument there and panics.
				report(context, site, message)
			case index < len(syntax.Args):
				reportAt(context, syntax.Args[index].Pos(), syntax.Args[index].End(), message)
			default:
				// This is f(g()) where g returns several values: report at the next best position, the first
				// argument. staticcheck's checkEncodingBinaryRules is one check that marks such an argument.
				reportAt(context, syntax.Args[0].Pos(), syntax.Args[0].End(), message)
			}
		}
	}
	for _, message := range call.invalids {
		report(context, call.Instruction, message)
	}
}

// callSyntax is the call expression site was built from, or nil when it has none. The builder gives a call the call
// expression, or the go or defer statement around it, and a call it makes itself, such as the call of a function
// ranged over, no syntax at all. The original panics on any other shape; there is none today, and should the builder
// make one, the call's marks are reported at the call (report) rather than losing the package's other findings.
func callSyntax(site ir.CallInstruction) *ast.CallExpr {
	switch source := site.Source().(type) {
	case *ast.CallExpr:
		return source
	case *ast.DeferStmt:
		return source.Call
	case *ast.GoStmt:
		return source.Call
	}
	return nil
}

// report reports message at instruction, over the range of the syntax it was built from, as staticcheck's
// report.Report reports at an instruction. An instruction with no syntax has nowhere a reader could be sent, and
// the original panics there, so the mark is dropped.
func report(context *rule.Context, instruction ir.Instruction, message string) {
	source := instruction.Source()
	if source == nil {
		return
	}
	reportAt(context, source.Pos(), source.End(), message)
}

// reportAt reports message over the range from position to end.
func reportAt(context *rule.Context, position token.Pos, end token.Pos, message string) {
	context.Report(analysis.Diagnostic{Pos: position, End: end, Message: message})
}
