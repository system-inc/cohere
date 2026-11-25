// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strconv"

	"golang.org/x/exp/typeparams"
)

// A Constant represents a value known at build time.
//
// Constants include true constants of boolean, numeric, and string types, as
// defined by the Go spec; these are represented by a non-nil Value field.
//
// Constants also include the "zero" value of any type, of which the nil values
// of various pointer-like types are a special case; these are represented
// by a nil Value field.
//
// Example printed form:
//
//	42:int
//	"hello":untyped string
//	3+4i:MyComplex
//	nil:*int
//	nil:[]string
//	[3]int{}:[3]int
//	struct{x string}{}:struct{x string}
//	0:interface{int|int64}
//	nil:interface{bool|int} // no go/constant representation
type Constant struct {
	node

	valueType types.Type
	Value     constant.Value
}

func (irConstant *Constant) Operands(rands []*Value) []*Value { return rands }

// This file defines the Constant SSA value type.

// soleTypeKind returns a BasicInfo for which constant.Value can
// represent all zero values for the types in the type set.
//
//	types.IsBoolean for false is a representative.
//	types.IsInteger for 0
//	types.IsString for ""
//	0 otherwise.
func soleTypeKind(goType types.Type) types.BasicInfo {
	// State records the set of possible zero values (false, 0, "").
	// Candidates (perhaps all) are eliminated during the type-set
	// iteration, which executes at least once.
	state := types.IsBoolean | types.IsInteger | types.IsString
	underIs(goType, func(ut types.Type) bool {
		var info types.BasicInfo
		if basic, ok := ut.(*types.Basic); ok {
			info = basic.Info()
		}
		if info&types.IsNumeric != 0 { // int/float/complex
			info = types.IsInteger
		}
		state &= info
		return state != 0
	})
	return state
}

// NewConstant returns a new constant of the specified value and type.
// val must be valid according to the specification of Constant.Value.
func NewConstant(value constant.Value, constantType types.Type, source ast.Node) *Constant {
	constantValue := value
	if value == nil {
		switch soleTypeKind(constantType) {
		case types.IsBoolean:
			constantValue = constant.MakeBool(false)
		case types.IsInteger:
			constantValue = constant.MakeInt64(0)
		case types.IsString:
			constantValue = constant.MakeString("")
		}
	}
	irConstant := &Constant{
		valueType: constantType,
		Value:     constantValue,
	}
	irConstant.setSource(source)
	return irConstant
}

// intConst returns an 'int' constant that evaluates to `index`.
// (index is an int64 in case the host is narrower than the target.)
func intConstant(index int64, source ast.Node) *Constant {
	return NewConstant(constant.MakeInt64(index), tInt, source)
}

// nilConst returns a nil constant of the specified type, which may
// be any reference type, including interfaces.
func nilConstant(constantType types.Type, source ast.Node) *Constant {
	return NewConstant(nil, constantType, source)
}

// stringConst returns a 'string' constant that evaluates to `text`.
func stringConstant(text string, source ast.Node) *Constant {
	return NewConstant(constant.MakeString(text), tString, source)
}

// zeroConst returns a new "zero" constant of the specified type.
func zeroConstant(resolvedType types.Type, source ast.Node) *Constant {
	return NewConstant(nil, resolvedType, source)
}

func (irConstant *Constant) RelativeString(from *types.Package) string {
	var valueText string
	if irConstant.Value == nil {
		switch irConstant.valueType.(type) {
		case *types.Array, *types.Struct:
			valueText = "{}"
		case *types.Alias:
			switch irConstant.valueType.Underlying().(type) {
			case *types.Array, *types.Struct:
				valueText = "{}"
			default:
				valueText, _ = zeroString(types.Unalias(irConstant.valueType), types.RelativeTo(from))
			}
		default:
			valueText, _ = zeroString(irConstant.valueType, types.RelativeTo(from))
		}
	} else if irConstant.Value.Kind() == constant.String {
		value := constant.StringVal(irConstant.Value)
		const maximum = 20
		// The abbreviation cuts at a byte, so it may cut a rune in half.
		if len(value) > maximum {
			value = value[:maximum-3] + "..." // abbreviate
		}
		valueText = strconv.Quote(value)
	} else {
		valueText = irConstant.Value.String()
	}
	return valueText + ":" + relativeType(irConstant.Type(), from)
}

func (irConstant *Constant) Name() string {
	return irConstant.RelativeString(nil)
}

func (irConstant *Constant) String() string {
	return irConstant.RelativeString(nil)
}

func (irConstant *Constant) Type() types.Type {
	return irConstant.valueType
}

func (irConstant *Constant) Referrers() *[]Instruction {
	return nil
}

func (irConstant *Constant) Parent() *Function { return nil }

// IsNil returns true if this constant represents a typed or untyped nil value.
func (irConstant *Constant) IsNil() bool {
	return irConstant.Value == nil && nillable(irConstant.valueType)
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

// The methods below read a constant's value as a Go number, the way an interpreter would.

// Int64 returns the numeric value of this constant truncated to fit
// a signed 64-bit integer.
func (irConstant *Constant) Int64() int64 {
	switch x := constant.ToInt(irConstant.Value); x.Kind() {
	case constant.Int:
		if index, ok := constant.Int64Val(x); ok {
			return index
		}
		return 0
	case constant.Float:
		floatValue, _ := constant.Float64Val(x)
		return int64(floatValue)
	}
	// Invariant: the builder asks for Int64 only of an array index, length or capacity, which go/types holds to be a
	// constant representable as an integer.
	panic(fmt.Sprintf("unexpected constant value: %T", irConstant.Value))
}

// Uint64 returns the numeric value of this constant truncated to fit
// an unsigned 64-bit integer.
func (irConstant *Constant) Uint64() uint64 {
	switch x := constant.ToInt(irConstant.Value); x.Kind() {
	case constant.Int:
		if unsignedValue, ok := constant.Uint64Val(x); ok {
			return unsignedValue
		}
		return 0
	case constant.Float:
		floatValue, _ := constant.Float64Val(x)
		return uint64(floatValue)
	}
	// Invariant: nothing in this package calls Uint64, and it is for a constant representable as an integer, as
	// Int64 is.
	panic(fmt.Sprintf("unexpected constant value: %T", irConstant.Value))
}

// Float64 returns the numeric value of this constant truncated to fit
// a float64.
func (irConstant *Constant) Float64() float64 {
	x := constant.ToFloat(irConstant.Value) // (c.Value == nil) => x.Kind() == Unknown
	floatValue, _ := constant.Float64Val(x)
	return floatValue
}

// Complex128 returns the complex value of this constant truncated to
// fit a complex128.
func (irConstant *Constant) Complex128() complex128 {
	x := constant.ToComplex(irConstant.Value) // (c.Value == nil) => x.Kind() == Unknown
	re, _ := constant.Float64Val(constant.Real(x))
	im, _ := constant.Float64Val(constant.Imag(x))
	return complex(re, im)
}
