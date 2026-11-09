// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines the Const SSA value type.

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
		var c types.BasicInfo
		if t, ok := ut.(*types.Basic); ok {
			c = t.Info()
		}
		if c&types.IsNumeric != 0 { // int/float/complex
			c = types.IsInteger
		}
		state = state & c
		return state != 0
	})
	return state
}

// NewConst returns a new constant of the specified value and type.
// val must be valid according to the specification of Const.Value.
func NewConstant(val constant.Value, typ types.Type, source ast.Node) *Constant {
	if val == nil {
		switch soleTypeKind(typ) {
		case types.IsBoolean:
			val = constant.MakeBool(false)
		case types.IsInteger:
			val = constant.MakeInt64(0)
		case types.IsString:
			val = constant.MakeString("")
		}
	}
	c := &Constant{
		typ:   typ,
		Value: val,
	}
	c.setSource(source)
	return c
}

// intConst returns an 'int' constant that evaluates to i.
// (i is an int64 in case the host is narrower than the target.)
func intConstant(i int64, source ast.Node) *Constant {
	return NewConstant(constant.MakeInt64(i), tInt, source)
}

// nilConst returns a nil constant of the specified type, which may
// be any reference type, including interfaces.
func nilConstant(typ types.Type, source ast.Node) *Constant {
	return NewConstant(nil, typ, source)
}

// stringConst returns a 'string' constant that evaluates to s.
func stringConstant(s string, source ast.Node) *Constant {
	return NewConstant(constant.MakeString(s), tString, source)
}

// zeroConst returns a new "zero" constant of the specified type.
func zeroConstant(t types.Type, source ast.Node) *Constant {
	return NewConstant(nil, t, source)
}

func (c *Constant) RelativeString(from *types.Package) string {
	var p string
	if c.Value == nil {
		switch c.typ.(type) {
		case *types.Array, *types.Struct:
			p = "{}"
		case *types.Alias:
			switch c.typ.Underlying().(type) {
			case *types.Array, *types.Struct:
				p = "{}"
			default:
				p, _ = zeroString(types.Unalias(c.typ), types.RelativeTo(from))
			}
		default:
			p, _ = zeroString(c.typ, types.RelativeTo(from))
		}
	} else if c.Value.Kind() == constant.String {
		v := constant.StringVal(c.Value)
		const max = 20
		// TODO(adonovan): don't cut a rune in half.
		if len(v) > max {
			v = v[:max-3] + "..." // abbreviate
		}
		p = strconv.Quote(v)
	} else {
		p = c.Value.String()
	}
	return p + ":" + relativeType(c.Type(), from)
}

func (c *Constant) Name() string {
	return c.RelativeString(nil)
}

func (c *Constant) String() string {
	return c.RelativeString(nil)
}

func (c *Constant) Type() types.Type {
	return c.typ
}

func (c *Constant) Referrers() *[]Instruction {
	return nil
}

func (c *Constant) Parent() *Function { return nil }

// IsNil returns true if this constant represents a typed or untyped nil value.
func (c *Constant) IsNil() bool {
	return c.Value == nil && nillable(c.typ)
}

// nillable reports whether *new(T) == nil is legal for type T.
func nillable(t types.Type) bool {
	if typeparams.IsTypeParam(t) {
		return underIs(t, func(u types.Type) bool {
			// empty type set (u==nil) => any underlying types => not nillable
			return u != nil && nillable(u)
		})
	}
	switch t.Underlying().(type) {
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
func (c *Constant) Int64() int64 {
	switch x := constant.ToInt(c.Value); x.Kind() {
	case constant.Int:
		if i, ok := constant.Int64Val(x); ok {
			return i
		}
		return 0
	case constant.Float:
		f, _ := constant.Float64Val(x)
		return int64(f)
	}
	panic(fmt.Sprintf("unexpected constant value: %T", c.Value))
}

// Uint64 returns the numeric value of this constant truncated to fit
// an unsigned 64-bit integer.
func (c *Constant) Uint64() uint64 {
	switch x := constant.ToInt(c.Value); x.Kind() {
	case constant.Int:
		if u, ok := constant.Uint64Val(x); ok {
			return u
		}
		return 0
	case constant.Float:
		f, _ := constant.Float64Val(x)
		return uint64(f)
	}
	panic(fmt.Sprintf("unexpected constant value: %T", c.Value))
}

// Float64 returns the numeric value of this constant truncated to fit
// a float64.
func (c *Constant) Float64() float64 {
	x := constant.ToFloat(c.Value) // (c.Value == nil) => x.Kind() == Unknown
	f, _ := constant.Float64Val(x)
	return f
}

// Complex128 returns the complex value of this constant truncated to
// fit a complex128.
func (c *Constant) Complex128() complex128 {
	x := constant.ToComplex(c.Value) // (c.Value == nil) => x.Kind() == Unknown
	re, _ := constant.Float64Val(constant.Real(x))
	im, _ := constant.Float64Val(constant.Imag(x))
	return complex(re, im)
}
