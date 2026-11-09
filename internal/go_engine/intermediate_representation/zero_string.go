// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file carries x/tools' internal typesinternal.ZeroString, which Constant.RelativeString (go/ir's Const.RelString)
// calls to print a zero constant and a module outside x/tools can't import.

import (
	"fmt"
	"go/types"
	"strings"
)

// zeroString returns the string representation of the zero value for any type `resolvedType`.
// The boolean result indicates whether the type is or contains an invalid type
// or a non-basic (constraint) interface type.
//
// Even for invalid input types, zeroString may return a partially correct
// string representation. The caller should use the returned isValid boolean
// to determine the validity of the expression.
//
// When assigning to a wider type (such as 'any'), it's the caller's
// responsibility to handle any necessary type conversions.
//
// This string can be used on the right-hand side of an assignment where the
// left-hand side has that explicit type.
// References to named types are qualified by an appropriate (optional)
// qualifier function.
// Exception: This does not apply to tuples. Their string representation is
// informational only and cannot be used in an assignment.
func zeroString(resolvedType types.Type, qual types.Qualifier) (_ string, isValid bool) {
	switch typed := resolvedType.(type) {
	case *types.Basic:
		switch {
		case typed.Info()&types.IsBoolean != 0:
			return "false", true
		case typed.Info()&types.IsNumeric != 0:
			return "0", true
		case typed.Info()&types.IsString != 0:
			return `""`, true
		case typed.Kind() == types.UnsafePointer:
			fallthrough
		case typed.Kind() == types.UntypedNil:
			return "nil", true
		case typed.Kind() == types.Invalid:
			return "invalid", false
		default:
			panic(fmt.Sprintf("zeroString for unexpected type %v", typed))
		}

	case *types.Pointer, *types.Slice, *types.Chan, *types.Map, *types.Signature:
		return "nil", true

	case *types.Interface:
		if !typed.IsMethodSet() {
			return "invalid", false
		}
		return "nil", true

	case *types.Named:
		switch under := typed.Underlying().(type) {
		case *types.Struct, *types.Array:
			return types.TypeString(typed, qual) + "{}", true
		default:
			return zeroString(under, qual)
		}

	case *types.Alias:
		switch typed.Underlying().(type) {
		case *types.Struct, *types.Array:
			return types.TypeString(typed, qual) + "{}", true
		default:
			// A type parameter can have alias but alias type's underlying type
			// can never be a type parameter.
			// Use types.Unalias to preserve the info of type parameter instead
			// of call Underlying() going right through and get the underlying
			// type of the type parameter which is always an interface.
			return zeroString(types.Unalias(typed), qual)
		}

	case *types.Array, *types.Struct:
		return types.TypeString(typed, qual) + "{}", true

	case *types.TypeParam:
		// Assumes func new is not shadowed.
		return "*new(" + types.TypeString(typed, qual) + ")", true

	case *types.Tuple:
		// Tuples are not normal values.
		// We are currently format as "(t[0], ..., t[n])". Could be something else.
		isValid := true
		components := make([]string, typed.Len())
		for index := 0; index < typed.Len(); index++ {
			comp, ok := zeroString(typed.At(index).Type(), qual)

			components[index] = comp
			isValid = isValid && ok
		}
		return "(" + strings.Join(components, ", ") + ")", isValid

	case *types.Union:
		// Variables of these types cannot be created, so it makes
		// no sense to ask for their zero value.
		panic(fmt.Sprintf("invalid type for a variable: %v", typed))

	default:
		panic(typed) // unreachable.
	}
}
