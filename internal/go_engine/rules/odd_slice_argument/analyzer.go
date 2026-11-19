// Package odd_slice_argument is staticcheck's SA5012, a slice of odd length passed to a function that panics unless
// its slice has an even length, as strings.NewReplacer and logging APIs taking pairs do, ported onto the engine's own
// intermediate representation as an analyzer that asks every mark it reads of the run's fact resolver (#w7zmvat).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5012/sa5012.go, whose reading of one
// function's body it keeps and whose message it reports word for word, at the same range, the argument. The original
// runs in three passes over the package's functions: it marks each slice parameter a function checks for even length
// on every path before panicking, then each slice parameter a function passes on every path to a parameter so marked,
// and then reports each call passing a slice of known odd length to a marked parameter. It pushes its marks as object
// facts, judging every package, dependencies included, each in one walk in the order buildir lists its functions.
//
// Here a mark is asked, never pushed. The port reports the same calls, one function at a time, and asks the mark of
// each such call's parameter of the run's fact resolver, the analyzed package's own as another package's, which judges
// only that parameter's function and what its marks rest on, in the package that declares it (#w7zmvat unit 2,
// #8d9r2rh). The marks are defined with no order, as a least set, and demand_walk.go says how they are found. Where the
// original asks staticcheck's internal iterutil.All whether every return block is dominated, the port loops over them
// itself, and where it asks irutil.IsCallTo whether a call is to len, the port asks whether the callee is the builtin
// len, the one callee whose name irutil.CallName gives as "len". It is a plain analyzer on
// build_intermediate_representation.Analyzer, and generated, whose judgment the context reads though this check filters
// nothing. The push walk stays in push_walk_test.go, the tests' record of what the original's walk marks, and the fact
// differential beside this file holds that record to the original's facts over whole trees.
//
// It differs in two places, each adjudicated. The original reads a call's source as a call expression, and the source
// of a go or defer statement's call is the statement, so a go or defer passing an odd slice to a marked parameter
// panics the original's run. The port reads the statement's call and reports the argument there, which
// TestPortReportsWhereTheOriginalPanics holds, running the original behind a recover and requiring that it panic. And
// the original's marks turn on the order its walk meets a package's functions in, wherever a cycle of calls or a call
// through an instantiation or a wrapper lies between a parameter and its check, where the port marks what the
// definition marks in every order: definition_test.go is the definition's oracle, and the fixtures under
// testdata/src/order and testdata/src/standin each hold one package in two orders. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere else.
package odd_slice_argument

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port under the original's name, so the rule, its settings and every `//cohere:ignore` naming it
// read the same once the port takes the original's place. It asks the run's fact resolver of every mark it reads, of
// packages ctrlflow, required through build_intermediate_representation, has the resolver hold.
var Analyzer = &analysis.Analyzer{
	Name: "SA5012",
	Doc: "Flags a slice of odd length passed to a function that needs an even one: staticcheck's SA5012, ported onto " +
		"the engine's own intermediate representation.",
	Requires: []*analysis.Analyzer{build_intermediate_representation.Analyzer, generated.Analyzer},
	Run:      run,
}

// run reports the odd slices each function of the package passes to a marked parameter, every mark, the package's own
// among them, asked of the run's fact resolver.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	resolver, resolverError := fact_resolver.Of(pass)
	if resolverError != nil {
		return nil, resolverError
	}
	reader := resolver.Package(pass.Pkg)
	if reader == nil {
		return nil, fmt.Errorf("reading the marks of %s: the fact resolver never learned it, though ctrlflow, which "+
			"it learns a package from, ran on it first", pass.Pkg.Path())
	}
	marks := (&Result{resolver: resolver, reader: reader}).Asking(pass.Analyzer.Name)
	for function := range built.SourceFunctions() {
		reportOddLengths(context, marks.Marked, function)
	}
	return nil, nil
}

// dominatesEveryReturn is whether every path through function to a return passes through block.
func dominatesEveryReturn(
	function *intermediate_representation.Function,
	block *intermediate_representation.BasicBlock,
) bool {
	for returned := range function.Returns() {
		if !block.Dominates(returned) {
			return false
		}
	}
	return true
}

// directChecks is each slice parameter function checks on every path with `len(p) % 2 != 0` or `len(p) % 2 == 1`,
// either way around, and panics when the check holds, which the push marks first, before any call passes one on.
func directChecks(function *intermediate_representation.Function) []*intermediate_representation.Parameter {
	var checked []*intermediate_representation.Parameter
	for _, block := range function.Blocks {
		if !dominatesEveryReturn(function, block) {
			continue
		}
		branch, isIf := block.Control().(*intermediate_representation.If)
		if !isIf {
			continue
		}
		comparison, isBinary := branch.Condition.(*intermediate_representation.BinaryOperation)
		if !isBinary {
			continue
		}
		var needle uint64
		switch comparison.Operator {
		case token.NEQ:
			needle = 0
		case token.EQL:
			needle = 1
		default:
			continue
		}
		remainder, isRemainder := comparison.X.(*intermediate_representation.BinaryOperation)
		compared, isConstant := comparison.Y.(*intermediate_representation.Constant)
		// The original looks the other way around only when neither side is in place, so a remainder with no
		// constant beside it, or a constant with no remainder, is no check.
		if isRemainder != isConstant {
			continue
		}
		if !isRemainder {
			remainder, isRemainder = comparison.Y.(*intermediate_representation.BinaryOperation)
			compared, isConstant = comparison.X.(*intermediate_representation.Constant)
		}
		if !isRemainder || !isConstant || remainder.Operator != token.REM ||
			compared.Value.Kind() != constant.Int || compared.Uint64() != needle {
			continue
		}
		divisor, isDivisorConstant := remainder.Y.(*intermediate_representation.Constant)
		if !isDivisorConstant || divisor.Value.Kind() != constant.Int || divisor.Uint64() != 2 {
			continue
		}
		length, isCall := remainder.X.(*intermediate_representation.Call)
		if !isCall {
			continue
		}
		builtin, isBuiltin := length.Call.Value.(*intermediate_representation.Builtin)
		if !isBuiltin || builtin.Name() != "len" {
			continue
		}
		// Only a parameter read directly; one that has flowed through a phi is not followed.
		parameter, isParameter := length.Call.Arguments[0].(*intermediate_representation.Parameter)
		if !isParameter || !typeutil.All(parameter.Type(), typeutil.IsSlice) {
			continue
		}
		if _, panics := block.Successors[0].Control().(*intermediate_representation.Panic); !panics {
			continue
		}
		checked = append(checked, parameter)
	}
	return checked
}

// reportOddLengths reports each call in function passing a slice whose length is known and odd to a parameter marked
// says is marked. It reads function's own instructions and asks one mark for each such call, so it judges a function
// on its own.
func reportOddLengths(
	context *rule.Context,
	marked func(parameter *types.Var) bool,
	function *intermediate_representation.Function,
) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			call, isCall := instruction.(intermediate_representation.CallInstruction)
			if !isCall {
				continue
			}
			callee := call.Common().StaticCallee()
			if callee == nil {
				continue
			}
			for argumentIndex, argument := range call.Common().Arguments {
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
				// The length is read before the mark is asked, since a finding needs both and only the mark is
				// costly: it is a question of the resolver, which judges the parameter's function and records the
				// answer and the read for the cache. A slice of unknown or even length is never reported, whatever
				// the mark, so leaving its question unasked changes only what the cache holds, never a finding.
				// A slice of an allocation of no array, which the builder makes for an array whose type is a
				// type parameter, has no length the scan can read, and asking the mark first as the original
				// does keeps its invariant reached exactly where the original reaches it, at a marked parameter.
				length, unreadable := knownSliceLength(argument)
				if unreadable == nil && (length < 0 || length%2 == 0) {
					continue
				}
				parameter := callee.Signature.Params().At(parameterIndex)
				if !marked(parameter) {
					continue
				}
				if unreadable != nil {
					// Invariant: a slice expression with no high bound slices an array it allocated, a slice or
					// a string, and only the array is an Allocation.
					panic(fmt.Sprintf("odd_slice_argument: a slice of an allocation of %s", unreadable.Type()))
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

// callExpression is the call expression a call instruction was built from: its source, or for a go or defer
// statement, whose source is the statement, the statement's call.
func callExpression(call intermediate_representation.CallInstruction) *ast.CallExpr {
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

// knownSliceLength is the length of value when it is a slice expression whose bounds are constants, of an array, of
// a literal or make with a constant length, or of another such slice, and -1 when it is not known. Phis are looked
// through as intermediate_representation.Flatten, staticcheck's irutil.Flatten translated, looks through them. A slice
// with no high bound of an allocation of no array is returned as unreadable, -1 beside it, for the caller to judge.
func knownSliceLength(
	value intermediate_representation.Value,
) (length int, unreadable *intermediate_representation.Allocation) {
	constantOf := func(bound intermediate_representation.Value) int {
		if known, isConstant := bound.(*intermediate_representation.Constant); isConstant {
			return int(known.Int64())
		}
		return -1
	}
	slice, isSlice := intermediate_representation.Flatten(value).(*intermediate_representation.Slice)
	if !isSlice {
		return -1, nil
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
		case *intermediate_representation.Allocation:
			array, isArray := typeutil.Dereference(sliced.Type()).Underlying().(*types.Array)
			if !isArray {
				return -1, sliced
			}
			high = int(array.Len())
		case *intermediate_representation.Slice:
			high, unreadable = knownSliceLength(sliced)
			if unreadable != nil {
				return -1, unreadable
			}
		}
	}
	if low == -1 || high == -1 {
		return -1, nil
	}
	return high - low, nil
}
