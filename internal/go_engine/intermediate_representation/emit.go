// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// Helpers for emitting IR instructions.

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/exp/typeparams"
)

// emitAllocation emits to `function` a new Allocation instruction allocating a variable
// of type typ.
//
// The caller must set Allocation.Heap=true (for a heap-allocated variable)
// or add the Allocation to f.Locals (for a frame-allocated variable).
//
// During building, a variable in f.Locals may have its Heap flag
// set when it is discovered that its address is taken.
// These Allocations are removed from f.Locals at the end.
//
// The builder should generally call one of the emit{New,Local,LocalVar} wrappers instead.
func emitAllocation(function *Function, typ types.Type, source ast.Node, comment string) *Allocation {
	allocation := &Allocation{}
	allocation.comment = comment
	allocation.setType(types.NewPointer(typ))
	function.emit(allocation, source)
	return allocation
}

// emitNew emits to `function` a new Allocation instruction heap-allocating a
// variable of type typ.
func emitNew(function *Function, typ types.Type, source ast.Node, comment string) *Allocation {
	alloc := emitAllocation(function, typ, source, comment)
	alloc.Heap = true
	return alloc
}

// emitLocal creates a local var for (resolvedType, source, comment) and
// emits an Allocation instruction for it.
//
// (Use this function or emitNew for synthetic variables;
// for source-level variables in the same function, use emitLocalVar.)
//
// Mutates: function gains the allocation among its frame locals
func emitLocal(function *Function, resolvedType types.Type, source ast.Node, comment string) *Allocation {
	local := emitAllocation(function, resolvedType, source, comment)
	function.Locals = append(function.Locals, local)
	return local
}

// emitLocalVariable creates a local var for `variable` and emits an Allocation instruction for it.
// Subsequent calls to f.lookup(variable) return it.
// It applies the appropriate generic instantiation to the type.
func emitLocalVariable(function *Function, variable *types.Var, source ast.Node) *Allocation {
	alloc := emitLocal(function, function.typ(variable.Type()), source, variable.Name())
	function.vars[variable] = alloc
	return alloc
}

// emitLoad emits to `function` an instruction to load the address addr into a
// new temporary, and returns the value so defined.
func emitLoad(function *Function, addr Value, source ast.Node) *Load {
	load := &Load{X: addr}
	load.setType(deref(addr.Type()))
	function.emit(load, source)
	return load
}

func emitReceive(function *Function, channel Value, commaOk bool, typ types.Type, source ast.Node) Value {
	recv := &Receive{
		Channel: channel,
		CommaOk: commaOk,
	}
	recv.setType(typ)
	return function.emit(recv, source)
}

// emitArith emits to `function` code to compute the binary operation op(x, y)
// where op is an eager shift, logical or arithmetic operation.
// (Use emitCompare() for comparisons and Builder.logicalBinop() for
// non-eager operations.)
func emitArith(function *Function, op token.Token, x, y Value, resolvedType types.Type, source ast.Node) Value {
	operandX, operandY := x, y
	switch op {
	case token.SHL, token.SHR:
		operandX = emitConv(function, x, resolvedType, source)
		// y may be signed or an 'untyped' constant.
		// There is a runtime panic if y is signed and <0. Instead of inserting a check for y<0
		// and converting to an unsigned value (like the compiler) leave y as is.
		if basic, ok := y.Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsUntyped != 0 {
			// Untyped conversion:
			// Spec https://go.dev/ref/spec#Operators:
			// The right operand in a shift expression must have integer type or be an untyped constant
			// representable by a value of type uint.
			operandY = emitConv(function, y, types.Typ[types.Uint], source)
		}

	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM, token.AND, token.OR, token.XOR, token.AND_NOT:
		operandX = emitConv(function, x, resolvedType, source)
		operandY = emitConv(function, y, resolvedType, source)

	default:
		// Invariant: callers pass only the operators of shift, logical and arithmetic expressions and of
		// op-assignments, since comparisons go to emitCompare and && and || go to logicalBinop.
		panic("illegal op in emitArith: " + op.String())

	}
	operation := &BinaryOperation{
		Operator: op,
		X:        operandX,
		Y:        operandY,
	}
	operation.setType(resolvedType)
	return function.emit(operation, source)
}

// emitCompare emits to `function` code compute the boolean result of
// comparison 'x op y'.
func emitCompare(function *Function, op token.Token, x, y Value, source ast.Node) Value {
	xt := x.Type().Underlying()
	yt := y.Type().Underlying()

	// Special case to optimise a tagless SwitchStmt so that
	// these are equivalent
	//   switch { case e: ...}
	//   switch true { case e: ... }
	//   if e==true { ... }
	// even in the case when e's type is an interface.
	// Only a constant true on the left of == is optimised; x==true, false!=y and the like are not.
	if x, ok := x.(*Constant); ok &&
		op == token.EQL &&
		x.Value != nil &&
		x.Value.Kind() == constant.Bool &&
		constant.BoolVal(x.Value) {
		if yt, ok := yt.(*types.Basic); ok && yt.Info()&types.IsBoolean != 0 {
			return y
		}
	}

	operandX, operandY := x, y
	if types.Identical(xt, yt) {
		// no conversion necessary
	} else if _, ok := xt.(*types.Interface); ok && !typeparams.IsTypeParam(x.Type()) {
		operandY = emitConv(function, y, x.Type(), source)
	} else if _, ok := yt.(*types.Interface); ok && !typeparams.IsTypeParam(y.Type()) {
		operandX = emitConv(function, x, y.Type(), source)
	} else if _, ok := x.(*Constant); ok {
		operandX = emitConv(function, x, y.Type(), source)
	} else if _, ok := y.(*Constant); ok {
		operandY = emitConv(function, y, x.Type(), source)
		//lint:ignore SA9003 no-op
	} else {
		// other cases, e.g. channels.  No-op.
	}

	operation := &BinaryOperation{
		Operator: op,
		X:        operandX,
		Y:        operandY,
	}
	operation.setType(tBool)
	return function.emit(operation, source)
}

// isValuePreserving returns true if a conversion from ut_src to
// ut_dst is value-preserving, i.e. just a change of type.
// Precondition: neither argument is a named or alias type.
func isValuePreserving(fromUnderlyingType, toUnderlyingType types.Type) bool {
	// Identical underlying types?
	if types.IdenticalIgnoreTags(toUnderlyingType, fromUnderlyingType) {
		return true
	}

	switch toUnderlyingType.(type) {
	case *types.Chan:
		// Conversion between channel types?
		_, ok := fromUnderlyingType.(*types.Chan)
		return ok

	case *types.Pointer:
		// Conversion between pointers with identical base types?
		_, ok := fromUnderlyingType.(*types.Pointer)
		return ok
	}
	return false
}

// emitConv emits to `function` code to convert Value val to exactly type typ,
// and returns the converted value.  Implicit conversions are required
// by language assignability rules in assignments, parameter passing,
// etc.
func emitConv(function *Function, value Value, toType types.Type, source ast.Node) Value {
	fromType := value.Type()

	// Identical types?  Conversion is a no-op.
	if types.Identical(fromType, toType) {
		return value
	}

	toUnderlyingType := toType.Underlying()
	fromUnderlyingType := fromType.Underlying()

	// Conversion to, or construction of a value of, an interface type?
	if isNonTypeParamInterface(toType) {
		// Interface name change?
		if isValuePreserving(fromUnderlyingType, toUnderlyingType) {
			changeType := &ChangeType{X: value}
			changeType.setType(toType)
			return function.emit(changeType, source)
		}

		// Assignment from one interface type to another?
		if isNonTypeParamInterface(fromType) {
			changeInterface := &ChangeInterface{X: value}
			changeInterface.setType(toType)
			return function.emit(changeInterface, source)
		}

		// Untyped nil constant?  Return interface-typed nil constant.
		if fromUnderlyingType == tUntypedNil {
			return zeroConstant(toType, source)
		}

		// Convert (non-nil) "untyped" literals to their default type.
		typedValue := value
		if basic, ok := fromUnderlyingType.(*types.Basic); ok && basic.Info()&types.IsUntyped != 0 {
			typedValue = emitConv(function, value, types.Default(fromUnderlyingType), source)
		}

		mi := &MakeInterface{X: typedValue}
		mi.setType(toType)
		return function.emit(mi, source)
	}

	// conversionCase describes an instruction pattern that maybe emitted to
	// model d <- s for d in dst_terms and s in src_terms.
	// Multiple conversions can match the same pattern.
	type conversionCase uint8
	const (
		changeType conversionCase = 1 << iota
		sliceToArray
		sliceToArrayPtr
		sliceTo0Array
		sliceTo0ArrayPtr
		convert
	)
	// classify the conversion case of a source type us to a destination type ud.
	// us and ud are underlying types (not *Named or *Alias)
	classify := func(fromTerm, toTerm types.Type) conversionCase {
		// Just a change of type, but not value or representation?
		if isValuePreserving(fromTerm, toTerm) {
			return changeType
		}

		// Conversion from slice to array or slice to array pointer?
		if slice, ok := fromTerm.(*types.Slice); ok {
			var array *types.Array
			var ptr bool
			// Conversion from slice to array pointer?
			switch typed := toTerm.(type) {
			case *types.Array:
				array = typed
			case *types.Pointer:
				array, _ = typed.Elem().Underlying().(*types.Array)
				ptr = true
			}
			if array != nil && types.Identical(slice.Elem(), array.Elem()) {
				if array.Len() == 0 {
					if ptr {
						return sliceTo0ArrayPtr
					}
					return sliceTo0Array
				}
				if ptr {
					return sliceToArrayPtr
				}
				return sliceToArray
			}
		}

		// The only remaining case in well-typed code is a representation-
		// changing conversion of basic types (possibly with []byte/[]rune).
		if !isBasic(fromTerm) && !isBasic(toTerm) {
			// Invariant: go/types accepts only well-typed conversions, and every case of those but this one returned
			// above, so at least one of the terms is basic.
			panic(fmt.Sprintf(
				"in %s: cannot convert term %s (%s [within %s]) to type %s [within %s]",
				function,
				value,
				value.Type(),
				fromTerm,
				toType,
				toTerm,
			))
		}
		return convert
	}

	var classifications conversionCase
	underIs(fromUnderlyingType, func(us types.Type) bool {
		return underIs(toUnderlyingType, func(ud types.Type) bool {
			if us != nil && ud != nil {
				classifications |= classify(us, ud)
			}
			return classifications != 0
		})
	})
	if classifications == 0 {
		// Invariant: go/types accepts a conversion between type sets only when every pair of specific terms
		// converts, so both sides have terms here, and classify returns a nonzero case for each pair or panics.
		panic(fmt.Sprintf("in %s: cannot convert %s (%s) to %s", function, value, value.Type(), toType))
	}

	// Conversion of a compile-time constant value?
	if irConstant, ok := value.(*Constant); ok {
		// Conversion to a basic type?
		if isBasic(toUnderlyingType) {
			// Conversion of a compile-time constant to
			// another constant type results in a new
			// constant of the destination type and
			// (initially) the same abstract value.
			// We don't truncate the value yet.
			return NewConstant(irConstant.Value, toType, source)
		}
		// Can we always convert from zero value without panicking?
		const mayPanic = sliceToArray | sliceToArrayPtr
		if irConstant.Value == nil && classifications&mayPanic == 0 {
			return NewConstant(nil, toType, source)
		}

		// We're converting from constant to non-constant type,
		// e.g. string -> []byte/[]rune.
	}

	switch classifications {
	case changeType: // representation-preserving change
		changeType := &ChangeType{X: value}
		changeType.setType(toType)
		return function.emit(changeType, source)

	case sliceToArrayPtr, sliceTo0ArrayPtr: // slice to array pointer
		pointer := &SliceToArrayPointer{X: value}
		pointer.setType(toType)
		return function.emit(pointer, source)

	case sliceToArray: // slice to arrays (not zero-length)
		array := &SliceToArray{X: value}
		array.setType(toType)
		return function.emit(array, source)

	case sliceTo0Array: // slice to zero-length arrays (constant)
		return zeroConstant(toType, source)

	case convert: // representation-changing conversion
		convert := &Convert{X: value}
		convert.setType(toType)
		return function.emit(convert, source)

	default: // The conversion represents a cross product.
		convert := &MultiConvert{X: value, from: fromType, to: toType}
		convert.setType(toType)
		return function.emit(convert, source)
	}
}

// emitTypeCoercion emits to `function` code to coerce the type of a
// Value `value` to exactly type typ, and returns the coerced value.
//
// Requires that coercing v.Typ() to typ is a value preserving change.
//
// Currently used only when v.Type() is a type instance of typ or vice versa.
// A type `value` is a type instance of a type t if there exists a
// type parameter substitution σ s.t. σ(value) == t. Example:
//
//	σ(func(T) T) == func(int) int for σ == [T ↦ int]
//
// This happens in instantiation wrappers for conversion
// from an instantiation to a parameterized type (and vice versa)
// with σ substituting f.typeparams by f.typeargs.
func emitTypeCoercion(function *Function, value Value, typ types.Type, source ast.Node) Value {
	if types.Identical(value.Type(), typ) {
		return value // no coercion needed
	}
	// The coercion does not record which side is the instance.
	changeType := &ChangeType{
		X: value,
	}
	changeType.setType(typ)
	function.emit(changeType, source)
	return changeType
}

// emitStore emits to `function` an instruction to store value val at location
// addr, applying implicit conversions as required by assignability rules.
func emitStore(function *Function, addr, value Value, source ast.Node) *Store {
	typ := deref(addr.Type())
	store := &Store{
		Address: addr,
		Value:   emitConv(function, value, typ, source),
	}
	function.emit(store, source)
	return store
}

// emitJump emits to `function` a jump to target, and updates the control-flow graph.
// Postcondition: f.currentBlock is nil.
//
// Mutates: function ends its current block with the jump
func emitJump(function *Function, target *BasicBlock, source ast.Node) *Jump {
	block := function.currentBlock
	jump := new(Jump)
	block.emit(jump, source)
	addEdge(block, target)
	function.currentBlock = nil
	return jump
}

// emitIf emits to `function` a conditional jump to tblock or fblock based on
// cond, and updates the control-flow graph.
// Postcondition: f.currentBlock is nil.
//
// Mutates: function ends its current block with the conditional jump
func emitIf(function *Function, cond Value, tblock, fblock *BasicBlock, source ast.Node) *If {
	block := function.currentBlock
	stmt := &If{Condition: cond}
	block.emit(stmt, source)
	addEdge(block, tblock)
	addEdge(block, fblock)
	function.currentBlock = nil
	return stmt
}

// emitExtract emits to `function` an instruction to extract the index'th
// component of tuple.  It returns the extracted value.
func emitExtract(function *Function, tuple Value, index int, source ast.Node) Value {
	extract := &Extract{Tuple: tuple, Index: index}
	tupleType, isTuple := tuple.Type().(*types.Tuple)
	if !isTuple {
		// Invariant: callers extract only from a multi-value call, a comma-ok form, a select, a range step or a
		// type switch, and each of those values has a tuple type.
		panic(fmt.Sprintf("emitExtract: %s has type %s, not a tuple", tuple, tuple.Type()))
	}
	extract.setType(tupleType.At(index).Type())
	return function.emit(extract, source)
}

// emitTypeAssert emits to `function` a type assertion value := x.(resolvedType) and
// returns the value.  x.Type() must be an interface.
func emitTypeAssert(function *Function, x Value, resolvedType types.Type, source ast.Node) Value {
	assert := &TypeAssert{X: x, AssertedType: resolvedType}
	assert.setType(resolvedType)
	return function.emit(assert, source)
}

// emitTypeTest emits to `function` a type test value,ok := x.(resolvedType) and returns
// a (value, ok) tuple.  x.Type() must be an interface.
func emitTypeTest(function *Function, x Value, resolvedType types.Type, source ast.Node) Value {
	assert := &TypeAssert{
		X:            x,
		AssertedType: resolvedType,
		CommaOk:      true,
	}
	assert.setType(types.NewTuple(
		newVariable("value", resolvedType),
		varOk,
	))
	return function.emit(assert, source)
}

// emitTailCall emits to `function` a function call in tail position.  The
// caller is responsible for all fields of 'call' except its type.
// Intended for wrapper methods.
// Precondition: `function` does/will not use deferred procedure calls.
// Postcondition: f.currentBlock is nil.
//
// Mutates: call takes its type from function's results
// Mutates: function ends its current block with the call and its return
func emitTailCall(function *Function, call *Call, source ast.Node) {
	tresults := function.Signature.Results()
	nr := tresults.Len()
	if nr == 1 {
		call.typ = tresults.At(0).Type()
	} else {
		call.typ = tresults
	}
	tuple := emitCall(function, call, source)
	var ret Return
	switch nr {
	case 0:
		// no-op
	case 1:
		ret.Results = []Value{tuple}
	default:
		for index := range nr {
			value := emitExtract(function, tuple, index, source)
			// In principle each value needs converting to its result type,
			//   v = emitConv(f, o.Type, f.Signature.Results[i].Type)
			// but emitTailCall is only used when the types exactly
			// match, so it extracts without converting.
			ret.Results = append(ret.Results, value)
		}
	}

	function.emit(&ret, source)
	function.currentBlock = nil
}

// emitCall emits a call instruction. If the callee is "no return",
// it also emits a panic to eliminate infeasible CFG edges.
//
// Mutates: function moves on to an unreachable block after a call that does not return
func emitCall(function *Function, call *Call, source ast.Node) Value {
	result := function.emit(call, source)

	callee := call.Call.StaticCallee()
	if callee != nil &&
		callee.object != nil &&
		function.Program.noReturn != nil &&
		function.Program.noReturn(callee.object) {
		// Call doesn't return normally. Either it doesn't return at all
		// (infinitely blocked or exitting the process), or it unwinds the stack
		// (panic, runtime.Goexit). Model this as a panic.
		function.emit(&Panic{
			X: emitConv(function, vNoReturn, tEface, source),
		}, source)
		function.currentBlock = function.newBasicBlock("unreachable")
	}

	return result
}

// emitImplicitSelections emits to `function` code to apply the sequence of
// implicit field selections specified by indices to base value `value`, and
// returns the selected value.
//
// If `value` is the address of a struct, the result will be the address of
// a field; if it is the value of a struct, the result will be the
// value of a field.
func emitImplicitSelections(function *Function, value Value, indices []int, source ast.Node) Value {
	selected := value
	for _, index := range indices {
		// We may have a generic type containing a pointer, or a pointer to a generic type containing a struct. A
		// pointer to a generic containing a pointer to a struct shouldn't be possible because the outer pointer gets
		// dereferenced implicitly before we get here.
		if isPointerCore(selected.Type()) {
			fld := fieldOf(deref(selected.Type()), index)
			instr := &FieldAddress{
				X:     selected,
				Field: index,
			}
			instr.setType(types.NewPointer(fld.Type()))
			selected = function.emit(instr, source)
			// Load the field's value iff indirectly embedded.
			if isPointerCore(fld.Type()) {
				selected = emitLoad(function, selected, source)
			}
		} else {
			fld := fieldOf(selected.Type(), index)
			instr := &Field{
				X:     selected,
				Field: index,
			}
			instr.setType(fld.Type())
			selected = function.emit(instr, source)
		}
	}
	return selected
}

// emitFieldSelection emits to `function` code to select the index'th field of `value`.
//
// If wantAddr, the input must be a pointer-to-struct and the result
// will be the field's address; otherwise the result will be the
// field's value.
// Ident id is used for position and debug info.
func emitFieldSelection(function *Function, value Value, index int, wantAddr bool, id *ast.Ident) Value {
	var selected Value
	if isPointerCore(value.Type()) {
		fld := fieldOf(deref(value.Type()), index)
		instr := &FieldAddress{
			X:     value,
			Field: index,
		}
		instr.setSource(id)
		instr.setType(types.NewPointer(fld.Type()))
		selected = function.emit(instr, id)
		// Load the field's value iff we don't want its address.
		if !wantAddr {
			selected = emitLoad(function, selected, id)
		}
	} else {
		fld := fieldOf(value.Type(), index)
		instr := &Field{
			X:     value,
			Field: index,
		}
		instr.setSource(id)
		instr.setType(fld.Type())
		selected = function.emit(instr, id)
	}
	emitDebugReference(function, id, selected, wantAddr)
	return selected
}

// createRecoverBlock emits to `function` a block of code to return after a
// recovered panic, and sets f.Recover to it.
//
// If `function`'s result parameters are named, the code loads and returns
// their current values, otherwise it returns the zero values of their
// type.
//
// Idempotent.
//
// Mutates: function gains the recover block, and its current block is restored after building it
func createRecoverBlock(function *Function) {
	if function.Recover != nil {
		return // already created
	}
	saved := function.currentBlock

	function.Recover = function.newBasicBlock("recover")
	function.currentBlock = function.Recover

	var results []Value
	// Reload NRPs to form value tuple.
	for _, nr := range function.results {
		results = append(results, emitLoad(function, nr, nr.source))
	}

	function.emit(&Return{Results: results}, nil)

	function.currentBlock = saved
}
