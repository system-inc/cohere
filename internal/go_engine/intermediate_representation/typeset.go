// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"
)

// Utilities for dealing with type sets.

const debug = false

const (
	ixInvalid     indexMode = iota // index is invalid
	ixValue                        // index is a computed value (not addressable)
	ixArrVariable                  // like ixVar, but index operand contains an array
	ixVariable                     // index is an addressable variable
	ixMap                          // index is a map index (a variable on the lhs, comma-ok on the rhs of an assignment)
)

// typeset is an iterator over the (type/underlying type) pairs of the
// specific type terms of the type set implied by t.
// If t is a type parameter, the implied type set is the type set of t's constraint.
// In that case, if there are no specific terms, typeset calls yield with (nil, nil).
// If t is not a type parameter, the implied type set consists of just t.
// In any case, typeset is guaranteed to call yield at least once.
func typeset(goType types.Type, yield func(termType, underlyingType types.Type) bool) {
	switch unaliased := types.Unalias(goType).(type) {
	case *types.TypeParam, *types.Interface:
		terms := termListOf(unaliased)
		if len(terms) == 0 {
			yield(nil, nil)
			return
		}
		for _, term := range terms {
			resolvedType := types.Unalias(term.Type())
			if !term.Tilde() {
				resolvedType = resolvedType.Underlying()
			}
			if debug {
				assert(types.Identical(resolvedType, resolvedType.Underlying()), "Unalias(x) == under(x) for ~x terms")
			}
			if !yield(term.Type(), resolvedType) {
				break
			}
		}
		return
	default:
		yield(unaliased, unaliased.Underlying())
	}
}

// termListOf returns the type set of `goType` as a normalized term set. Returns an empty set on an error.
func termListOf(goType types.Type) []*types.Term {
	return typeutil.NewTypeSet(goType).Terms
}

// typeSetIsEmpty returns true if a typeset is empty.
func typeSetIsEmpty(goType types.Type) bool {
	var empty bool
	typeset(goType, func(resolvedType, _ types.Type) bool {
		empty = resolvedType == nil
		return false
	})
	return empty
}

// isBytestring returns true if T has the same terms as interface{[]byte | string}.
// These act like a core type for some operations: slice expressions, append and copy.
//
// See https://go.dev/ref/spec#Core_types for the details on bytestring.
func isBytestring(goType types.Type) bool {
	underlyingType := goType.Underlying()
	if _, ok := underlyingType.(*types.Interface); !ok {
		return false
	}

	hasBytes, hasString := false, false
	ok := underIs(underlyingType, func(resolvedType types.Type) bool {
		switch {
		case isString(resolvedType):
			hasString = true
			return true
		case isByteSlice(resolvedType):
			hasBytes = true
			return true
		default:
			return false
		}
	})
	return ok && hasBytes && hasString
}

// underIs calls `predicate` with the underlying types of the type terms
// of the type set of `goType` and reports whether all calls to `predicate` returned true.
// If there are no specific terms, underIs returns the result of `predicate`(nil).
func underIs(goType types.Type, predicate func(types.Type) bool) bool {
	var ok bool
	typeset(goType, func(termType, underlyingType types.Type) bool {
		ok = predicate(underlyingType)
		return ok
	})
	return ok
}

// indexType returns the element type and index mode of a IndexExpr over a type.
// It returns an invalid mode if the type is not indexable; this should never occur in a well-typed program.
func indexType(goType types.Type) (types.Type, indexMode) {
	switch underlyingType := goType.Underlying().(type) {
	case *types.Array:
		return underlyingType.Elem(), ixArrVariable
	case *types.Pointer:
		if array, ok := underlyingType.Elem().Underlying().(*types.Array); ok {
			return array.Elem(), ixVariable
		}
	case *types.Slice:
		return underlyingType.Elem(), ixVariable
	case *types.Map:
		return underlyingType.Elem(), ixMap
	case *types.Basic:
		return tByte, ixValue // must be a string
	case *types.Interface:
		var elem types.Type
		mode := ixInvalid
		typeset(goType, func(termType, _ types.Type) bool {
			if termType == nil {
				return false // empty set
			}
			elementType, indexMode := indexType(termType)
			if elem == nil {
				elem, mode = elementType, indexMode
			}
			if debug && !types.Identical(elem, elementType) { // if type checked, just a sanity check
				mode = ixInvalid
				return false
			}
			// Update the mode to the most constrained address type.
			mode = mode.meet(indexMode)
			return mode != ixInvalid
		})
		return elem, mode
	}
	return nil, ixInvalid
}
