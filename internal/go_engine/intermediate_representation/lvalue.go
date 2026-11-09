// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// lvalues are the union of addressable expressions and map-index
// expressions.

import (
	"go/ast"
	"go/types"
)

// An lvalue represents an assignable location that may appear on the
// left-hand side of an assignment.  This is a generalization of a
// pointer to permit updates to elements of maps.
type lvalue interface {
	store(function *Function, value Value, source ast.Node) // stores v into the location
	load(function *Function, source ast.Node) Value         // loads the contents of the location
	address(function *Function) Value                       // address of the location
	typ() types.Type                                        // returns the type of the location
}

// An address is an lvalue represented by a true pointer.
type address struct {
	addr Value // must have a pointer core type.
}

func (address *address) load(function *Function, source ast.Node) Value {
	return emitLoad(function, address.addr, source)
}

func (address *address) store(function *Function, value Value, source ast.Node) {
	emitStore(function, address.addr, value, source)
}

func (address *address) address(function *Function) Value {
	return address.addr
}

func (address *address) typ() types.Type {
	return deref(address.addr.Type())
}

type compositeElement struct {
	cv          *CompositeValue
	index       int
	elementType types.Type
}

func (element *compositeElement) load(function *Function, source ast.Node) Value {
	panic("not implemented")
}

func (element *compositeElement) store(function *Function, value Value, source ast.Node) {
	value = emitConv(function, value, element.elementType, source)
	element.cv.Values[element.index] = value
}

func (element *compositeElement) address(function *Function) Value {
	panic("not implemented")
}

func (element *compositeElement) typ() types.Type {
	return element.elementType
}

// An element is an lvalue represented by m[k], the location of an
// element of a map.  These locations are not addressable
// since pointers cannot be formed from them, but they do support
// load() and store().
type element struct {
	mapValue, key Value      // map
	elementType   types.Type // map element type
}

func (element *element) load(function *Function, source ast.Node) Value {
	lookup := &MapLookup{
		X:     element.mapValue,
		Index: element.key,
	}
	lookup.setType(element.elementType)
	return function.emit(lookup, source)
}

func (element *element) store(function *Function, value Value, source ast.Node) {
	up := &MapUpdate{
		Map:   element.mapValue,
		Key:   element.key,
		Value: emitConv(function, value, element.elementType, source),
	}
	function.emit(up, source)
}

func (element *element) address(function *Function) Value {
	panic("map elements are not addressable")
}

func (element *element) typ() types.Type {
	return element.elementType
}

// A lazyAddress is an lvalue whose address is the result of an instruction.
// These work like an *address except a new address.address() Value
// is created on each load, store and address call.
// A lazyAddress can be used to control when a side effect (nil pointer
// dereference, index out of bounds) of using a location happens.
type lazyAddress struct {
	addr         func(function *Function) Value // emit to fn the computation of the address
	locationType types.Type                     // type of the location
}

func (address *lazyAddress) load(function *Function, source ast.Node) Value {
	load := emitLoad(function, address.addr(function), source)
	return load
}

func (address *lazyAddress) store(function *Function, value Value, source ast.Node) {
	emitStore(function, address.addr(function), value, source)
}

func (address *lazyAddress) address(function *Function) Value {
	return address.addr(function)
}

func (address *lazyAddress) typ() types.Type { return address.locationType }

// A blank is a dummy variable whose name is "_".
// It is not reified: loads are illegal and stores are ignored.
type blank struct{}

func (blank blank) load(function *Function, source ast.Node) Value {
	panic("blank.load is illegal")
}

func (blank blank) store(function *Function, value Value, source ast.Node) {
	store := &BlankStore{
		Value: value,
	}
	function.emit(store, source)
}

func (blank blank) address(function *Function) Value {
	panic("blank var is not addressable")
}

func (blank blank) typ() types.Type {
	// This should be the type of the blank Ident; the typechecker
	// doesn't provide this yet, but fortunately, we don't need it
	// yet either.
	panic("blank.typ is unimplemented")
}
