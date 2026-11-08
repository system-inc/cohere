package tests

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// acceptedFuzzTypes are the types fuzzing generates values of, in the order the finding lists them: testing's
// supportedTypes, `byte` and `rune` being uint8 and int32.
var acceptedFuzzTypes = []types.Type{
	types.Typ[types.String],
	types.Typ[types.Bool],
	types.Typ[types.Float32],
	types.Typ[types.Float64],
	types.Typ[types.Int],
	types.Typ[types.Int8],
	types.Typ[types.Int16],
	types.Typ[types.Int32],
	types.Typ[types.Int64],
	types.Typ[types.Uint],
	types.Typ[types.Uint8],
	types.Typ[types.Uint16],
	types.Typ[types.Uint32],
	types.Typ[types.Uint64],
	types.NewSlice(types.Universe.Lookup("byte").Type()),
}

// checkFuzz judges a Fuzz function's calls of (*testing.F).Fuzz and, when one hands Fuzz a fuzz target whose
// parameters fuzzing can fill, its calls of (*testing.F).Add against that target's parameters. x/tools' checkFuzz.
func checkFuzz(context *rule.Context, function *ast.FuncDecl) {
	if parameters := checkFuzzCall(context, function); parameters != nil {
		checkAddCalls(context, function, parameters)
	}
}

// checkFuzzCall judges each call of (*testing.F).Fuzz in function: its argument must be a function, the fuzz target,
// which returns nothing, takes a *testing.T first and only types fuzzing generates after it, and calls no method of
// *testing.F but Name and Failed. It returns the parameters of the first target that takes only what it should, or
// nil when none does. x/tools' checkFuzzCall.
func checkFuzzCall(context *rule.Context, function *ast.FuncDecl) *types.Tuple {
	var parameters *types.Tuple
	ast.Inspect(function, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall || !isFuzzTargetDot(context, call, "Fuzz") {
			return true
		}
		// Fuzz takes one argument, so another number does not type-check, and neither does an argument without a
		// type. The original's guards are kept, and like it they walk on into the call's arguments.
		if len(call.Args) != 1 {
			return true
		}
		target := call.Args[0]
		targetType := context.TypesInfo.Types[target].Type
		if targetType == nil {
			return true
		}
		signature, isSignature := targetType.Underlying().(*types.Signature)
		if !isSignature {
			// A value of interface type, a type parameter's among them, may hold a function, which Fuzz takes, since it
			// reads the function from its argument through reflection. The original reports it as no function.
			if !types.IsInterface(targetType) {
				context.ReportRangef(target, "argument to Fuzz must be a function")
			}
			return false
		}
		if signature.Results().Len() != 0 {
			context.ReportRangef(target, "fuzz target must not return any value")
		}
		if signature.Params().Len() == 0 {
			context.ReportRangef(target, "fuzz target must have 1 or more argument")
			return false
		}
		if validateFuzzArguments(context, signature.Params(), target) && parameters == nil {
			parameters = signature.Params()
		}
		reportTargetCalls(context, target, false)
		// A call of Fuzz inside the argument is a call of a *testing.F method, reported above, so it is not judged
		// again as a call of Fuzz.
		return false
	})
	return parameters
}

// reportTargetCalls reports each call of a *testing.F method but Name and Failed in node, part of the argument of
// Fuzz, that the fuzz target makes. inTarget is whether node runs in the target: the argument itself does not, since
// Go evaluates it before Fuzz runs anything, and a function literal in it does unless the argument calls it on the
// spot, `func() func(*testing.T, string) { ... }()`, which runs as the argument is evaluated. The original reports
// every such call in the argument.
func reportTargetCalls(context *rule.Context, node ast.Node, inTarget bool) {
	calledOnTheSpot := map[*ast.FuncLit]bool{}
	ast.Inspect(node, func(inspected ast.Node) bool {
		if literal, isLiteral := inspected.(*ast.FuncLit); isLiteral && !inTarget && !calledOnTheSpot[literal] {
			reportTargetCalls(context, literal, true)
			return false
		}
		call, isCall := inspected.(*ast.CallExpr)
		if !isCall {
			return true
		}
		// Inspect visits a call before what it calls, so a literal the call calls is known by the time it is reached.
		if literal, isLiteral := ast.Unparen(call.Fun).(*ast.FuncLit); isLiteral {
			calledOnTheSpot[literal] = true
		}
		if inTarget && isFuzzTargetDot(context, call, "") &&
			!isFuzzTargetDot(context, call, "Name") && !isFuzzTargetDot(context, call, "Failed") {
			context.ReportRangef(call, "fuzz target must not call any *F methods")
		}
		return true
	})
}

// checkAddCalls judges each call of (*testing.F).Add in function against parameters, the fuzz target's: it must hand
// Add one value for each parameter after the *testing.T, each of the parameter's type. x/tools' checkAddCalls.
func checkAddCalls(context *rule.Context, function *ast.FuncDecl, parameters *types.Tuple) {
	ast.Inspect(function, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall || !isFuzzTargetDot(context, call, "Add") {
			return true
		}
		// A spread slice, `f.Add(seed...)`, hands Add values whose number and types the call does not show. The
		// original counts the slice as one value of the slice's type.
		if call.Ellipsis.IsValid() {
			return true
		}
		values := addedValues(context, call)
		expected := parameters.Len() - 1
		if len(values) != expected {
			context.ReportRangef(
				call,
				"wrong number of values in call to (*testing.F).Add: %d, fuzz target expects %d", len(values), expected,
			)
			return true
		}
		mismatched := []int{}
		for index, value := range values {
			valueType := context.TypesInfo.Types[value].Type
			// A call that type-checks records every argument's type; the original's guard is kept.
			if valueType == nil {
				return true
			}
			// Add records what a value of interface type holds, a type parameter's among them, whose type the call
			// does not show. The original counts the interface's own type as mismatched.
			if types.IsInterface(valueType) {
				continue
			}
			if !types.Identical(valueType, parameters.At(index+1).Type()) {
				mismatched = append(mismatched, index)
			}
		}
		// One mismatched value is reported at the value, and more than one at the call, listing every value's type.
		switch {
		case len(mismatched) == 1:
			value := values[mismatched[0]]
			context.ReportRangef(
				value, "mismatched type in call to (*testing.F).Add: %v, fuzz target expects %v",
				context.TypesInfo.Types[value].Type, parameters.At(mismatched[0]+1).Type(),
			)
		case len(mismatched) > 1:
			got := []types.Type{}
			want := []types.Type{}
			for index, value := range values {
				got = append(got, context.TypesInfo.Types[value].Type)
				want = append(want, parameters.At(index+1).Type())
			}
			context.ReportRangef(
				call, "mismatched types in call to (*testing.F).Add: %v, fuzz target expects %v", got, want,
			)
		}
		return true
	})
}

// addedValues is the values call, a call of (*testing.F).Add, hands Add: its arguments, less the first when it calls
// the method expression `(*testing.F).Add`, whose first argument is the receiver. The original counts the receiver
// as a value.
func addedValues(context *rule.Context, call *ast.CallExpr) []ast.Expr {
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector || len(call.Args) == 0 {
		return call.Args
	}
	if selection := context.TypesInfo.Selections[selector]; selection != nil && selection.Kind() == types.MethodExpr {
		return call.Args[1:]
	}
	return call.Args
}

// isFuzzTargetDot is whether call calls the method of *testing.F named name, or any of its methods when name is
// empty. x/tools' isFuzzTargetDot, with its isFuzzTargetDotFuzz and isFuzzTargetDotAdd folded in.
func isFuzzTargetDot(context *rule.Context, call *ast.CallExpr, name string) bool {
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector || !isTestingType(context.TypesInfo.Types[selector.X].Type, "F") {
		return false
	}
	return name == "" || selector.Sel.Name == name
}

// validateFuzzArguments reports each parameter of a fuzz target that fuzzing cannot fill, a first that is not a
// *testing.T and a later one of a type fuzzing does not generate, and returns whether there was none. A finding is
// placed on the parameter's type when target is a function literal, and on target otherwise. x/tools'
// validateFuzzArgs.
func validateFuzzArguments(context *rule.Context, parameters *types.Tuple, target ast.Expr) bool {
	literal, isLiteral := target.(*ast.FuncLit)
	// placed is where the next finding goes. It carries over from one finding to the next, so a later parameter of a
	// literal whose parameters are unnamed, which the search by name below never reaches, is placed where the last
	// finding was, as the original places it.
	var placed analysis.Range = target
	valid := true
	// The first parameter is judged through an alias, `type T = *testing.T`, since Fuzz compares its type through
	// reflection, which sees the type the alias names. The original reports an alias as not *testing.T.
	if !isTestingType(types.Unalias(parameters.At(0).Type()), "T") {
		if isLiteral {
			placed = literal.Type.Params.List[0].Type
		}
		context.ReportRangef(placed, "the first parameter of a fuzz target must be *testing.T")
		valid = false
	}
	for index := 1; index < parameters.Len(); index++ {
		if isAcceptedFuzzType(parameters.At(index).Type()) {
			continue
		}
		if isLiteral {
			declared := 0
			for _, field := range literal.Type.Params.List {
				declared += len(field.Names)
				if index < declared {
					placed = field.Type
					break
				}
			}
		}
		context.ReportRangef(
			placed, "fuzzing arguments can only have the following types: %s", formatAcceptedFuzzType(),
		)
		valid = false
	}
	return valid
}

// isTestingType is whether typ is a pointer to package testing's type named name. An alias of the pointer is not
// looked through, as the original does not look through one. x/tools' isTestingType.
func isTestingType(typ types.Type, name string) bool {
	pointer, isPointer := typ.(*types.Pointer)
	if !isPointer {
		return false
	}
	return isTypeNamed(pointer.Elem(), "testing", name)
}

// isTypeNamed is whether typ is, or is an alias of, a type declared at package level in the package at path, named
// name. x/tools' typesinternal.IsTypeNamed, for one name.
func isTypeNamed(typ types.Type, path string, name string) bool {
	named, isNamed := types.Unalias(typ).(*types.Named)
	if !isNamed {
		return false
	}
	object := named.Obj()
	// x/tools' typesinternal.IsPackageLevel.
	isPackageLevel := object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
	return isPackageLevel && object.Pkg().Path() == path && object.Name() == name
}

// isAcceptedFuzzType is whether parameterType is one fuzzing generates values of. x/tools' isAcceptedFuzzType.
func isAcceptedFuzzType(parameterType types.Type) bool {
	return slices.ContainsFunc(acceptedFuzzTypes, func(accepted types.Type) bool {
		return types.Identical(accepted, parameterType)
	})
}

// formatAcceptedFuzzType is acceptedFuzzTypes as the finding lists them. x/tools' formatAcceptedFuzzType.
func formatAcceptedFuzzType() string {
	names := []string{}
	for _, accepted := range acceptedFuzzTypes {
		names = append(names, accepted.String())
	}
	return strings.Join(names, ", ")
}
