// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines the Constant SSA value type.

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strconv"

	"golang.org/x/exp/typeparams"
)

// soleTypeKind returns a BasicInfo for which constant.Value can
// represent all zero values for the types in the type set.
//
//	types.IsBoolean for false is a representative.
//	types.IsInteger for 0
//	types.IsString for ""
//	0 otherwise.
func soleTypeKind(typ types.Type) types.BasicInfo {
	// State records the set of possible zero values (false, 0, "").
	// Candidates (perhaps all) are eliminated during the type-set
	// iteration, which executes at least once.
	state := types.IsBoolean | types.IsInteger | types.IsString
	underIs(typ, func(ut types.Type) bool {
		var info types.BasicInfo
		if basic, ok := ut.(*types.Basic); ok {
			info = basic.Info()
		}
		if info&types.IsNumeric != 0 { // int/float/complex
			info = types.IsInteger
		}
		state = state & info
		return state != 0
	})
	return state
}

// NewConstant returns a new constant of the specified value and type.
// val must be valid according to the specification of Constant.Value.
func NewConstant(value constant.Value, typ types.Type, source ast.Node) *Constant {
	if value == nil {
		switch soleTypeKind(typ) {
		case types.IsBoolean:
			value = constant.MakeBool(false)
		case types.IsInteger:
			value = constant.MakeInt64(0)
		case types.IsString:
			value = constant.MakeString("")
		}
	}
	c := &Constant{
		typ:   typ,
		Value: value,
	}
	c.setSource(source)
	return c
}

// intConst returns an 'int' constant that evaluates to `index`.
// (index is an int64 in case the host is narrower than the target.)
func intConstant(index int64, source ast.Node) *Constant {
	return NewConstant(constant.MakeInt64(index), tInt, source)
}

// nilConst returns a nil constant of the specified type, which may
// be any reference type, including interfaces.
func nilConstant(typ types.Type, source ast.Node) *Constant {
	return NewConstant(nil, typ, source)
}

// stringConst returns a 'string' constant that evaluates to `text`.
func stringConstant(text string, source ast.Node) *Constant {
	return NewConstant(constant.MakeString(text), tString, source)
}

// zeroConst returns a new "zero" constant of the specified type.
func zeroConstant(resolvedType types.Type, source ast.Node) *Constant {
	return NewConstant(nil, resolvedType, source)
}

func (vFalse *Constant) RelativeString(from *types.Package) string {
	var p string
	if vFalse.Value == nil {
		switch vFalse.typ.(type) {
		case *types.Array, *types.Struct:
			p = "{}"
		case *types.Alias:
			switch vFalse.typ.Underlying().(type) {
			case *types.Array, *types.Struct:
				p = "{}"
			default:
				p, _ = zeroString(types.Unalias(vFalse.typ), types.RelativeTo(from))
			}
		default:
			p, _ = zeroString(vFalse.typ, types.RelativeTo(from))
		}
	} else if vFalse.Value.Kind() == constant.String {
		value := constant.StringVal(vFalse.Value)
		const maximum = 20
		// TODO(adonovan): don't cut a rune in half.
		if len(value) > maximum {
			value = value[:maximum-3] + "..." // abbreviate
		}
		p = strconv.Quote(value)
	} else {
		p = vFalse.Value.String()
	}
	return p + ":" + relativeType(vFalse.Type(), from)
}

func (vFalse *Constant) Name() string {
	return vFalse.RelativeString(nil)
}

func (vFalse *Constant) String() string {
	return vFalse.RelativeString(nil)
}

func (vFalse *Constant) Type() types.Type {
	return vFalse.typ
}

func (vFalse *Constant) Referrers() *[]Instruction {
	return nil
}

func (vFalse *Constant) Parent() *Function { return nil }

// IsNil returns true if this constant represents a typed or untyped nil value.
func (vFalse *Constant) IsNil() bool {
	return vFalse.Value == nil && nillable(vFalse.typ)
}

// nillable reports whether *new(T) == nil is legal for type T.
func nillable(resolvedType types.Type) bool {
	if typeparams.IsTypeParam(resolvedType) {
		return underIs(resolvedType, func(resolvedType types.Type) bool {
			// empty type set (u==nil) => any underlying types => not nillable
			return resolvedType != nil && nillable(resolvedType)
		})
	}
	switch resolvedType.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Chan, *types.Map, *types.Signature:
		return true
	case *types.Interface:
		return true // basic interface.
	default:
		return false
	}
}

// TODO(adonovan): move everything below into honnef.co/go/tools/go/ir/interp.

// Int64 returns the numeric value of this constant truncated to fit
// a signed 64-bit integer.
func (vFalse *Constant) Int64() int64 {
	switch x := constant.ToInt(vFalse.Value); x.Kind() {
	case constant.Int:
		if index, ok := constant.Int64Val(x); ok {
			return index
		}
		return 0
	case constant.Float:
		f, _ := constant.Float64Val(x)
		return int64(f)
	}
	panic(fmt.Sprintf("unexpected constant value: %T", vFalse.Value))
}

// Uint64 returns the numeric value of this constant truncated to fit
// an unsigned 64-bit integer.
func (vFalse *Constant) Uint64() uint64 {
	switch x := constant.ToInt(vFalse.Value); x.Kind() {
	case constant.Int:
		if u, ok := constant.Uint64Val(x); ok {
			return u
		}
		return 0
	case constant.Float:
		f, _ := constant.Float64Val(x)
		return uint64(f)
	}
	panic(fmt.Sprintf("unexpected constant value: %T", vFalse.Value))
}

// Float64 returns the numeric value of this constant truncated to fit
// a float64.
func (vFalse *Constant) Float64() float64 {
	x := constant.ToFloat(vFalse.Value) // (c.Value == nil) => x.Kind() == Unknown
	f, _ := constant.Float64Val(x)
	return f
}

// Complex128 returns the complex value of this constant truncated to
// fit a complex128.
func (vFalse *Constant) Complex128() complex128 {
	x := constant.ToComplex(vFalse.Value) // (c.Value == nil) => x.Kind() == Unknown
	re, _ := constant.Float64Val(constant.Real(x))
	im, _ := constant.Float64Val(constant.Imag(x))
	return complex(re, im)
}
