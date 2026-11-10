// Package correctness_require_wrapped_error is the Go engine's rule that an error made from another error wraps it
// with %w, rather than keeping only its text.
//
// An error formatted with %v or %s, or rebuilt from its Error() text, reads the same in a log and is a different
// error to code: errors.Is and errors.As walk the chain %w builds, and a caller checking for os.ErrNotExist or a
// typed error finds nothing once the chain is cut. The fix costs one letter, and the chain is what callers decide on.
package correctness_require_wrapped_error

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-wrapped-error"

// listener hears every call of the syntax dispatcher's walk.
var listener = rule.Listener{
	FileLocal: true, Nodes: []ast.Node{(*ast.CallExpr)(nil)}, Node: judgeCall}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "correctness_require_wrapped_error", "Flags an error formatted into "+
	"fmt.Errorf by a verb that prints its text (%v, %s, %q, %x, %X) instead of %w, and an errors.New or fmt.Errorf "+
	"built from an error's Error() text, since both cut the chain errors.Is and errors.As read. A call whose verbs "+
	"cannot be mapped to its arguments with certainty, through a * width or precision, a malformed index, a missing "+
	"argument or a spread slice, is not judged.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var unwrappedErrorMessage = policy.MessageOf(Name, "unwrappedError")

var rebuiltErrorMessage = policy.MessageOf(Name, "rebuiltError")

// errorInterface is the predeclared error, the interface an argument's static type is checked against.
var errorInterface = func() *types.Interface {
	predeclared, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		// Invariant: the predeclared error is an interface in every Go this engine is built with.
		panic("the predeclared error is not an interface")
	}
	return predeclared
}()

// textVerbs are the verbs fmt answers with an error's Error() text, as its handleMethods does. %T and %p print the
// type and the address, which describe the error rather than stand in for it, so they are no lost wrap.
const textVerbs = "vsqxX"

// judgeCall judges a call of errors.New or fmt.Errorf.
func judgeCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall || len(call.Args) == 0 {
		return
	}
	function, isFunction := typeutil.Callee(context.TypesInfo, call).(*types.Func)
	// Package functions only, so a method that happens to be named New or Errorf is never mistaken for them.
	if !isFunction || function.Pkg() == nil || function.Signature().Recv() != nil {
		return
	}
	switch {
	case function.Pkg().Path() == "errors" && function.Name() == "New":
		reportRebuilds(context, call.Args[0])
	case function.Pkg().Path() == "fmt" && function.Name() == "Errorf":
		judgeErrorf(context, call)
	}
}

// judgeErrorf reports every error argument of a fmt.Errorf call that a text verb formats, and every Error() text
// the call builds from. A format that is not a constant is no format the rule can read, but it may still be an
// error's text, fmt.Errorf(readError.Error()), which is the rebuild.
func judgeErrorf(context *rule.Context, call *ast.CallExpr) {
	format := context.TypesInfo.Types[call.Args[0]].Value
	if format == nil || format.Kind() != constant.String {
		reportRebuilds(context, call.Args[0])
		return
	}
	// A spread slice, fmt.Errorf("%v", values...), holds arguments the rule cannot see one by one.
	if call.Ellipsis.IsValid() {
		return
	}
	arguments := call.Args[1:]
	directives, mapped := parseFormat(constant.StringVal(format), len(arguments))
	if !mapped {
		return
	}
	var wrapped []ast.Expr
	for _, each := range directives {
		if each.verb == 'w' {
			wrapped = append(wrapped, arguments[each.argument])
		}
	}
	reported := map[int]bool{}
	for _, each := range directives {
		if !each.printsText() || reported[each.argument] {
			continue
		}
		argument := arguments[each.argument]
		// An error the same call also wraps keeps its chain, so printing it a second time loses nothing.
		if isError(context, argument) && !isAnyOf(context, argument, wrapped) {
			reported[each.argument] = true
			context.Report(analysis.Diagnostic{
				Pos:      argument.Pos(),
				End:      argument.End(),
				Category: unwrappedErrorMessage.Id,
				Message:  unwrappedErrorMessage.Render(map[string]string{"verb": each.text}),
			})
			continue
		}
		if receiver := errorTextReceiver(context, argument); receiver != nil && !isAnyOf(context, receiver, wrapped) {
			reported[each.argument] = true
			reportRebuild(context, argument, receiver)
		}
	}
}

// reportRebuilds reports each Error() text that expression is made of, directly or as a part of a string
// concatenation, `"reading: " + readError.Error()`, since every one of them is an error whose chain the new error
// drops.
func reportRebuilds(context *rule.Context, expression ast.Expr) {
	unwrapped := ast.Unparen(expression)
	if binary, isBinary := unwrapped.(*ast.BinaryExpr); isBinary && binary.Op == token.ADD {
		reportRebuilds(context, binary.X)
		reportRebuilds(context, binary.Y)
		return
	}
	if receiver := errorTextReceiver(context, unwrapped); receiver != nil {
		reportRebuild(context, unwrapped, receiver)
	}
}

func reportRebuild(context *rule.Context, call ast.Expr, receiver ast.Expr) {
	context.Report(analysis.Diagnostic{
		Pos:      call.Pos(),
		End:      call.End(),
		Category: rebuiltErrorMessage.Id,
		Message:  rebuiltErrorMessage.Render(map[string]string{"error": types.ExprString(receiver)}),
	})
}

// errorTextReceiver is the error whose text expression reads, when expression is a call of the Error method of the
// error interface on a value whose type implements it, and nil otherwise. An Error method of another shape, one
// taking arguments or returning something other than a string, makes no error and is not the text of one.
func errorTextReceiver(context *rule.Context, expression ast.Expr) ast.Expr {
	call, isCall := ast.Unparen(expression).(*ast.CallExpr)
	if !isCall || len(call.Args) != 0 {
		return nil
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return nil
	}
	// A method value only: a method expression, error.Error(readError), names its receiver as an argument.
	selection, isSelection := context.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal || selection.Obj().Name() != "Error" {
		return nil
	}
	if !isError(context, selector.X) {
		return nil
	}
	return selector.X
}

// isError is whether expression's static type implements error. The untyped nil implements nothing, and a value
// held in an any is not known to be an error, so neither is judged.
func isError(context *rule.Context, expression ast.Expr) bool {
	recorded, isRecorded := context.TypesInfo.Types[expression]
	if !isRecorded || !recorded.IsValue() || recorded.IsNil() {
		return false
	}
	return types.Implements(recorded.Type, errorInterface)
}

// isAnyOf is whether expression is the same value as one of candidates, judged by objects rather than spelling: the
// same variable, or the same field reached through the same value.
func isAnyOf(context *rule.Context, expression ast.Expr, candidates []ast.Expr) bool {
	for _, candidate := range candidates {
		if sameValue(context, expression, candidate) {
			return true
		}
	}
	return false
}

func sameValue(context *rule.Context, left ast.Expr, right ast.Expr) bool {
	unwrappedLeft := ast.Unparen(left)
	unwrappedRight := ast.Unparen(right)
	switch leftNode := unwrappedLeft.(type) {
	case *ast.Ident:
		rightNode, isIdentifier := unwrappedRight.(*ast.Ident)
		if !isIdentifier {
			return false
		}
		object := context.TypesInfo.Uses[leftNode]
		_, isVariable := object.(*types.Var)
		return isVariable && object == context.TypesInfo.Uses[rightNode]
	case *ast.SelectorExpr:
		rightNode, isSelector := unwrappedRight.(*ast.SelectorExpr)
		if !isSelector {
			return false
		}
		leftSelection, leftIsSelection := context.TypesInfo.Selections[leftNode]
		rightSelection, rightIsSelection := context.TypesInfo.Selections[rightNode]
		// Fields only: a method call may answer differently each time, so two calls are never the same value.
		if !leftIsSelection || !rightIsSelection || leftSelection.Kind() != types.FieldVal {
			return false
		}
		return leftSelection.Obj() == rightSelection.Obj() && sameValue(context, leftNode.X, rightNode.X)
	}
	return false
}

// parseFormat reads format the way fmt's doPrintf does and pairs each verb with the argument it formats, out of
// argumentCount. It answers false, so the call goes unjudged, wherever the pairing is not certain: a * width or
// precision takes an argument that shifts every verb after it, and a malformed or out of range index, a verb with
// no argument, or a directive with no verb are mistakes vet's printf reports and this rule would only guess past.
func parseFormat(format string, argumentCount int) ([]directive, bool) {
	var directives []directive
	argument := 0
	for index := 0; index < len(format); {
		if format[index] != '%' {
			index++
			continue
		}
		start := index
		index++
		for index < len(format) && strings.IndexByte("+-# 0", format[index]) >= 0 {
			index++
		}
		sharp := strings.IndexByte(format[start:index], '#') >= 0
		var afterIndex bool
		var good bool
		argument, index, afterIndex, good = argumentNumber(format, index, argument, argumentCount)
		if !good || (index < len(format) && format[index] == '*') {
			return nil, false
		}
		widthStart := index
		index = skipDigits(format, index)
		// fmt refuses an index followed by a width, %[1]5d, as a bad argument number.
		if afterIndex && index > widthStart {
			return nil, false
		}
		if index < len(format) && format[index] == '.' {
			index++
			// And an index before the precision's dot, %[1].2f, the same way.
			if afterIndex {
				return nil, false
			}
			argument, index, afterIndex, good = argumentNumber(format, index, argument, argumentCount)
			if !good || (index < len(format) && format[index] == '*') {
				return nil, false
			}
			index = skipDigits(format, index)
		}
		if !afterIndex {
			argument, index, _, good = argumentNumber(format, index, argument, argumentCount)
			if !good {
				return nil, false
			}
		}
		if index >= len(format) {
			return nil, false
		}
		verb, size := utf8.DecodeRuneInString(format[index:])
		index += size
		// %% prints a percent sign and takes no argument, whatever flags stand between.
		if verb == '%' {
			continue
		}
		if argument >= argumentCount {
			return nil, false
		}
		directives = append(directives, directive{
			text:     format[start:index],
			verb:     verb,
			sharp:    sharp,
			argument: argument,
		})
		argument++
	}
	return directives, true
}

// argumentNumber reads an explicit argument index, [n], at index, as fmt's argNumber does: n counts from one, and
// the verbs after it continue from n. It answers the argument to use, where reading resumes, whether an index was
// read, and false when the brackets are there and hold no argument fmt would accept.
func argumentNumber(format string, index int, argument int, argumentCount int) (int, int, bool, bool) {
	if index >= len(format) || format[index] != '[' {
		return argument, index, false, true
	}
	closing := strings.IndexByte(format[index:], ']')
	if closing < 0 {
		return argument, index, false, false
	}
	digits := format[index+1 : index+closing]
	if digits == "" || skipDigits(digits, 0) != len(digits) {
		return argument, index, false, false
	}
	number := 0
	for _, digit := range digits {
		number = number*10 + int(digit-'0')
		// fmt gives up on a number this large too, and no call has this many arguments.
		if number > 1e6 {
			return argument, index, false, false
		}
	}
	if number < 1 || number > argumentCount {
		return argument, index, false, false
	}
	return number - 1, index + closing + 1, true, true
}

func skipDigits(text string, index int) int {
	end := index
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	return end
}
