// Package misplaced_error_result is staticcheck's `ST1008`, a function returning an error before its last result,
// ported onto the engine's syntax dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1008/st1008.go, whose message it reports
// word for word, at the misplaced result, with no range, as the original does, in generated files too, which the
// original does not filter. The original requires buildir only to list the package's source functions, SrcFuncs, and
// reads nothing of each but its signature. The port lists the same functions from the syntax instead: every function
// declaration, each of which the intermediate representation makes a function of (honnef.co/go/tools go/ir/create.go,
// membersFromDecl), and every function literal its builder builds, each an anonymous function of the one around it
// (go/ir/builder.go, expr0). The builder builds every literal but one inside an expression the type checker gave a
// constant value, such as `unsafe.Sizeof(func() {})`, which it takes as the constant and never descends into, so the
// port leaves such a literal out as well. The builder's other anonymous functions, the yield functions of range over a
// function, return a single bool, which the check never reports. Each signature is the type checker's own, the one the
// intermediate representation's function holds, so the run builds no intermediate representation for this check.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge.
package misplaced_error_result

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1008"

// Doc is the original's title.
const Doc = "A function's error value should be its last return value"

// Listener is what the port hears of each package from the dispatcher: every function declaration and every function
// literal, with the literal's ancestors, which say whether the original's builder ever builds it.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)},
	Node:  checkFunction,
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// checkFunction judges the signature of node, a function declaration or a function literal the original's builder
// builds.
func checkFunction(context *rule.Context, node ast.Node, stack []ast.Node) {
	switch function := node.(type) {
	case *ast.FuncDecl:
		declared, isFunction := context.TypesInfo.Defs[function.Name].(*types.Func)
		// The type checker defines a function for every declaration of a package that checks, as the intermediate
		// representation's own creation assumes, so this is never met.
		if !isFunction {
			return
		}
		checkSignature(context, declared.Signature())
	case *ast.FuncLit:
		if insideConstant(context, stack) {
			return
		}
		// The type the literal's builder gives its anonymous function, the type of the literal's function type.
		signature, isSignature := context.TypesInfo.TypeOf(function.Type).(*types.Signature)
		// A literal of a package that checks has a signature, so this is never met.
		if !isSignature {
			return
		}
		checkSignature(context, signature)
	}
}

// insideConstant is whether an expression among stack, a literal's ancestors, has a constant value, so the original's
// builder evaluates it as that constant and never builds the literal.
func insideConstant(context *rule.Context, stack []ast.Node) bool {
	for _, ancestor := range stack {
		expression, isExpression := ancestor.(ast.Expr)
		if isExpression && context.TypesInfo.Types[expression].Value != nil {
			return true
		}
	}
	return false
}

// checkSignature reports the last error among signature's results but its last, unless its last result is an error,
// or its last two are an error and a bool, the comma-ok shape the original accepts.
func checkSignature(context *rule.Context, signature *types.Signature) {
	results := signature.Results()
	if results == nil || results.Len() < 2 {
		return
	}
	// The types are compared as the original compares them, by identity with the universe's after any alias is
	// resolved, so a named type whose underlying type is error is no error here, as it is none there.
	errorType := types.Universe.Lookup("error").Type()
	booleanType := types.Universe.Lookup("bool").Type()
	last := results.Len() - 1
	if types.Unalias(results.At(last).Type()) == errorType {
		// The last result is an error, so errors in other positions are fine.
		return
	}
	if types.Unalias(results.At(last).Type()) == booleanType && types.Unalias(results.At(last-1).Type()) == errorType {
		// (..., error, bool) is taken for a comma-ok function, where it is not clear whether the bool should come last.
		return
	}
	for index := last - 1; index >= 0; index-- {
		if types.Unalias(results.At(index).Type()) == errorType {
			staticcheck_report.Report(context, results.At(index), "error should be returned as the last argument",
				staticcheck_report.ShortRange())
			return
		}
	}
}
