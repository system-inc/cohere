// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines questions asked of types. Some of these belong in go/types.

import (
	"go/types"

	"golang.org/x/exp/typeparams"

	"github.com/system-inc/cohere/internal/go_engine/core_type"
)

// isString reports whether `resolvedType` is exactly a string type.
// `resolvedType` is assumed to be an Underlying type (not Named or Alias).
func isString(resolvedType types.Type) bool {
	basic, ok := resolvedType.(*types.Basic)
	return ok && basic.Info()&types.IsString != 0
}

// isByteSlice reports whether `resolvedType` is of the form []~bytes.
// `resolvedType` is assumed to be an Underlying type (not Named or Alias).
func isByteSlice(resolvedType types.Type) bool {
	if slice, ok := resolvedType.(*types.Slice); ok {
		basic, _ := slice.Elem().Underlying().(*types.Basic)
		return basic != nil && basic.Kind() == types.Byte
	}
	return false
}

// isPointer reports whether `resolvedType`'s underlying type is a pointer.
func isPointer(resolvedType types.Type) bool {
	return is[*types.Pointer](resolvedType.Underlying())
}

// isPointerCore reports whether `resolvedType`'s core type is a pointer.
//
// (Most pointer manipulation is related to receivers, in which case
// isPointer is appropriate. tecallers can use isPointer(resolvedType).
func isPointerCore(resolvedType types.Type) bool {
	return is[*types.Pointer](core_type.Of(resolvedType))
}

// is reports whether x holds a T.
func is[T any](x types.Type) bool {
	_, ok := x.(T)
	return ok
}

// deref returns a pointer's element type; otherwise it returns typ.
func deref(typ types.Type) types.Type {
	resolvedType := types.Unalias(typ)

	if parameter, ok := resolvedType.(*types.TypeParam); ok {
		if ctyp := core_type.Of(parameter); ctyp != nil {
			// This can happen, for example, with len(T) where T is a
			// type parameter whose core type is a pointer to array.
			resolvedType = ctyp
		}
	}
	if pointer, ok := resolvedType.Underlying().(*types.Pointer); ok {
		return pointer.Elem()
	}
	return typ
}

// fieldOf returns the index'th field of the (core type of) a struct type;
// otherwise returns nil.
func fieldOf(typ types.Type, index int) *types.Var {
	if st, ok := core_type.Of(typ).(*types.Struct); ok {
		if 0 <= index && index < st.NumFields() {
			return st.Field(index)
		}
	}
	return nil
}

// isUntyped reports whether typ is the type of an untyped constant.
func isUntyped(typ types.Type) bool {
	// No Underlying/Unalias: untyped constant types cannot be Named or Alias.
	basic, ok := typ.(*types.Basic)
	return ok && basic.Info()&types.IsUntyped != 0
}

// isBasic reports whether `resolvedType` is a basic type.
func isBasic(resolvedType types.Type) bool {
	_, ok := resolvedType.(*types.Basic)
	return ok
}

// isNonTypeParamInterface reports whether `resolvedType` is an interface type but not a type parameter.
func isNonTypeParamInterface(resolvedType types.Type) bool {
	return !typeparams.IsTypeParam(resolvedType) && types.IsInterface(resolvedType)
}
