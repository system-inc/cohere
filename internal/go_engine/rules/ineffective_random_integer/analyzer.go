// Package ineffective_random_integer is staticcheck's SA4030, a call such as rand.Intn(1), whose bound excludes itself
// so the call always returns 0, ported as a check on syntax that reports through the kit every port of staticcheck
// shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4030/sa4030.go, whose message it reports
// word for word, over the whole call, through staticcheck_report.Report with no option, as the original reports through
// report.Report: a generated file is reported too. The message writes the call as staticcheck's report.Render writes
// it, which stays staticcheck's: the port calls it with a pass holding only the file set, all it reads. The original
// matches the pattern `(CallExpr (Symbol name@(Or ...)) [(IntegerLiteral "1")])` through code.Matches, which for a
// pattern rooted at a call of a symbol walks the calls staticcheck's type index records for each symbol named, those
// whose function, through parentheses, a selector and an instantiation, uses the symbol's object, rather than every
// call. The port walks every call and asks the same of each: staticcheck_code.IsSymbol, the port of the pattern's
// Symbol, takes its function for one of the nineteen, and it has exactly one argument, which
// staticcheck_code.IntegerLiteral, the port of the pattern's IntegerLiteral, takes for a literal whose constant prints
// as 1. Every call the pattern accepts is one the index records, since both read the object the type checker recorded
// for the function's identifier, so the two walks reach the same findings, in another order. The pattern has no field
// for a call's ellipsis, so neither does the port. The original keeps nothing from one call to the next, and stays the
// oracle: the parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package ineffective_random_integer

import (
	"fmt"
	"go/ast"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/report"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA4030",
	Doc: "Flags a call such as rand.Intn(1), which always returns 0: staticcheck's SA4030, ported as a check on " +
		"syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// messageFormat is the original's message, word for word, around the function's full name and the call as written.
const messageFormat = "%s(n) generates a random value 0 <= x < n; that is, the generated values don't include n; %s " +
	"therefore always returns 0"

// boundedFunctions are the full names of the functions the original names, each returning a value from 0 up to and
// excluding its one argument.
var boundedFunctions = []string{
	"math/rand.Int31n",
	"math/rand.Int63n",
	"math/rand.Intn",
	"(*math/rand.Rand).Int31n",
	"(*math/rand.Rand).Int63n",
	"(*math/rand.Rand).Intn",

	"math/rand/v2.Int32N",
	"math/rand/v2.Int64N",
	"math/rand/v2.IntN",
	"math/rand/v2.N",
	"math/rand/v2.Uint32N",
	"math/rand/v2.Uint64N",
	"math/rand/v2.UintN",

	"(*math/rand/v2.Rand).Int32N",
	"(*math/rand/v2.Rand).Int64N",
	"(*math/rand/v2.Rand).IntN",
	"(*math/rand/v2.Rand).Uint32N",
	"(*math/rand/v2.Rand).Uint64N",
	"(*math/rand/v2.Rand).UintN",
}

// run builds the pass's context and judges every call of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("ineffective_random_integer: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(node ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: Preorder hands this function only the calls it was asked for.
			panic("ineffective_random_integer: the inspector handed over a node that is not a call")
		}
		Check(context, call)
	})
	return nil, nil
}

// Check judges one call: one of the bounded functions called with the literal 1 alone.
func Check(context *rule.Context, call *ast.CallExpr) {
	if len(call.Args) != 1 {
		return
	}
	argument, isLiteral := staticcheck_code.IntegerLiteral(context, call.Args[0])
	// The pattern compares the constant as it prints, so 0x1, +1 and -(-1) are 1 too.
	if !isLiteral || argument.Value.String() != "1" {
		return
	}
	// The pattern's Or tries each name in turn, and binds the one the function is.
	index := slices.IndexFunc(boundedFunctions, func(name string) bool {
		return staticcheck_code.IsSymbol(context, call.Fun, name)
	})
	if index < 0 {
		return
	}
	name := boundedFunctions[index]
	rendered := report.Render(&analysis.Pass{Fset: context.Fset}, call)
	staticcheck_report.Report(context, call, fmt.Sprintf(messageFormat, name, rendered))
}
