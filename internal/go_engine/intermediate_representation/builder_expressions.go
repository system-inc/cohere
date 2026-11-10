// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"honnef.co/go/tools/analysis/lint"

	"golang.org/x/exp/typeparams"

	"github.com/system-inc/cohere/internal/go_engine/core_type"
)

// condition emits to fn code to evaluate boolean condition `expression` and jump
// to t or f depending on its value, performing various simplifications.
//
// Postcondition: fn.currentBlock is nil.
//
// Mutates: function gains the blocks of the condition, and its current block moves into them
func (packageBuilder *builder) condition(
	function *Function,
	expression ast.Expr,
	trueBlock, falseBlock *BasicBlock,
) *If {
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		return packageBuilder.condition(function, typed.X, trueBlock, falseBlock)

	case *ast.BinaryExpr:
		switch typed.Op {
		case token.LAND:
			ltrue := function.newBasicBlock("cond.true")
			packageBuilder.condition(function, typed.X, ltrue, falseBlock)
			function.currentBlock = ltrue
			return packageBuilder.condition(function, typed.Y, trueBlock, falseBlock)

		case token.LOR:
			lfalse := function.newBasicBlock("cond.false")
			packageBuilder.condition(function, typed.X, trueBlock, lfalse)
			function.currentBlock = lfalse
			return packageBuilder.condition(function, typed.Y, trueBlock, falseBlock)
		}

	case *ast.UnaryExpr:
		if typed.Op == token.NOT {
			return packageBuilder.condition(function, typed.X, falseBlock, trueBlock)
		}
	}

	// A traditional compiler would simplify "if false" (etc) here
	// but we do not, for better fidelity to the source code.
	//
	// The value of a constant condition may be platform-specific,
	// and may cause blocks that are reachable in some configuration
	// to be hidden from subsequent analyses such as bug-finding tools.
	return emitIf(function, packageBuilder.expression(function, expression), trueBlock, falseBlock, expression)
}

// logicalBinop emits code to fn to evaluate `expression`, a &&- or
// ||-expression whose reified boolean value is wanted.
// The value is returned.
//
// Mutates: function gains the blocks of both operands, and its current block moves on to the done block
func (packageBuilder *builder) logicalBinop(function *Function, expression *ast.BinaryExpr) Value {
	rhs := function.newBasicBlock("binop.rhs")
	done := function.newBasicBlock("binop.done")

	// T(e) = T(e.X) = T(e.Y) after untyped constants have been
	// eliminated.
	// That does not hold in every case: MyBool==MyBool yields UntypedBool.
	resolvedType := function.typeOf(expression)

	var short Value // value of the short-circuit path
	switch expression.Op {
	case token.LAND:
		packageBuilder.condition(function, expression.X, rhs, done)
		short = NewConstant(constant.MakeBool(false), resolvedType, expression)

	case token.LOR:
		packageBuilder.condition(function, expression.X, done, rhs)
		short = NewConstant(constant.MakeBool(true), resolvedType, expression)
	}

	// Is rhs unreachable?
	if rhs.Predecessors == nil {
		// Simplify false&&y to false, true||y to true.
		function.currentBlock = done
		return short
	}

	// Is done unreachable?
	if done.Predecessors == nil {
		// Simplify true&&y (or false||y) to y.
		function.currentBlock = rhs
		return packageBuilder.expression(function, expression.Y)
	}

	// All edges from e.X to done carry the short-circuit value.
	var edges []Value
	for range done.Predecessors {
		edges = append(edges, short)
	}

	// The edge from e.Y to done carries the value of e.Y.
	function.currentBlock = rhs
	edges = append(edges, packageBuilder.expression(function, expression.Y))
	emitJump(function, done, expression)
	function.currentBlock = done

	phi := &Phi{Edges: edges}
	phi.valueType = resolvedType
	phi.comment = expression.Op.String()
	return done.emit(phi, expression)
}

// expressionN lowers a multi-result expression `expression` to IR form, emitting code
// to fn and returning a single Value whose type is a *types.Tuple.
// The caller must access the components via Extract.
//
// Multi-result expressions include CallExprs in a multi-value
// assignment or return statement, and "value,ok" uses of
// TypeAssertExpr, IndexExpr (when X is a map), and Receive.
func (packageBuilder *builder) expressionN(function *Function, expression ast.Expr) Value {
	tuple, isTuple := function.typeOf(expression).(*types.Tuple)
	if !isTuple {
		// Invariant: callers pass only multi-result expressions, a call in a multi-value assignment or return or a
		// comma-ok form, and go/types gives each of those a tuple type.
		panic(fmt.Sprintf("exprN(%T) in %s: type %s is not a tuple", expression, function, function.typeOf(expression)))
	}
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		return packageBuilder.expressionN(function, typed.X)

	case *ast.CallExpr:
		// Currently, no built-in function nor type conversion
		// has multiple results, so we can avoid some of the
		// cases for single-valued CallExpr.
		var call Call
		packageBuilder.setCall(function, typed, &call.Call)
		call.valueType = tuple
		return emitCall(function, &call, typed)

	case *ast.IndexExpr:
		mapt, isMap := core_type.Of(function.typeOf(typed.X)).(*types.Map) // ,ok must be a map.
		if !isMap {
			// Invariant: go/types gives a comma-ok form to an index expression only when it looks up a map.
			panic(fmt.Sprintf("exprN: comma-ok index of %s, not a map", function.typeOf(typed.X)))
		}
		lookup := &MapLookup{
			X:       packageBuilder.expression(function, typed.X),
			Index:   emitConv(function, packageBuilder.expression(function, typed.Index), mapt.Key(), typed),
			CommaOk: true,
		}
		lookup.setType(tuple)
		return function.emit(lookup, typed)

	case *ast.TypeAssertExpr:
		return emitTypeTest(function, packageBuilder.expression(function, typed.X), tuple.At(0).Type(), typed)

	case *ast.UnaryExpr: // must be receive <-
		return emitReceive(function, packageBuilder.expression(function, typed.X), true, tuple, typed)
	}
	// Invariant: the multi-result expressions are calls, comma-ok map indexes, type assertions and receives, each
	// handled above, with parentheses unwrapped.
	panic(fmt.Sprintf("exprN(%T) in %s", expression, function))
}

// builtin emits to fn IR instructions to implement a call to the
// built-in function obj with the specified arguments
// and return type.  It returns the value defined by the result.
//
// The result is nil if no special handling was required; in this case
// the caller should treat this like an ordinary library function
// call.
//
// Mutates: function moves on to an unreachable block after a panic
func (packageBuilder *builder) builtin(
	function *Function,
	object *types.Builtin,
	arguments []ast.Expr,
	callType types.Type,
	source ast.Node,
) Value {
	resultType := function.instantiatedType(callType)
	switch object.Name() {
	case "make":
		switch ct := core_type.Of(resultType).(type) {
		case *types.Slice:
			length := packageBuilder.expression(function, arguments[1])
			capacity := length
			if len(arguments) == 3 {
				capacity = packageBuilder.expression(function, arguments[2])
			}
			if constantCapacity, ok := capacity.(*Constant); ok {
				// treat make([]T, n, m) as new([m]T)[:n]
				cap := constantCapacity.Int64()
				at := types.NewArray(ct.Elem(), cap)
				slice := &Slice{
					X:    emitNew(function, at, source, "makeslice"),
					High: length,
				}
				slice.setType(resultType)
				return function.emit(slice, source)
			}
			slice := &MakeSlice{
				Length:   length,
				Capacity: capacity,
			}
			slice.setType(resultType)
			return function.emit(slice, source)

		case *types.Map:
			var reserve Value
			if len(arguments) == 2 {
				reserve = packageBuilder.expression(function, arguments[1])
			}
			makeMap := &MakeMap{Reserve: reserve}
			makeMap.setType(resultType)
			return function.emit(makeMap, source)

		case *types.Chan:
			var sz Value = intConstant(0, source)
			if len(arguments) == 2 {
				sz = packageBuilder.expression(function, arguments[1])
			}
			channel := &MakeChannel{Size: sz}
			channel.setType(resultType)
			return function.emit(channel, source)

		default:
			lint.ExhaustiveTypeSwitch(resultType.Underlying())
		}

	case "new":
		allocation := emitNew(function, dereference(resultType), source, "new")
		if !function.info.Types[arguments[0]].IsType() {
			// new(expr), requires go1.26
			value := packageBuilder.expression(function, arguments[0])
			emitStore(function, allocation, value, source)
		}
		return allocation

	case "len", "cap":
		// Special case: len or cap of an array or *array is based on the type, not the value which may be nil. We must
		// still evaluate the value, though. (If it was side-effect free, the whole call would have been
		// constant-folded.)
		//
		// For example, for len(gen()), we need to evaluate gen() for its side-effects, but don't need the returned
		// value to determine the length of the array, which is constant.
		//
		// Technically this shouldn't apply to type parameters because their length/capacity is never constant. We still
		// choose to treat them as constant so that users of the IR get the practically constant length for free.
		resolvedType := core_type.Of(dereference(function.typeOf(arguments[0])))
		if at, ok := resolvedType.(*types.Array); ok {
			packageBuilder.expression(function, arguments[0]) // for effects only
			return intConstant(at.Len(), arguments[0])
		}
		// Otherwise treat as normal.

	case "panic":
		function.emit(&Panic{
			X: emitConv(function, packageBuilder.expression(function, arguments[0]), tEface, source),
		}, source)
		function.currentBlock = function.newBasicBlock("unreachable")
		return vTrue // any non-nil Value will do
	}
	return nil // treat all others as a regular function call
}

// address lowers a single-result addressable expression `expression` to IR form,
// emitting code to fn and returning the location (an lvalue) defined
// by the expression.
//
// If escaping is true, address marks the base variable of the
// addressable expression `expression` as being a potentially escaping pointer
// value.  For example, in this code:
//
//	a := A{
//	  b: [1]B{B{c: 1}}
//	}
//	return &a.b[0].c
//
// the application of & causes a.b[0].c to have its address taken,
// which means that ultimately the local variable a must be
// heap-allocated.  This is a simple but very conservative escape
// analysis.
//
// Operations forming potentially escaping pointers include:
// - &x, including when implicit in method call or composite literals.
// - a[:] iff a is an array (not *array)
// - references to variables in lexically enclosing functions.
func (packageBuilder *builder) address(function *Function, expression ast.Expr, escaping bool) lvalue {
	switch typed := expression.(type) {
	case *ast.Ident:
		if isBlankIdent(typed) {
			return blank{}
		}
		object, isVariable := function.objectOf(typed).(*types.Var)
		if !isVariable {
			// Invariant: an addressable identifier, or one assigned to, names a variable, and the blank
			// identifier returned above.
			panic(fmt.Sprintf("addr: identifier %s is %T, not a variable", typed.Name, function.objectOf(typed)))
		}
		var value Value
		if member := function.Program.packageLevelMember(object); member != nil {
			global, isGlobal := member.(*Global)
			if !isGlobal {
				// Invariant: CreatePackage records every package-level variable as a Global.
				panic(fmt.Sprintf("addr: package-level variable %s is %T, not a Global", object.Name(), member))
			}
			value = global // var (address)
		} else {
			value = function.lookup(object, escaping)
		}
		return &address{pointer: value, expression: typed}

	case *ast.CompositeLit:
		resolvedType := dereference(function.typeOf(typed))
		var allocation *Allocation
		if escaping {
			allocation = emitNew(function, resolvedType, typed, "complit")
		} else {
			allocation = emitLocal(function, resolvedType, typed, "complit")
		}
		var stores storebuf
		packageBuilder.compLit(function, allocation, typed, true, &stores)
		stores.emit(function)
		return &address{pointer: allocation, expression: typed}

	case *ast.ParenExpr:
		return packageBuilder.address(function, typed.X, escaping)

	case *ast.SelectorExpr:
		selection := function.selection(typed)
		if selection == nil {
			// qualified identifier
			return packageBuilder.address(function, typed.Sel, escaping)
		}
		if selection.kind != types.FieldVal {
			// Invariant: an addressable selector selects a field, since method values and method expressions are
			// not addressable and cannot be assigned to.
			panic(selection)
		}
		wantAddress := true
		value := packageBuilder.receiver(function, typed.X, wantAddress, escaping, selection, typed)
		index := selection.index[len(selection.index)-1]
		fld := fieldOf(dereference(value.Type()), index) // v is an addr.

		// Due to the two phases of resolving AssignStmt, a panic from x.f = p()
		// when x is nil is required to come after the side-effects of
		// evaluating x and p().
		emit := func(function *Function) Value {
			return emitFieldSelection(function, value, index, true, typed.Sel)
		}
		return &lazyAddress{computeAddress: emit, locationType: fld.Type(), expression: typed.Sel}

	case *ast.IndexExpr:
		xt := function.typeOf(typed.X)
		elem, mode := indexType(xt)
		var x Value
		var et types.Type
		switch mode {
		case ixArrVariable: // array, array|slice, array|*array, or array|*array|slice.
			x = packageBuilder.address(function, typed.X, escaping).address(function)
			et = types.NewPointer(elem)
		case ixVariable: // *array, slice, *array|slice
			x = packageBuilder.expression(function, typed.X)
			et = types.NewPointer(elem)
		case ixMap:
			mt, isMap := core_type.Of(xt).(*types.Map)
			if !isMap {
				// Invariant: indexType gives ixMap only when every type in the operand's type set is a map with the
				// same key and element types, so its core type is that map.
				panic("addr: map index of " + xt.String() + ", which has no map core type")
			}
			return &element{
				mapValue: packageBuilder.expression(function, typed.X),
				key: emitConv(
					function,
					packageBuilder.expression(function, typed.Index),
					mt.Key(),
					typed.Index,
				),
				elementType: mt.Elem(),
			}
		default:
			// Invariant: address is reached only for an addressable index expression or a map element assigned to, and
			// indexing a string or an array value is neither.
			panic("unexpected container type in IndexExpr: " + xt.String())
		}
		index := packageBuilder.expression(function, typed.Index)
		if isUntyped(index.Type()) {
			index = emitConv(function, index, tInt, typed.Index)
		}

		// Due to the two phases of resolving AssignStmt, a panic from x[i] = p()
		// when x is nil or i is out-of-bounds is required to come after the
		// side-effects of evaluating x, i and p().
		emit := func(function *Function) Value {
			address := &IndexAddress{
				X:     x,
				Index: index,
			}
			address.setType(et)
			return function.emit(address, typed)
		}
		return &lazyAddress{computeAddress: emit, locationType: dereference(et), expression: typed}

	case *ast.StarExpr:
		return &address{pointer: packageBuilder.expression(function, typed.X), expression: typed}
	}

	// Invariant: go/types makes addressable, or lets a statement assign to, only identifiers, field selectors, index
	// expressions and pointer indirections, and & applies also to composite literals; each is handled above.
	panic(fmt.Sprintf("unexpected address expression: %T", expression))
}

type store struct {
	lhs    lvalue
	rhs    Value
	source ast.Node

	// debugReference, when set, is the store: a debug reference to emit in its place, and no other field is
	// set.
	debugReference *debugReference
}

// assign emits to fn code to initialize the lvalue loc with the value
// of expression `expression`.  If isZero is true, assign assumes that loc holds
// the zero value for its type.
//
// This is equivalent to loc.store(fn, b.expression(fn, expression)), but may generate
// better code in some cases, e.g., for composite literals in an
// addressable location.
//
// If sb is not nil, assign generates code to evaluate expression `expression`, but
// not to update loc.  Instead, the necessary stores are appended to the
// storebuf sb so that they can be executed later.  This allows correct
// in-place update of existing variables when the RHS is a composite
// literal that may reference parts of the LHS.
func (packageBuilder *builder) assign(
	function *Function,
	loc lvalue,
	expression ast.Expr,
	isZero bool,
	deferredStores *storebuf,
	source ast.Node,
) {
	// Can we initialize it in place?
	if lit, ok := ast.Unparen(expression).(*ast.CompositeLit); ok {
		// A CompositeLit never evaluates to a pointer,
		// so if the type of the location is a pointer,
		// an &-operation is implied.
		if _, ok := loc.(blank); !ok { // avoid calling blank.storedType()
			if isPointerCore(loc.storedType()) {
				// Example input that hits this code:
				//
				// 	type S1 struct{ X int }
				// 	x := []*S1{
				// 		{1}, // <-- & is implied
				// 	}
				// 	_ = x
				pointer := packageBuilder.address(function, lit, true).address(function)
				// copy address
				if deferredStores != nil {
					deferredStores.store(loc, pointer, source)
				} else {
					loc.store(function, pointer, source)
				}
				return
			}
		}

		if _, ok := loc.(*address); ok {
			if types.IsInterface(loc.storedType()) && !typeparams.IsTypeParam(loc.storedType()) {
				// e.g. var x interface{} = T{...}
				// Can't in-place initialize an interface value.
				// Fall back to copying.
			} else {
				// x = T{...} or x := T{...}
				address := loc.address(function)
				if deferredStores != nil {
					packageBuilder.compLit(function, address, lit, isZero, deferredStores)
				} else {
					var stores storebuf
					packageBuilder.compLit(function, address, lit, isZero, &stores)
					stores.emit(function)
				}

				// Subtle: emit debug ref for aggregate types only;
				// slice and map are handled by store ops in compLit.
				switch core_type.Of(loc.storedType()).(type) {
				case *types.Struct, *types.Array:
					if deferredStores != nil {
						// Make sure we don't emit DebugRefs before the store has actually occurred
						if reference := makeDebugReference(function, lit, address, true); reference != nil {
							deferredStores.storeDebugReference(reference)
						}
					} else {
						emitDebugReference(function, lit, address, true)
					}
				}

				return
			}
		}
	}

	// simple case: just copy
	rhs := packageBuilder.expression(function, expression)
	if deferredStores != nil {
		deferredStores.store(loc, rhs, source)
	} else {
		loc.store(function, rhs, source)
	}
}

// expression lowers a single-result expression `expression` to IR form, emitting code
// to fn and returning the Value defined by the expression.
func (packageBuilder *builder) expression(function *Function, expression ast.Expr) Value {
	unparenthesized := ast.Unparen(expression)

	tv := function.info.Types[unparenthesized]

	// Is expression a constant?
	if tv.Value != nil {
		return NewConstant(tv.Value, function.instantiatedType(tv.Type), unparenthesized)
	}

	var value Value
	if tv.Addressable() {
		// Prefer pointer arithmetic ({Index,Field}Address) followed
		// by Load over subelement extraction (e.g. Index, Field),
		// to avoid large copies.
		value = packageBuilder.address(function, unparenthesized, false).load(function, unparenthesized)
	} else {
		value = packageBuilder.expression0(function, unparenthesized, tv)
	}
	if function.debugInformation() {
		emitDebugReference(function, unparenthesized, value, false)
	}
	return value
}

// expression0 lowers a single-result expression `expression` that is neither a constant nor addressable to IR form,
// emitting code to fn and returning the Value defined by the expression.
//
// Mutates: function gains a function literal as an anonymous function and resumes its unique values after it's built
func (packageBuilder *builder) expression0(function *Function, expression ast.Expr, tv types.TypeAndValue) Value {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		// Invariant: a basic literal is always constant, so expression returned it as a constant before calling
		// expression0.
		panic("non-constant BasicLit") // unreachable

	case *ast.FuncLit:
		/* function literal */
		signature, isSignature := function.typeOf(typed.Type).(*types.Signature)
		if !isSignature {
			// Invariant: go/types gives a function literal's type expression a signature type.
			panic(fmt.Sprintf(
				"function literal in %s has type %s, not a signature",
				function,
				function.typeOf(typed.Type),
			))
		}
		fn2 := &Function{
			name:           fmt.Sprintf("%s$%d", function.Name(), 1+len(function.AnonymousFunctions)),
			Signature:      signature,
			position:       typed.Type.Func,
			parent:         function,
			anonymousIndex: int32(len(function.AnonymousFunctions)),
			Package:        function.Package,
			Program:        function.Program,
			syntax:         typed,
			info:           function.info,
			goversion:      function.goversion,
			build:          (*builder).buildFromSyntax,
			topLevelOrigin: nil,                 // use anonIdx to lookup an anon instance's origin.
			typeparams:     function.typeparams, // share the parent's type parameters.
			typeargs:       function.typeargs,   // share the parent's type arguments.
		}
		fn2.uniq = function.uniq // start from parent's unique values
		function.AnonymousFunctions = append(function.AnonymousFunctions, fn2)
		// Build anon immediately, as it may cause fn's locals to escape.
		// (It is not marked 'built' until the end of the enclosing FuncDecl.)
		fn2.build(packageBuilder, fn2)
		function.uniq = fn2.uniq // resume after anon's unique values
		if fn2.FreeVariables == nil {
			return fn2
		}
		closure := &MakeClosure{Function: fn2}
		closure.setType(function.instantiatedType(tv.Type))
		for _, fv := range fn2.FreeVariables {
			closure.Bindings = append(closure.Bindings, fv.outer)
			fv.outer = nil
		}
		return function.emit(closure, typed)

	case *ast.TypeAssertExpr: // single-result form only
		return emitTypeAssert(
			function,
			packageBuilder.expression(function, typed.X),
			function.instantiatedType(tv.Type),
			typed,
		)

	case *ast.CallExpr:
		if function.info.Types[typed.Fun].IsType() {
			// Explicit type conversion, e.g. string(x) or big.Int(x)
			x := packageBuilder.expression(function, typed.Args[0])
			y := emitConv(function, x, function.instantiatedType(tv.Type), typed)
			return y
		}
		// Call to "intrinsic" built-ins, e.g. new, make, panic.
		if id, ok := ast.Unparen(typed.Fun).(*ast.Ident); ok {
			if object, ok := function.info.Uses[id].(*types.Builtin); ok {
				if value := packageBuilder.builtin(
					function,
					object,
					typed.Args,
					function.instantiatedType(tv.Type),
					typed,
				); value != nil {
					return value
				}
			}
		}
		// Regular function call.
		var call Call
		packageBuilder.setCall(function, typed, &call.Call)
		call.setType(function.instantiatedType(tv.Type))
		return emitCall(function, &call, typed)

	case *ast.UnaryExpr:
		switch typed.Op {
		case token.AND: // &X --- potentially escaping.
			address := packageBuilder.address(function, typed.X, true)
			if _, ok := ast.Unparen(typed.X).(*ast.StarExpr); ok {
				// &*p must panic if p is nil (https://golang.org/s/go12nil).
				// For simplicity, we'll just (suboptimally) rely
				// on the side effects of a load.
				// No dedicated nil check is emitted.
				address.load(function, typed)
			}
			return address.address(function)
		case token.ADD:
			return packageBuilder.expression(function, typed.X)
		case token.NOT, token.SUB, token.XOR: // ! <- - ^
			operation := &UnaryOperation{
				Operator: typed.Op,
				X:        packageBuilder.expression(function, typed.X),
			}
			operation.setType(function.instantiatedType(tv.Type))
			return function.emit(operation, typed)
		case token.ARROW:
			return emitReceive(
				function,
				packageBuilder.expression(function, typed.X),
				false,
				function.instantiatedType(tv.Type),
				typed,
			)
		default:
			// Invariant: go/types accepts only the unary operators &, +, !, -, ^ and <-, each handled above, as
			// *p is a StarExpr.
			panic(typed.Op)
		}

	case *ast.BinaryExpr:
		switch typed.Op {
		case token.LAND, token.LOR:
			return packageBuilder.logicalBinop(function, typed)
		case token.SHL, token.SHR:
			fallthrough
		case token.ADD, token.SUB, token.MUL, token.QUO, token.REM, token.AND, token.OR, token.XOR, token.AND_NOT:
			return emitArith(
				function,
				typed.Op,
				packageBuilder.expression(function, typed.X),
				packageBuilder.expression(function, typed.Y),
				function.instantiatedType(tv.Type),
				typed,
			)

		case token.EQL, token.NEQ, token.GTR, token.LSS, token.LEQ, token.GEQ:
			cmp := emitCompare(
				function,
				typed.Op,
				packageBuilder.expression(function, typed.X),
				packageBuilder.expression(function, typed.Y),
				typed,
			)
			// The type of x==y may be UntypedBool.
			return emitConv(function, cmp, types.Default(function.instantiatedType(tv.Type)), typed)
		default:
			// Invariant: the cases above cover every binary operator Go has.
			panic("illegal op in BinaryExpr: " + typed.Op.String())
		}

	case *ast.SliceExpr:
		var low Value
		var high Value
		var maximum Value
		var x Value
		xtyp := function.typeOf(typed.X)
		switch core_type.Of(xtyp).(type) {
		case *types.Array:
			// Potentially escaping.
			x = packageBuilder.address(function, typed.X, true).address(function)
		case *types.Basic, *types.Slice, *types.Pointer: // *array
			x = packageBuilder.expression(function, typed.X)
		default:
			// core type exception?
			if isBytestring(xtyp) {
				x = packageBuilder.expression(function, typed.X) // bytestring is handled as string and []byte.
			} else {
				// Invariant: go/types slices only strings, arrays, pointers to arrays, slices and type sets of
				// byte strings, each handled above.
				panic("unexpected sequence type in SliceExpr")
			}
		}
		if typed.Low != nil {
			low = packageBuilder.expression(function, typed.Low)
		}
		if typed.High != nil {
			high = packageBuilder.expression(function, typed.High)
		}
		if typed.Slice3 {
			maximum = packageBuilder.expression(function, typed.Max)
		}
		slice := &Slice{
			X:       x,
			Low:     low,
			High:    high,
			Maximum: maximum,
		}
		slice.setType(function.instantiatedType(tv.Type))
		return function.emit(slice, typed)

	case *ast.Ident:
		object := function.info.Uses[typed]
		// Universal built-in or nil?
		switch typedObject := object.(type) {
		case *types.Builtin:
			signature, isSignature := function.instanceType(typed).(*types.Signature)
			if !isSignature {
				// Invariant: a built-in reaches expression only as the callee of a call the builtin method left as a
				// regular call, and go/types records that call's signature as its type.
				panic(fmt.Sprintf(
					"built-in %s has type %s, not a signature",
					typedObject.Name(),
					function.instanceType(typed),
				))
			}
			return &Builtin{name: typedObject.Name(), sig: signature}
		case *types.Nil:
			return zeroConstant(function.instanceType(typed), typed)
		}

		// Package-level func or var?
		// (obj must belong to same package or a direct import.)
		if member := function.Program.packageLevelMember(object); member != nil {
			if global, ok := member.(*Global); ok {
				return emitLoad(function, global, typed) // var (address)
			}
			callee, isFunction := member.(*Function) // (func)
			if !isFunction {
				// Invariant: a package's values are its constants, Globals and Functions; expression returns a constant
				// before calling expression0, and a Global returns above as a load.
				panic(fmt.Sprintf("package-level member %s is %T, not a function", typed.Name, member))
			}
			if callee.typeparams.Len() > 0 {
				targs := function.subtargs(typed)
				callee = callee.instance(nil, targs, packageBuilder)
			}
			return callee
		}
		// Local var.
		variable, isVariable := object.(*types.Var)
		if !isVariable {
			// Invariant: an identifier that is not a built-in, nil, a constant or a package-level member names a
			// local variable, as functions are declared only at package level.
			panic(fmt.Sprintf("identifier %s is %T, not a variable", typed.Name, object))
		}
		return emitLoad(function, function.lookup(variable, false), typed) // var (address)

	case *ast.SelectorExpr:
		selection := function.selection(typed)
		if selection == nil {
			// builtin unsafe.{Add,Slice}
			if object, ok := function.info.Uses[typed.Sel].(*types.Builtin); ok {
				signature, isSignature := function.instantiatedType(tv.Type).(*types.Signature)
				if !isSignature {
					// Invariant: unsafe.Add and unsafe.Slice reach expression only as the callee of a call, and
					// go/types records that call's signature as their type.
					panic(fmt.Sprintf("unsafe.%s has type %s, not a signature", object.Name(), tv.Type))
				}
				return &Builtin{name: "Unsafe" + object.Name(), sig: signature}
			}
			// qualified identifier
			return packageBuilder.expression(function, typed.Sel)
		}
		switch selection.kind {
		case types.MethodExpr:
			// (*T).f or T.f, the method f from the method-set of type T.
			// The result is a "thunk".
			targs := function.subtargs(typed.Sel)
			thunk := createThunk(function.Program, selection, targs)
			packageBuilder.enqueue(thunk)
			return thunk

		case types.MethodVal:
			// e.f where e is an expression and f is a method.
			// The result is a "bound".
			method, isFunction := selection.object.(*types.Func)
			if !isFunction {
				// Invariant: go/types makes the object of every method selection a *types.Func.
				panic(fmt.Sprintf("method value %s selects %T, not a method", typed.Sel.Name, selection.object))
			}
			rt := function.instantiatedType(methodReceiverType(method))
			wantAddress := isPointer(rt)
			escaping := true
			value := packageBuilder.receiver(function, typed.X, wantAddress, escaping, selection, typed)

			if types.IsInterface(rt) {
				// If v may be an interface type I (after instantiating),
				// we must emit a check that v is non-nil.
				if receiverTypeParameter, ok := types.Unalias(selection.receiverType).(*types.TypeParam); ok {
					// Emit a nil check if any possible instantiation of the
					// type parameter is an interface type.
					if !typeSetIsEmpty(receiverTypeParameter) {
						// receiverTypeParameter has a concrete term its typeset.
						// So it cannot be instantiated as an interface.
						//
						// Example:
						// func _[T interface{~int; Foo()}] () {
						//    var v T
						//    _ = v.Foo // <-- MethodVal
						// }
					} else {
						// rt may be instantiated as an interface.
						// Emit nil check: typeassert (any(v)).(any).
						emitTypeAssert(function, emitConv(function, value, tEface, nil), tEface, nil)
					}
				} else {
					// non-type param interface
					// Emit nil check: typeassert v.(I).
					emitTypeAssert(function, value, rt, typed.Sel)
				}
			}

			if rtargs := function.subrtargs(method); len(rtargs) > 0 {
				method = function.Program.canon.instantiateMethod(method, rtargs, function.Program.ctxt)
			}

			targs := function.subtargs(typed.Sel)
			bound := createBound(function.Program, method, targs)
			packageBuilder.enqueue(bound)

			// The assignment may widen a type parameter to its
			// interface bound (case #3 of go.dev/issue.78110).
			value = emitConv(function, value, bound.FreeVariables[0].Type(), nil)

			closure := &MakeClosure{
				Function: bound,
				Bindings: []Value{value},
			}
			closure.source = typed.Sel
			closure.setType(bound.Signature)
			return function.emit(closure, typed.Sel)

		case types.FieldVal:
			indices := selection.index
			last := len(indices) - 1
			value := packageBuilder.expression(function, typed.X)
			value = emitImplicitSelections(function, value, indices[:last], typed)
			value = emitFieldSelection(function, value, indices[last], false, typed.Sel)
			return value
		}

		// Invariant: the cases above cover every selection kind go/types has.
		panic("unexpected expression-relative selector")

	case *ast.IndexListExpr:
		// f[X, Y] must be a generic function
		if !instance(function.info, typed.X) {
			// Invariant: go/types accepts an index list expression only as an instantiation, and in a value
			// position only of a generic function.
			panic("unexpected expression-could not match index list to instantiation")
		}
		return packageBuilder.expression(
			function,
			typed.X,
		) // Handle instantiation within the *Ident or *SelectorExpr cases.

	case *ast.IndexExpr:
		if instance(function.info, typed.X) {
			// Handle instantiation within the *Ident or *SelectorExpr cases.
			return packageBuilder.expression(function, typed.X)
		}
		// not a generic instantiation.
		xt := function.typeOf(typed.X)
		switch et, mode := indexType(xt); mode {
		case ixVariable:
			// Addressable slice/array; use IndexAddress and Load.
			return packageBuilder.address(function, typed, false).load(function, typed)

		case ixArrVariable, ixValue:
			// An array in a register, a string or a combined type that contains
			// either an [_]array (ixArrVar) or string (ixValue).

			// Note: for ixArrVar and CoreType(xt)==nil can be IndexAddress and Load.
			index := packageBuilder.expression(function, typed.Index)
			if isUntyped(index.Type()) {
				index = emitConv(function, index, tInt, typed.Index)
			}
			elementValue := &Index{
				X:     packageBuilder.expression(function, typed.X),
				Index: index,
			}
			elementValue.setType(et)
			return function.emit(elementValue, typed)

		case ixMap:
			ct, isMap := core_type.Of(xt).(*types.Map)
			if !isMap {
				// Invariant: indexType gives ixMap only when every type in the operand's type set is a map with the
				// same key and element types, so its core type is that map.
				panic("map index of " + xt.String() + ", which has no map core type")
			}
			lookup := &MapLookup{
				X:     packageBuilder.expression(function, typed.X),
				Index: emitConv(function, packageBuilder.expression(function, typed.Index), ct.Key(), typed.Index),
			}
			lookup.setType(ct.Elem())
			return function.emit(lookup, typed)
		default:
			// Invariant: go/types indexes only arrays, pointers to arrays, slices, strings, maps and type sets of
			// those with one element type, and indexType gives each of them one of the modes above.
			panic("unexpected container type in IndexExpr: " + xt.String())
		}

	case *ast.CompositeLit, *ast.StarExpr:
		// Addressable types (lvalues)
		return packageBuilder.address(function, typed, false).load(function, typed)
	}

	// Invariant: expression unwraps parentheses and returns constants, and every other expression that has a value is
	// handled above.
	panic(fmt.Sprintf("unexpected expr: %T", expression))
}

// receiver emits to fn code for expression `expression` in the "receiver"
// position of selection e.f (where f may be a field or a method) and
// returns the effective receiver after applying the implicit field
// selections of selection.
//
// wantAddress requests that the result is an address.  If
// !selection.indirect, this may require that `expression` be built in address() mode; it
// must thus be addressable.
//
// escaping is defined as per builder.address().
func (packageBuilder *builder) receiver(
	function *Function,
	expression ast.Expr,
	wantAddress, escaping bool,
	selection *selection,
	source ast.Node,
) Value {
	var value Value
	if wantAddress && !selection.indirect && !isPointerCore(function.typeOf(expression)) {
		value = packageBuilder.address(function, expression, escaping).address(function)
	} else {
		value = packageBuilder.expression(function, expression)
	}

	last := len(selection.index) - 1
	value = emitImplicitSelections(function, value, selection.index[:last], source)
	if types.IsInterface(value.Type()) {
		// When v is an interface, sel.Kind()==MethodValue and v.f is invoked.
		// So v is not loaded, even if v has a pointer core type.
	} else if !wantAddress && isPointerCore(value.Type()) {
		value = emitLoad(function, value, expression)
	}
	return value
}

// setCallFunction populates the function parts of a CallCommon structure
// (Function, Method, receiver, Arguments[0]) based on the kind of invocation
// occurring in `callExpr`.
//
// Mutates: common takes the callee, and for a method its receiver or method
func (packageBuilder *builder) setCallFunction(function *Function, callExpression *ast.CallExpr, common *CallCommon) {
	// Is this a (possibly generic) method call?
	calleeExpression := ast.Unparen(callExpression.Fun)
	switch typed := calleeExpression.(type) {
	case *ast.IndexExpr:
		calleeExpression = typed.X
	case *ast.IndexListExpr:
		calleeExpression = typed.X
	}
	if selector, ok := calleeExpression.(*ast.SelectorExpr); ok {
		selection := function.selection(selector)
		if selection != nil && selection.kind == types.MethodVal {
			object, isFunction := selection.object.(*types.Func)
			if !isFunction {
				// Invariant: go/types makes the object of every method selection a *types.Func.
				panic(fmt.Sprintf("method call %s selects %T, not a method", selector.Sel.Name, selection.object))
			}
			receiverType := methodReceiverType(object)

			wantAddress := isPointer(receiverType)
			escaping := true
			value := packageBuilder.receiver(function, selector.X, wantAddress, escaping, selection, selector)
			if types.IsInterface(receiverType) {
				// Invoke-mode call.
				common.Value = value // possibly type param
				common.Method = object
			} else {
				// "Call"-mode call.
				targs := function.subtargs(selector.Sel)
				common.Value = function.Program.objectMethod(object, targs, packageBuilder)
				common.Arguments = append(common.Arguments, value)
			}
			return
		}

		// sel.kind==MethodExpr indicates T.f() or (*T).f():
		// a statically dispatched call to the method f in the
		// method-set of T or *T.  T may be an interface.
		//
		// e.Fun would evaluate to a concrete method, interface
		// wrapper function, or promotion wrapper.
		//
		// It is evaluated in the usual way. Inlining expression() here
		// would make the call static and avoid generating wrappers,
		// but it's somewhat tricky as it may consume the first
		// actual parameter if the call is "invoke" mode.
		//
		// Examples:
		//  type T struct{}; func (T) f() {}   // "call" mode
		//  type T interface { f() }           // "invoke" mode
		//
		//  type S struct{ T }
		//
		//  var s S
		//  S.f(s)
		//  (*S).f(&s)
		//
		// An approach to inlining it:
		// - consume the first actual parameter expression
		//   and build it with b.expression().
		// - apply implicit field selections.
		// - use MethodVal logic to populate fields of c.
	}

	// Evaluate the function operand in the usual way.
	common.Value = packageBuilder.expression(function, callExpression.Fun)
}

// emitCallArguments emits to f code for the actual parameters of call `expression` to
// a (possibly built-in) function of effective type sig.
// The argument values are appended to args, which is then returned.
func (packageBuilder *builder) emitCallArguments(
	function *Function,
	sig *types.Signature,
	expression *ast.CallExpr,
	arguments []Value,
) []Value {
	callArguments := arguments

	// f(x, y, z...): pass slice z straight through.
	if expression.Ellipsis != 0 {
		for index, argument := range expression.Args {
			value := emitConv(
				function,
				packageBuilder.expression(function, argument),
				sig.Params().At(index).Type(),
				argument,
			)
			callArguments = append(callArguments, value)
		}
		return callArguments
	}

	offset := len(arguments) // 1 if call has receiver, 0 otherwise

	// Evaluate actual parameter expressions.
	//
	// If this is a chained call of the form f(g()) where g has
	// multiple return values (MRV), they are flattened out into
	// args; a suffix of them may end up in a varargs slice.
	for _, argument := range expression.Args {
		value := packageBuilder.expression(function, argument)
		if ttuple, ok := value.Type().(*types.Tuple); ok { // MRV chain
			for index, length := 0, ttuple.Len(); index < length; index++ {
				callArguments = append(callArguments, emitExtract(function, value, index, argument))
			}
		} else {
			callArguments = append(callArguments, value)
		}
	}

	// Actual->formal assignability conversions for normal parameters.
	np := sig.Params().Len() // number of normal parameters
	if sig.Variadic() {
		np--
	}
	for index := 0; index < np; index++ {
		callArguments[offset+index] = emitConv(
			function,
			callArguments[offset+index],
			sig.Params().At(index).Type(),
			callArguments[offset+index].Source(),
		)
	}

	// Actual->formal assignability conversions for variadic parameter,
	// and construction of slice.
	if sig.Variadic() {
		varargs := callArguments[offset+np:]
		st, isSlice := sig.Params().At(np).Type().(*types.Slice)
		if !isSlice {
			// Invariant: a variadic parameter has a slice type unless the call spreads a string into append with
			// ..., and the ... form returned above.
			panic(fmt.Sprintf("variadic parameter of %s has type %s, not a slice", sig, sig.Params().At(np).Type()))
		}
		vt := st.Elem()
		if len(varargs) == 0 {
			callArguments = append(callArguments, zeroConstant(st, nil))
		} else {
			// Replace a suffix of args with a slice containing it.
			at := types.NewArray(vt, int64(len(varargs)))
			allocation := emitNew(function, at, expression, "varargs")
			for index, argument := range varargs {
				iaddr := &IndexAddress{
					X:     allocation,
					Index: intConstant(int64(index), nil),
				}
				iaddr.setType(types.NewPointer(vt))
				function.emit(iaddr, expression)
				emitStore(function, iaddr, argument, argument.Source())
			}
			slice := &Slice{X: allocation}
			slice.setType(st)
			callArguments[offset+np] = function.emit(slice, callArguments[offset+np].Source())
			callArguments = callArguments[:offset+np+1]
		}
	}
	return callArguments
}

// setCall emits to fn code to evaluate all the parameters of a function
// call `expression`, and populates *`common` with those values.
//
// Mutates: common takes the callee and the call's arguments
func (packageBuilder *builder) setCall(function *Function, expression *ast.CallExpr, common *CallCommon) {
	// First deal with the f(...) part and optional receiver.
	packageBuilder.setCallFunction(function, expression, common)

	// Then append the other actual parameters.
	sig, _ := core_type.Of(function.typeOf(expression.Fun)).(*types.Signature)
	if sig == nil {
		// Invariant: setCall is reached only for calls of functions, conversions having returned in expression0, and
		// go/types records a signature as the core type of every callee, built-ins included.
		panic(fmt.Sprintf("no signature for call of %s", expression.Fun))
	}
	common.Arguments = packageBuilder.emitCallArguments(function, sig, expression, common.Arguments)
}

// assignOperation emits to fn code to perform loc <op>= val.
func (packageBuilder *builder) assignOperation(
	function *Function,
	loc lvalue,
	value Value,
	operator token.Token,
	source ast.Node,
) {
	loc.store(
		function,
		emitArith(function, operator, loc.load(function, source), value, loc.storedType(), source),
		source,
	)
}

// arrayLength returns the length of the array whose composite literal elements are elts.
func (packageBuilder *builder) arrayLength(function *Function, elts []ast.Expr) int64 {
	var maximum int64 = -1
	var index int64 = -1
	for _, elt := range elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			key, isConstant := packageBuilder.expression(function, kv.Key).(*Constant)
			if !isConstant {
				// Invariant: go/types requires every index key of an array or slice literal to be a constant.
				panic(fmt.Sprintf("array literal key %T is not a constant", kv.Key))
			}
			index = key.Int64()
		} else {
			index++
		}
		if index > maximum {
			maximum = index
		}
	}
	return maximum + 1
}

// compLit emits to fn code to initialize a composite literal `lit` at
// address `pointer`.
//
// Nested composite literals are recursively initialized in place
// where possible. If isZero is true, compLit assumes that `pointer`
// holds the zero value for the literal's type.
//
// Because the elements of a composite literal may refer to the
// variables being updated, as in the second line below,
//
//	x := T{a: 1}
//	x = T{a: x.a}
//
// all the reads must occur before all the writes. Thus all stores to
// loc are emitted to the storebuf sb for later execution.
//
// A CompositeLit may have pointer type only in the recursive (nested)
// case when the type name is implicit.  e.g. in []*T{{}}, the inner
// literal has type *T behaves like &T{}.
// In that case, `pointer` must point to a T, not a *T.
func (packageBuilder *builder) compLit(
	function *Function,
	pointer Value,
	lit *ast.CompositeLit,
	isZero bool,
	deferredStores *storebuf,
) {
	literalType := dereference(function.typeOf(lit)) // retain the named/alias/param type, if any
	switch typed := core_type.Of(literalType).(type) {
	case *types.Struct:
		lvalue := &address{pointer: pointer, expression: lit}
		if len(lit.Elts) == 0 {
			if !isZero {
				deferredStores.store(lvalue, zeroConstant(dereference(pointer.Type()), lit), lit)
			}
		} else {
			value := &CompositeValue{
				Values: make([]Value, typed.NumFields()),
			}
			for index := range typed.NumFields() {
				value.Values[index] = zeroConstant(typed.Field(index).Type(), lit)
			}
			value.setType(literalType)

			type chainElement struct {
				children []chainElement
				cv       *CompositeValue
			}

			chain := chainElement{
				cv: value,
			}

			for eltIndex, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					key, isIdent := kv.Key.(*ast.Ident)
					if !isIdent {
						// Invariant: go/types requires every key of a struct literal to be a field name.
						panic(fmt.Sprintf("struct literal key %T is not a field name", kv.Key))
					}
					fname := key.Name
					_, index, _ := types.LookupFieldOrMethod(typed, true, function.declaredPackage().Types, fname)

					var parent *chainElement
					chain := &chain
					chainCoreType := typed

					// Ensure that the entire chain of embedded fields has
					// corresponding CompositeValues
					for _, embeddedFieldIndex := range index[:len(index)-1] {
						if embeddedFieldIndex >= len(chain.children) {
							elements := make([]chainElement, embeddedFieldIndex+1)
							copy(elements, chain.children)
							chain.children = elements
						}

						parent = chain
						chain = &chain.children[embeddedFieldIndex]
						treeType := chainCoreType.Field(embeddedFieldIndex).Type()
						embeddedStruct, isStruct := core_type.Of(treeType).(*types.Struct)
						if !isStruct {
							// Invariant: go/types accepts only a struct's own fields as keys of its literal, never
							// promoted ones, so the path to a key has no embedded step and this loop does not run.
							panic(fmt.Sprintf("embedded field of type %s is not a struct", treeType))
						}
						chainCoreType = embeddedStruct
						if chain.cv == nil {
							ncv := &CompositeValue{
								Values: make([]Value, chainCoreType.NumFields()),
							}
							for fieldIndex := range chainCoreType.NumFields() {
								ncv.Values[fieldIndex] = zeroConstant(chainCoreType.Field(fieldIndex).Type(), kv)
							}
							ncv.setType(treeType)
							chain.cv = ncv
							ce := &compositeElement{
								cv:          parent.cv,
								index:       embeddedFieldIndex,
								elementType: ncv.Type(),
							}
							parent.cv.Bitmap.SetBit(&parent.cv.Bitmap, embeddedFieldIndex, 1)
							parent.cv.ExplicitCount++
							deferredStores.store(ce, chain.cv, kv)
						}
					}

					ce := &compositeElement{
						cv:          chain.cv,
						index:       index[len(index)-1],
						elementType: chainCoreType.Field(index[len(index)-1]).Type(),
						expression:  kv.Value,
					}
					// We use b.assign for its handling of implicit & (which,
					// albeit not needed for structs now, may be needed in the
					// future), but no store buffer because 1) it's not needed 2)
					// implicit conversions have to be emitted before we emit the
					// CompositeValue.
					packageBuilder.assign(function, ce, kv.Value, isZero, nil, kv)
					chain.cv.Bitmap.SetBit(&chain.cv.Bitmap, index[len(index)-1], 1)
					chain.cv.ExplicitCount++
				} else {
					ce := &compositeElement{
						cv:          value,
						index:       eltIndex,
						elementType: typed.Field(eltIndex).Type(),
						expression:  elt,
					}
					packageBuilder.assign(function, ce, elt, isZero, nil, elt)
					value.Bitmap.SetBit(&value.Bitmap, eltIndex, 1)
					value.ExplicitCount++
				}
			}

			var dfs func(element chainElement)
			dfs = func(element chainElement) {
				for _, tt := range element.children {
					dfs(tt)
				}

				// Each element's composite value is emitted after those of its children.
				if element.cv != nil {
					function.emit(element.cv, lit)
				}
			}
			dfs(chain)
			deferredStores.store(lvalue, value, lit)
		}

	case *types.Array, *types.Slice:
		var at *types.Array
		var array Value
		switch typed := typed.(type) {
		case *types.Slice:
			at = types.NewArray(typed.Elem(), packageBuilder.arrayLength(function, lit.Elts))
			array = emitNew(function, at, lit, "slicelit")
		case *types.Array:
			at = typed
			array = pointer
		}

		var final Value
		if len(lit.Elts) == 0 {
			if !isZero {
				zc := zeroConstant(at, lit)
				final = zc
			}
		} else if at.Len() == int64(len(lit.Elts)) {
			// The literal specifies all elements, so we can use a composite value
			value := &CompositeValue{
				Values: make([]Value, at.Len()),
			}
			zc := zeroConstant(at.Elem(), lit)
			for index := range value.Values {
				value.Values[index] = zc
			}
			value.setType(at)

			var index *Constant
			for _, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					key, isConstant := packageBuilder.expression(function, kv.Key).(*Constant)
					if !isConstant {
						// Invariant: go/types requires every index key of an array or slice literal to be a
						// constant.
						panic(fmt.Sprintf("array literal key %T is not a constant", kv.Key))
					}
					index = key
					elt = kv.Value
				} else {
					var idxval int64
					if index != nil {
						idxval = index.Int64() + 1
					}
					index = intConstant(idxval, elt)
				}

				iaddr := &compositeElement{
					cv:          value,
					index:       int(index.Int64()),
					elementType: at.Elem(),
					expression:  elt,
				}

				// We use b.assign for its handling of implicit &, but no
				// store buffer because 1) it's not needed 2) implicit
				// conversions have to be emitted before we emit the
				// CompositeValue.
				packageBuilder.assign(function, iaddr, elt, true, nil, elt)
				value.Bitmap.SetBit(&value.Bitmap, int(index.Int64()), 1)
				value.ExplicitCount++
			}
			final = value
			function.emit(value, lit)
		} else {
			// Not all elements are specified. Populate the array with a series of stores, to guard against literals
			// like []int{1<<62: 1}.
			if !isZero {
				// memclear
				deferredStores.store(&address{pointer: array}, zeroConstant(dereference(array.Type()), lit), lit)
			}

			var index *Constant
			for _, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					key, isConstant := packageBuilder.expression(function, kv.Key).(*Constant)
					if !isConstant {
						// Invariant: go/types requires every index key of an array or slice literal to be a
						// constant.
						panic(fmt.Sprintf("array literal key %T is not a constant", kv.Key))
					}
					index = key
					elt = kv.Value
				} else {
					var idxval int64
					if index != nil {
						idxval = index.Int64() + 1
					}
					index = intConstant(idxval, elt)
				}
				iaddr := &IndexAddress{
					X:     array,
					Index: index,
				}
				iaddr.setType(types.NewPointer(at.Elem()))
				function.emit(iaddr, elt)
				if typed != at { // slice
					// backing array is unaliased => storebuf not needed.
					packageBuilder.assign(function, &address{pointer: iaddr, expression: elt}, elt, true, nil, elt)
				} else {
					location := &address{pointer: iaddr, expression: elt}
					packageBuilder.assign(function, location, elt, true, deferredStores, elt)
				}
			}
		}
		if typed != at { // slice
			if final != nil {
				deferredStores.store(&address{pointer: array}, final, lit)
			}
			slice := &Slice{X: array}
			slice.setType(literalType)
			deferredStores.store(&address{pointer: pointer, expression: lit}, function.emit(slice, lit), lit)
		} else if final != nil {
			deferredStores.store(&address{pointer: array, expression: lit}, final, lit)
		}

	case *types.Map:
		makeMap := &MakeMap{Reserve: intConstant(int64(len(lit.Elts)), lit)}
		makeMap.setType(literalType)
		function.emit(makeMap, lit)
		for _, elt := range lit.Elts {
			expression, isKeyValue := elt.(*ast.KeyValueExpr)
			if !isKeyValue {
				// Invariant: go/types requires every element of a map literal to be a key-value pair.
				panic(fmt.Sprintf("map literal element %T is not a key-value pair", elt))
			}

			// If a key expression in a map literal is  itself a
			// composite literal, the type may be omitted.
			// For example:
			//	map[*struct{}]bool{{}: true}
			// An &-operation may be implied:
			//	map[*struct{}]bool{&struct{}{}: true}
			wantAddress := false
			if _, ok := ast.Unparen(expression.Key).(*ast.CompositeLit); ok {
				wantAddress = isPointerCore(typed.Key())
			}

			var key Value
			if wantAddress {
				// A CompositeLit never evaluates to a pointer,
				// so if the type of the location is a pointer,
				// an &-operation is implied.
				key = packageBuilder.address(function, expression.Key, true).address(function)
			} else {
				key = packageBuilder.expression(function, expression.Key)
			}

			loc := element{
				mapValue:    makeMap,
				key:         emitConv(function, key, typed.Key(), expression),
				elementType: typed.Elem(),
			}

			// We call assign() only because it takes care
			// of any &-operation required in the recursive
			// case, e.g.,
			// map[int]*struct{}{0: {}} implies &struct{}{}.
			// In-place update is of course impossible,
			// and no storebuf is needed.
			packageBuilder.assign(function, &loc, expression.Value, true, nil, expression)
		}
		deferredStores.store(&address{pointer: pointer, expression: lit}, makeMap, lit)

	default:
		// Invariant: go/types accepts composite literals only of struct, array, slice and map core types.
		panic("unexpected CompositeLit type: " + literalType.String())
	}
}
