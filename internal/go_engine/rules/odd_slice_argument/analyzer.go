// Package odd_slice_argument is staticcheck's SA5012, a slice of odd length passed to a function that panics unless
// its slice has an even length, as strings.NewReplacer and logging APIs taking pairs do, ported onto staticcheck's
// intermediate representation as an analyzer that exports the facts it reads.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5012/sa5012.go, whose logic it keeps and
// whose message it reports word for word, at the same range, the argument. It runs in three passes over the package's
// functions, as the original does: it marks each slice parameter a function checks for even length on every path
// before panicking, then each slice parameter a function passes on every path to a parameter so marked, and then
// reports each call passing a slice of known odd length to a marked parameter. A mark is an object fact, of a type
// of the port's own that prints as the original's does, "needs even elements", so a function of another package is
// marked when that package was analyzed. A check on rule.FunctionCheck reads no facts, so this is a plain analyzer
// on staticcheck_ir.Analyzer, staticcheck's buildir, and generated, whose judgment the context reads though this
// check filters nothing. Where the original asks staticcheck's internal iterutil.All whether every return block is
// dominated, the port loops over them itself.
//
// It differs in one place, adjudicated: the original reads a call's source as a call expression, and the source of a
// go or defer statement's call is the statement, so a go or defer passing an odd slice to a marked parameter panics
// the original's run. The port reads the statement's call and reports the argument there, which
// TestPortReportsWhereTheOriginalPanics holds, running the original behind a recover and requiring that it panic. The
// registry still runs the original, which stays the oracle: the parity tests beside this file run both over the same
// packages, and the two differ nowhere else.
package odd_slice_argument

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port under the original's name, so the rule, its settings and every `//cohere:ignore` naming it
// read the same once the port takes the original's place. It exports evenElements facts, which its run on a package
// reads from its runs on the packages that one imports.
var Analyzer = &analysis.Analyzer{
	Name: "SA5012",
	Doc: "Flags a slice of odd length passed to a function that needs an even one: staticcheck's SA5012, ported onto " +
		"its intermediate representation.",
	Requires:  []*analysis.Analyzer{staticcheck_ir.Analyzer, generated.Analyzer},
	FactTypes: []analysis.Fact{new(evenElements)},
	Run:       run,
}

// run marks the package's parameters, directly and then through the calls that pass them on, and reports the odd
// slices passed to marked parameters.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	functions, functionsError := staticcheck_ir.Functions(pass)
	if functionsError != nil {
		return nil, functionsError
	}
	markLengthChecks(pass, functions)
	markIndirectLengthChecks(pass, functions)
	reportOddLengths(pass, context, functions)
	return nil, nil
}

// dominatesEveryReturn is whether every path through function to a return passes through block.
func dominatesEveryReturn(function *ir.Function, block *ir.BasicBlock) bool {
	for returned := range function.Returns() {
		if !block.Dominates(returned) {
			return false
		}
	}
	return true
}

// markLengthChecks marks each slice parameter a function checks on every path with `len(p) % 2 != 0` or
// `len(p) % 2 == 1`, either way around, and panics when the check holds.
func markLengthChecks(pass *analysis.Pass, functions []*ir.Function) {
	for _, function := range functions {
		for _, block := range function.Blocks {
			if !dominatesEveryReturn(function, block) {
				continue
			}
			branch, isIf := block.Control().(*ir.If)
			if !isIf {
				continue
			}
			comparison, isBinary := branch.Cond.(*ir.BinOp)
			if !isBinary {
				continue
			}
			var needle uint64
			switch comparison.Op {
			case token.NEQ:
				needle = 0
			case token.EQL:
				needle = 1
			default:
				continue
			}
			remainder, isRemainder := comparison.X.(*ir.BinOp)
			compared, isConstant := comparison.Y.(*ir.Const)
			// The original looks the other way around only when neither side is in place, so a remainder with no
			// constant beside it, or a constant with no remainder, is no check.
			if isRemainder != isConstant {
				continue
			}
			if !isRemainder {
				remainder, isRemainder = comparison.Y.(*ir.BinOp)
				compared, isConstant = comparison.X.(*ir.Const)
			}
			if !isRemainder || !isConstant || remainder.Op != token.REM || compared.Value.Kind() != constant.Int ||
				compared.Uint64() != needle {
				continue
			}
			divisor, isDivisorConstant := remainder.Y.(*ir.Const)
			if !isDivisorConstant || divisor.Value.Kind() != constant.Int || divisor.Uint64() != 2 {
				continue
			}
			length, isCall := remainder.X.(*ir.Call)
			if !isCall || !irutil.IsCallTo(length.Common(), "len") {
				continue
			}
			// Only a parameter read directly; one that has flowed through a sigma or a phi is not followed.
			parameter, isParameter := length.Call.Args[0].(*ir.Parameter)
			if !isParameter || !typeutil.All(parameter.Type(), typeutil.IsSlice) {
				continue
			}
			if _, panics := block.Succs[0].Control().(*ir.Panic); !panics {
				continue
			}
			pass.ExportObjectFact(parameter.Object(), new(evenElements))
		}
	}
}

// markIndirectLengthChecks marks each slice parameter a function passes on every path, as it is, to a parameter
// already marked, following the calls into functions of the same package first, so a chain of them is marked from
// its end.
func markIndirectLengthChecks(pass *analysis.Pass, functions []*ir.Function) {
	seen := map[*ir.Function]bool{}
	var visit func(function *ir.Function)
	visit = func(function *ir.Function) {
		if seen[function] {
			return
		}
		seen[function] = true
		for _, block := range function.Blocks {
			if !dominatesEveryReturn(function, block) {
				continue
			}
			for _, instruction := range block.Instrs {
				call, isCall := instruction.(*ir.Call)
				if !isCall {
					continue
				}
				callee := call.Call.StaticCallee()
				if callee == nil {
					continue
				}
				if callee.Pkg == function.Pkg || callee.Pkg == nil {
					visit(callee)
				}
				for argumentIndex, argument := range call.Call.Args {
					parameterIndex := argumentIndex
					if callee.Signature.Recv() != nil {
						if argumentIndex == 0 {
							continue
						}
						parameterIndex--
					}
					parameter, isParameter := argument.(*ir.Parameter)
					if !isParameter || !typeutil.All(parameter.Type(), typeutil.IsSlice) {
						continue
					}
					// The signature's parameters, not the callee's Params, which a function of another package,
					// built from export data, does not hold.
					if !pass.ImportObjectFact(callee.Signature.Params().At(parameterIndex), new(evenElements)) {
						continue
					}
					pass.ExportObjectFact(parameter.Object(), new(evenElements))
				}
			}
		}
	}
	for _, function := range functions {
		visit(function)
	}
}

// reportOddLengths reports each call passing a slice whose length is known and odd to a marked parameter.
func reportOddLengths(pass *analysis.Pass, context *rule.Context, functions []*ir.Function) {
	for _, function := range functions {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instrs {
				call, isCall := instruction.(ir.CallInstruction)
				if !isCall {
					continue
				}
				callee := call.Common().StaticCallee()
				if callee == nil {
					continue
				}
				for argumentIndex, argument := range call.Common().Args {
					parameterIndex := argumentIndex
					if callee.Signature.Recv() != nil {
						if argumentIndex == 0 {
							continue
						}
						parameterIndex--
					}
					if _, isSlice := argument.Type().Underlying().(*types.Slice); !isSlice {
						continue
					}
					parameter := callee.Signature.Params().At(parameterIndex)
					if !pass.ImportObjectFact(parameter, new(evenElements)) {
						continue
					}
					length := sliceLength(argument)
					if length < 0 || length%2 == 0 {
						continue
					}
					label := "argument"
					signature := call.Common().Signature()
					if parameterIndex == signature.Params().Len()-1 && signature.Variadic() {
						label = "variadic argument"
					}
					// The parameter has a name, since its function could not have checked its length otherwise.
					staticcheck_report.Report(context, callExpression(call).Args[parameterIndex],
						fmt.Sprintf("%s %q is expected to have even number of elements, but has %d elements", label,
							parameter.Name(), length))
				}
			}
		}
	}
}

// callExpression is the call expression a call instruction was built from: its source, or for a go or defer
// statement, whose source is the statement, the statement's call.
func callExpression(call ir.CallInstruction) *ast.CallExpr {
	switch source := call.Source().(type) {
	case *ast.CallExpr:
		return source
	case *ast.GoStmt:
		return source.Call
	case *ast.DeferStmt:
		return source.Call
	default:
		// Invariant: the builder gives every call instruction a call, a go statement or a defer statement as its
		// source, so this is a builder this port has not learned.
		panic(fmt.Sprintf("odd_slice_argument: a call instruction built from %T", source))
	}
}

// sliceLength is the length of value when it is a slice expression whose bounds are constants, of an array, of a
// literal or make with a constant length, or of another such slice, and -1 when it is not known. Phis are looked
// through as irutil.Flatten looks through them.
func sliceLength(value ir.Value) int {
	constantOf := func(bound ir.Value) int {
		if known, isConstant := bound.(*ir.Const); isConstant {
			return int(known.Int64())
		}
		return -1
	}
	slice, isSlice := irutil.Flatten(value).(*ir.Slice)
	if !isSlice {
		return -1
	}
	low := 0
	high := -1
	if slice.Low != nil {
		low = constantOf(slice.Low)
	}
	if slice.High != nil {
		high = constantOf(slice.High)
	} else {
		switch sliced := slice.X.(type) {
		case *ir.Alloc:
			array, isArray := typeutil.Dereference(sliced.Type()).Underlying().(*types.Array)
			if !isArray {
				// Invariant: a slice expression with no high bound slices an array it allocated, a slice or a
				// string, and only the array is an Alloc.
				panic(fmt.Sprintf("odd_slice_argument: a slice of an allocation of %s", sliced.Type()))
			}
			high = int(array.Len())
		case *ir.Slice:
			high = sliceLength(sliced)
		}
	}
	if low == -1 || high == -1 {
		return -1
	}
	return high - low
}
