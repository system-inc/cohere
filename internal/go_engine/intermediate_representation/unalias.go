package intermediate_representation

import (
	"go/types"
)

// unaliasDeeply is given with every alias in it replaced by the type it stands for, however deep: through
// pointers, slices, arrays, maps, channels, signatures, structs, tuples and the type arguments of instantiated
// named types. A type parameter, a basic type, an uninstantiated named type and an interface are left as they are.
//
// An instance is named from its type arguments unaliased (#6pt3bt8): identical type arguments one call spells
// through an alias and another doesn't would otherwise name the one instance both share after whichever call
// created it first, so a lazy or concurrent build would name it by its build order.
func unaliasDeeply(given types.Type) types.Type {
	switch typed := given.(type) {
	case *types.Alias:
		return unaliasDeeply(types.Unalias(typed))
	case *types.Pointer:
		return types.NewPointer(unaliasDeeply(typed.Elem()))
	case *types.Slice:
		return types.NewSlice(unaliasDeeply(typed.Elem()))
	case *types.Array:
		return types.NewArray(unaliasDeeply(typed.Elem()), typed.Len())
	case *types.Map:
		return types.NewMap(unaliasDeeply(typed.Key()), unaliasDeeply(typed.Elem()))
	case *types.Chan:
		return types.NewChan(typed.Dir(), unaliasDeeply(typed.Elem()))
	case *types.Tuple:
		return unaliasTuple(typed)
	case *types.Signature:
		return types.NewSignatureType(nil, nil, nil, unaliasTuple(typed.Params()), unaliasTuple(typed.Results()),
			typed.Variadic())
	case *types.Struct:
		fields := make([]*types.Var, typed.NumFields())
		tags := make([]string, typed.NumFields())
		for index := range typed.NumFields() {
			field := typed.Field(index)
			fields[index] = types.NewField(field.Pos(), field.Pkg(), field.Name(), unaliasDeeply(field.Type()),
				field.Embedded())
			tags[index] = typed.Tag(index)
		}
		return types.NewStruct(fields, tags)
	case *types.Named:
		arguments := typed.TypeArgs()
		if arguments.Len() == 0 {
			return typed
		}
		unaliased := make([]types.Type, arguments.Len())
		for index := range arguments.Len() {
			unaliased[index] = unaliasDeeply(arguments.At(index))
		}
		instance, instantiateError := types.Instantiate(nil, typed.Origin(), unaliased, false)
		if instantiateError != nil {
			// Not expected: with validate false, go/types documents that Instantiate never errs. The name then
			// keeps the arguments as spelled, which only loses canonical naming, never correctness.
			return typed
		}
		return instance
	}
	return given
}

// unaliasTuple is tuple with each variable's type unaliased deeply, or nil for a nil tuple.
func unaliasTuple(tuple *types.Tuple) *types.Tuple {
	if tuple == nil {
		return nil
	}
	variables := make([]*types.Var, tuple.Len())
	for index := range tuple.Len() {
		variable := tuple.At(index)
		variables[index] = types.NewParam(variable.Pos(), variable.Pkg(), variable.Name(),
			unaliasDeeply(variable.Type()))
	}
	return types.NewTuple(variables...)
}

// unaliasEach is each of given unaliased deeply.
func unaliasEach(given []types.Type) []types.Type {
	unaliased := make([]types.Type, len(given))
	for index, each := range given {
		unaliased[index] = unaliasDeeply(each)
	}
	return unaliased
}
