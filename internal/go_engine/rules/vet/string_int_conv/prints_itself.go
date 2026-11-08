package string_int_conv

import (
	"go/token"
	"go/types"
)

// errorInterface is the predeclared error's interface, `interface{ Error() string }`.
var errorInterface = predeclaredErrorInterface()

// stringerInterface is `interface{ String() string }`, fmt.Stringer's method set.
var stringerInterface = types.NewInterfaceType([]*types.Func{
	types.NewFunc(token.NoPos, nil, "String", types.NewSignatureType(
		nil, nil, nil, nil, types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.String])), false,
	)),
}, nil).Complete()

// printsItself is whether fmt.Sprint prints a value of candidate through one of its methods, not as a number: fmt's
// handleMethods, for the %v verb Sprint uses, calls a fmt.Formatter's Format, then an error's Error, then a
// fmt.Stringer's String, before it formats the value itself. A type parameter has the methods of its constraint.
// fmt.GoStringer's GoString serves only %#v, so it is not asked about. Each interface is built with go/types, so the
// port imports nothing of fmt's to name them, and a method answers only where its signature is the one fmt calls.
func printsItself(candidate types.Type) bool {
	if formatter := formatterInterfaceFor(candidate); formatter != nil && types.Implements(candidate, formatter) {
		return true
	}
	return types.Implements(candidate, errorInterface) || types.Implements(candidate, stringerInterface)
}

// predeclaredErrorInterface is the interface the universe's error names.
func predeclaredErrorInterface() *types.Interface {
	errorType, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		// Invariant: the language declares error as an interface type, and go/types' universe holds it as one.
		panic("string_int_conv: the predeclared error is not an interface")
	}
	return errorType
}

// formatterInterfaceFor is `interface{ Format(fmt.State, rune) }`, fmt.Formatter's method set, built with the
// fmt.State that candidate's Format method takes first, and nil where candidate has no Format method taking one. fmt's
// State is a type of fmt's own, which only a package that imports fmt can name, so it is taken from the method that
// names it: the type named State declared in the package whose path is fmt.
func formatterInterfaceFor(candidate types.Type) *types.Interface {
	object, _, _ := types.LookupFieldOrMethod(candidate, false, nil, "Format")
	method, isMethod := object.(*types.Func)
	if !isMethod || method.Signature().Params().Len() == 0 {
		return nil
	}
	state, isNamed := types.Unalias(method.Signature().Params().At(0).Type()).(*types.Named)
	if !isNamed || state.Obj().Pkg() == nil || state.Obj().Pkg().Path() != "fmt" || state.Obj().Name() != "State" {
		return nil
	}
	return types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, nil, "Format", types.NewSignatureType(nil, nil, nil, types.NewTuple(
			types.NewVar(token.NoPos, nil, "", state), types.NewVar(token.NoPos, nil, "", types.Typ[types.Rune]),
		), nil, false)),
	}, nil).Complete()
}
