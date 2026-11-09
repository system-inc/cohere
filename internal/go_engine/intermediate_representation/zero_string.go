// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file carries x/tools' internal typesinternal.ZeroString, which Constant.RelativeString (go/ir's Const.RelString) calls
// to print a zero constant and a module outside x/tools can't import.

import (
	"fmt"
	"go/types"
	"strings"
)

// zeroString returns the string representation of the zero value for any type t.
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
func zeroString(t types.Type, qual types.Qualifier) (_ string, isValid bool) {
	switch t := t.(type) {
	case *types.Basic:
		switch {
		case t.Info()&types.IsBoolean != 0:
			return "false", true
		case t.Info()&types.IsNumeric != 0:
			return "0", true
		case t.Info()&types.IsString != 0:
			return `""`, true
		case t.Kind() == types.UnsafePointer:
			fallthrough
		case t.Kind() == types.UntypedNil:
			return "nil", true
		case t.Kind() == types.Invalid:
			return "invalid", false
		default:
			panic(fmt.Sprintf("zeroString for unexpected type %v", t))
		}

	case *types.Pointer, *types.Slice, *types.Chan, *types.Map, *types.Signature:
		return "nil", true

	case *types.Interface:
		if !t.IsMethodSet() {
			return "invalid", false
		}
		return "nil", true

	case *types.Named:
		switch under := t.Underlying().(type) {
		case *types.Struct, *types.Array:
			return types.TypeString(t, qual) + "{}", true
		default:
			return zeroString(under, qual)
		}

	case *types.Alias:
		switch t.Underlying().(type) {
		case *types.Struct, *types.Array:
			return types.TypeString(t, qual) + "{}", true
		default:
			// A type parameter can have alias but alias type's underlying type
			// can never be a type parameter.
			// Use types.Unalias to preserve the info of type parameter instead
			// of call Underlying() going right through and get the underlying
			// type of the type parameter which is always an interface.
			return zeroString(types.Unalias(t), qual)
		}

	case *types.Array, *types.Struct:
		return types.TypeString(t, qual) + "{}", true

	case *types.TypeParam:
		// Assumes func new is not shadowed.
		return "*new(" + types.TypeString(t, qual) + ")", true

	case *types.Tuple:
		// Tuples are not normal values.
		// We are currently format as "(t[0], ..., t[n])". Could be something else.
		isValid := true
		components := make([]string, t.Len())
		for i := 0; i < t.Len(); i++ {
			comp, ok := zeroString(t.At(i).Type(), qual)

			components[i] = comp
			isValid = isValid && ok
		}
		return "(" + strings.Join(components, ", ") + ")", isValid

	case *types.Union:
		// Variables of these types cannot be created, so it makes
		// no sense to ask for their zero value.
		panic(fmt.Sprintf("invalid type for a variable: %v", t))

	default:
		panic(t) // unreachable.
	}
}
