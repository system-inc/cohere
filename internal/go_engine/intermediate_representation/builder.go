// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines the builder, which builds SSA-form IR for function bodies.
//
// SSA construction has two phases, "create" and "build". First, one
// or more packages are created in any order by a sequence of calls to
// CreatePackage, either from syntax or from mere type information.
// Each created package has a complete set of Members (const, var,
// type, func) that can be accessed through methods like
// Program.FunctionValue.
//
// It is not necessary to call CreatePackage for all dependencies of
// each syntax package, only for its direct imports. (In future
// perhaps even this restriction may be lifted.)
//
// Second, packages created from syntax are built, by one or more
// calls to Package.Build, which may be concurrent; or by a call to
// Program.Build, which builds all packages in parallel. Building
// traverses the type-annotated syntax tree of each function body and
// creates SSA-form IR, a control-flow graph of instructions,
// populating fields such as Function.Body, .Parameters, and others.
//
// Building may create additional methods, including:
// - wrapper methods (e.g. for embedding, or implicit &recv)
// - bound method closures (e.g. for use(recv.f))
// - thunks (e.g. for use(I.f) or use(T.f))
// - instantiation wrappers (e.g. f[int], which delegates to f[T]).
// As these methods are created, they are added to the build queue,
// and then processed in turn, until a fixed point is reached,
// Since these methods might belong to packages that were not
// created (by a call to CreatePackage), their Package field is unset.
//
// Instances of generic functions are always wrapped (f[int] delegates
// to f[T]), as go/ir does without its InstantiateGenerics mode: a
// generic body is built once, in generic form.
//
// Each package has an initializer function named "init" that calls
// the initializer functions of each direct import, computes and
// assigns the initial value of each global variable, and calls each
// source-level function named "init". (These generate SSA functions
// named "init#1", "init#2", etc.)
//
// Function literals
//
// Anonymous functions must be built as soon as they are encountered,
// as it may affect locals of the enclosing function, but they are not
// marked 'built' until the end of the outermost enclosing function.
// (Among other things, this causes them to be logged in top-down order.)
//
// The Function.build fields determines the algorithm for building the
// function body. It is cleared to mark that building is complete.

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"runtime"
	"slices"

	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/go/types/typeutil"

	"golang.org/x/exp/typeparams"
)

var (
	varOk    = newVariable("ok", tBool)
	varIndex = newVariable("index", tInt)

	// Type constants.
	tBool       = types.Typ[types.Bool]
	tByte       = types.Typ[types.Byte]
	tRune       = types.Universe.Lookup("rune").Type() // prints as "rune" (Typ[Rune] is same as Int32)
	tInt        = types.Typ[types.Int]
	tInvalid    = types.Typ[types.Invalid]
	tString     = types.Typ[types.String]
	tUntypedNil = types.Typ[types.UntypedNil]
	tEface      = types.NewInterfaceType(nil, nil).Complete()
	tDeferStack = types.NewPointer(typeutil.NewDeferStack())

	vOne      = intConstant(1, nil)
	vTrue     = NewConstant(constant.MakeBool(true), tBool, nil)
	vNoReturn = NewConstant(constant.MakeString("noreturn"), tString, nil)

	jReady        = intConstant(0, nil)  // range-over-func jump is READY
	jBusy         = intConstant(-1, nil) // range-over-func jump is BUSY
	jDone         = intConstant(-2, nil) // range-over-func jump is DONE
	jDroppedPanic = stringConstant("iterator call did not preserve panic", nil)
	jLateYield    = stringConstant("yield function called after range loop exit", nil)

	vDeferStack = &Builtin{
		name: "ssa:deferstack",
		sig:  types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(anonVariable(tDeferStack)), false),
	}
)

// builder holds state associated with the package currently being built.
// Its methods contain all the logic for AST-to-IR conversion.
//
// All Functions belong to the same Program.
//
// builders are not thread-safe.
type builder struct {
	fns []*Function // Functions that have finished their CREATE phases.

	finished int // finished is the length of the prefix of fns containing built functions.

	// The task of building shared functions within the builder.
	// Shared functions are ones the builder may either create or lookup.
	// These may be built by other builders in parallel.
	// The task is done when the builder has finished iterating, and it
	// waits for all shared functions to finish building.
	// nil implies there are no hared functions to wait on.
	buildshared *task
}

// shared is done when the builder has built all of the
// enqueued functions to a fixed-point.
func (packageBuilder *builder) shared() *task {
	if packageBuilder.buildshared == nil { // lazily-initialize
		packageBuilder.buildshared = &task{done: make(chan unit)}
	}
	return packageBuilder.buildshared
}

// enqueue fn to be built by the builder.
func (packageBuilder *builder) enqueue(function *Function) {
	packageBuilder.fns = append(packageBuilder.fns, function)
}

// waitForSharedFunction indicates that the builder should wait until
// the potentially shared function fn has finished building.
//
// This should include any functions that may be built by other
// builders.
func (packageBuilder *builder) waitForSharedFunction(function *Function) {
	if function.buildshared != nil { // maybe need to wait?
		task := packageBuilder.shared()
		task.addEdge(function.buildshared)
	}
}

// cond emits to fn code to evaluate boolean condition `expr` and jump
// to t or f depending on its value, performing various simplifications.
//
// Postcondition: fn.currentBlock is nil.
func (packageBuilder *builder) cond(function *Function, expr ast.Expr, trueBlock, falseBlock *BasicBlock) *If {
	switch typed := expr.(type) {
	case *ast.ParenExpr:
		return packageBuilder.cond(function, typed.X, trueBlock, falseBlock)

	case *ast.BinaryExpr:
		switch typed.Op {
		case token.LAND:
			ltrue := function.newBasicBlock("cond.true")
			packageBuilder.cond(function, typed.X, ltrue, falseBlock)
			function.currentBlock = ltrue
			return packageBuilder.cond(function, typed.Y, trueBlock, falseBlock)

		case token.LOR:
			lfalse := function.newBasicBlock("cond.false")
			packageBuilder.cond(function, typed.X, trueBlock, lfalse)
			function.currentBlock = lfalse
			return packageBuilder.cond(function, typed.Y, trueBlock, falseBlock)
		}

	case *ast.UnaryExpr:
		if typed.Op == token.NOT {
			return packageBuilder.cond(function, typed.X, falseBlock, trueBlock)
		}
	}

	// A traditional compiler would simplify "if false" (etc) here
	// but we do not, for better fidelity to the source code.
	//
	// The value of a constant condition may be platform-specific,
	// and may cause blocks that are reachable in some configuration
	// to be hidden from subsequent analyses such as bug-finding tools.
	return emitIf(function, packageBuilder.expr(function, expr), trueBlock, falseBlock, expr)
}

// logicalBinop emits code to fn to evaluate `expr`, a &&- or
// ||-expression whose reified boolean value is wanted.
// The value is returned.
func (packageBuilder *builder) logicalBinop(function *Function, expr *ast.BinaryExpr) Value {
	rhs := function.newBasicBlock("binop.rhs")
	done := function.newBasicBlock("binop.done")

	// T(e) = T(e.X) = T(e.Y) after untyped constants have been
	// eliminated.
	// TODO(adonovan): not true; MyBool==MyBool yields UntypedBool.
	resolvedType := function.typeOf(expr)

	var short Value // value of the short-circuit path
	switch expr.Op {
	case token.LAND:
		packageBuilder.cond(function, expr.X, rhs, done)
		short = NewConstant(constant.MakeBool(false), resolvedType, expr)

	case token.LOR:
		packageBuilder.cond(function, expr.X, done, rhs)
		short = NewConstant(constant.MakeBool(true), resolvedType, expr)
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
		return packageBuilder.expr(function, expr.Y)
	}

	// All edges from e.X to done carry the short-circuit value.
	var edges []Value
	for range done.Predecessors {
		edges = append(edges, short)
	}

	// The edge from e.Y to done carries the value of e.Y.
	function.currentBlock = rhs
	edges = append(edges, packageBuilder.expr(function, expr.Y))
	emitJump(function, done, expr)
	function.currentBlock = done

	phi := &Phi{Edges: edges}
	phi.typ = resolvedType
	phi.comment = expr.Op.String()
	return done.emit(phi, expr)
}

// exprN lowers a multi-result expression `expr` to IR form, emitting code
// to fn and returning a single Value whose type is a *types.Tuple.
// The caller must access the components via Extract.
//
// Multi-result expressions include CallExprs in a multi-value
// assignment or return statement, and "value,ok" uses of
// TypeAssertExpr, IndexExpr (when X is a map), and Receive.
func (packageBuilder *builder) exprN(function *Function, expr ast.Expr) Value {
	typ := function.typeOf(expr).(*types.Tuple)
	switch typed := expr.(type) {
	case *ast.ParenExpr:
		return packageBuilder.exprN(function, typed.X)

	case *ast.CallExpr:
		// Currently, no built-in function nor type conversion
		// has multiple results, so we can avoid some of the
		// cases for single-valued CallExpr.
		var call Call
		packageBuilder.setCall(function, typed, &call.Call)
		call.typ = typ
		return emitCall(function, &call, typed)

	case *ast.IndexExpr:
		mapt := typeutil.CoreType(function.typeOf(typed.X)).(*types.Map) // ,ok must be a map.
		lookup := &MapLookup{
			X:       packageBuilder.expr(function, typed.X),
			Index:   emitConv(function, packageBuilder.expr(function, typed.Index), mapt.Key(), typed),
			CommaOk: true,
		}
		lookup.setType(typ)
		return function.emit(lookup, typed)

	case *ast.TypeAssertExpr:
		return emitTypeTest(function, packageBuilder.expr(function, typed.X), typ.At(0).Type(), typed)

	case *ast.UnaryExpr: // must be receive <-
		return emitReceive(function, packageBuilder.expr(function, typed.X), true, typ, typed)
	}
	panic(fmt.Sprintf("exprN(%T) in %s", expr, function))
}

// builtin emits to fn IR instructions to implement a call to the
// built-in function obj with the specified arguments
// and return type.  It returns the value defined by the result.
//
// The result is nil if no special handling was required; in this case
// the caller should treat this like an ordinary library function
// call.
func (packageBuilder *builder) builtin(
	function *Function,
	object *types.Builtin,
	arguments []ast.Expr,
	typ types.Type,
	source ast.Node,
) Value {
	typ = function.typ(typ)
	switch object.Name() {
	case "make":
		switch ct := typeutil.CoreType(typ).(type) {
		case *types.Slice:
			length := packageBuilder.expr(function, arguments[1])
			capacity := length
			if len(arguments) == 3 {
				capacity = packageBuilder.expr(function, arguments[2])
			}
			if constantCapacity, ok := capacity.(*Constant); ok {
				// treat make([]T, n, m) as new([m]T)[:n]
				cap := constantCapacity.Int64()
				at := types.NewArray(ct.Elem(), cap)
				slice := &Slice{
					X:    emitNew(function, at, source, "makeslice"),
					High: length,
				}
				slice.setType(typ)
				return function.emit(slice, source)
			}
			slice := &MakeSlice{
				Length:   length,
				Capacity: capacity,
			}
			slice.setType(typ)
			return function.emit(slice, source)

		case *types.Map:
			var reserve Value
			if len(arguments) == 2 {
				reserve = packageBuilder.expr(function, arguments[1])
			}
			makeMap := &MakeMap{Reserve: reserve}
			makeMap.setType(typ)
			return function.emit(makeMap, source)

		case *types.Chan:
			var sz Value = intConstant(0, source)
			if len(arguments) == 2 {
				sz = packageBuilder.expr(function, arguments[1])
			}
			channel := &MakeChannel{Size: sz}
			channel.setType(typ)
			return function.emit(channel, source)

		default:
			lint.ExhaustiveTypeSwitch(typ.Underlying())
		}

	case "new":
		alloc := emitNew(function, deref(typ), source, "new")
		if !function.info.Types[arguments[0]].IsType() {
			// new(expr), requires go1.26
			value := packageBuilder.expr(function, arguments[0])
			emitStore(function, alloc, value, source)
		}
		return alloc

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
		resolvedType := typeutil.CoreType(deref(function.typeOf(arguments[0])))
		if at, ok := resolvedType.(*types.Array); ok {
			packageBuilder.expr(function, arguments[0]) // for effects only
			return intConstant(at.Len(), arguments[0])
		}
		// Otherwise treat as normal.

	case "panic":
		function.emit(&Panic{
			X: emitConv(function, packageBuilder.expr(function, arguments[0]), tEface, source),
		}, source)
		function.currentBlock = function.newBasicBlock("unreachable")
		return vTrue // any non-nil Value will do
	}
	return nil // treat all others as a regular function call
}

// addr lowers a single-result addressable expression `expr` to IR form,
// emitting code to fn and returning the location (an lvalue) defined
// by the expression.
//
// If escaping is true, addr marks the base variable of the
// addressable expression `expr` as being a potentially escaping pointer
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
func (packageBuilder *builder) addr(function *Function, expr ast.Expr, escaping bool) lvalue {
	switch typed := expr.(type) {
	case *ast.Ident:
		if isBlankIdent(typed) {
			return blank{}
		}
		object := function.objectOf(typed).(*types.Var)
		var value Value
		if member := function.Program.packageLevelMember(object); member != nil {
			value = member.(*Global) // var (address)
		} else {
			value = function.lookup(object, escaping)
		}
		return &address{addr: value}

	case *ast.CompositeLit:
		resolvedType := deref(function.typeOf(typed))
		var allocation *Allocation
		if escaping {
			allocation = emitNew(function, resolvedType, typed, "complit")
		} else {
			allocation = emitLocal(function, resolvedType, typed, "complit")
		}
		var stores storebuf
		packageBuilder.compLit(function, allocation, typed, true, &stores)
		stores.emit(function)
		return &address{addr: allocation}

	case *ast.ParenExpr:
		return packageBuilder.addr(function, typed.X, escaping)

	case *ast.SelectorExpr:
		sel := function.selection(typed)
		if sel == nil {
			// qualified identifier
			return packageBuilder.addr(function, typed.Sel, escaping)
		}
		if sel.kind != types.FieldVal {
			panic(sel)
		}
		wantAddr := true
		value := packageBuilder.receiver(function, typed.X, wantAddr, escaping, sel, typed)
		index := sel.index[len(sel.index)-1]
		fld := fieldOf(deref(value.Type()), index) // v is an addr.

		// Due to the two phases of resolving AssignStmt, a panic from x.f = p()
		// when x is nil is required to come after the side-effects of
		// evaluating x and p().
		emit := func(function *Function) Value {
			return emitFieldSelection(function, value, index, true, typed.Sel)
		}
		return &lazyAddress{addr: emit, locationType: fld.Type()}

	case *ast.IndexExpr:
		xt := function.typeOf(typed.X)
		elem, mode := indexType(xt)
		var x Value
		var et types.Type
		switch mode {
		case ixArrVariable: // array, array|slice, array|*array, or array|*array|slice.
			x = packageBuilder.addr(function, typed.X, escaping).address(function)
			et = types.NewPointer(elem)
		case ixVariable: // *array, slice, *array|slice
			x = packageBuilder.expr(function, typed.X)
			et = types.NewPointer(elem)
		case ixMap:
			mt := typeutil.CoreType(xt).(*types.Map)
			return &element{
				mapValue:    packageBuilder.expr(function, typed.X),
				key:         emitConv(function, packageBuilder.expr(function, typed.Index), mt.Key(), typed.Index),
				elementType: mt.Elem(),
			}
		default:
			panic("unexpected container type in IndexExpr: " + xt.String())
		}
		index := packageBuilder.expr(function, typed.Index)
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
		return &lazyAddress{addr: emit, locationType: deref(et)}

	case *ast.StarExpr:
		return &address{addr: packageBuilder.expr(function, typed.X)}
	}

	panic(fmt.Sprintf("unexpected address expression: %T", expr))
}

type store struct {
	lhs    lvalue
	rhs    Value
	source ast.Node
}

// assign emits to fn code to initialize the lvalue loc with the value
// of expression `expr`.  If isZero is true, assign assumes that loc holds
// the zero value for its type.
//
// This is equivalent to loc.store(fn, b.expr(fn, expr)), but may generate
// better code in some cases, e.g., for composite literals in an
// addressable location.
//
// If sb is not nil, assign generates code to evaluate expression `expr`, but
// not to update loc.  Instead, the necessary stores are appended to the
// storebuf sb so that they can be executed later.  This allows correct
// in-place update of existing variables when the RHS is a composite
// literal that may reference parts of the LHS.
func (packageBuilder *builder) assign(function *Function, loc lvalue, expr ast.Expr, isZero bool, deferredStores *storebuf, source ast.Node) {
	// Can we initialize it in place?
	if lit, ok := ast.Unparen(expr).(*ast.CompositeLit); ok {
		// A CompositeLit never evaluates to a pointer,
		// so if the type of the location is a pointer,
		// an &-operation is implied.
		if _, ok := loc.(blank); !ok { // avoid calling blank.typ()
			if isPointerCore(loc.typ()) {
				// Example input that hits this code:
				//
				// 	type S1 struct{ X int }
				// 	x := []*S1{
				// 		{1}, // <-- & is implied
				// 	}
				// 	_ = x
				ptr := packageBuilder.addr(function, lit, true).address(function)
				// copy address
				if deferredStores != nil {
					deferredStores.store(loc, ptr, source)
				} else {
					loc.store(function, ptr, source)
				}
				return
			}
		}

		if _, ok := loc.(*address); ok {
			if types.IsInterface(loc.typ()) && !typeparams.IsTypeParam(loc.typ()) {
				// e.g. var x interface{} = T{...}
				// Can't in-place initialize an interface value.
				// Fall back to copying.
			} else {
				// x = T{...} or x := T{...}
				addr := loc.address(function)
				if deferredStores != nil {
					packageBuilder.compLit(function, addr, lit, isZero, deferredStores)
				} else {
					var stores storebuf
					packageBuilder.compLit(function, addr, lit, isZero, &stores)
					stores.emit(function)
				}

				return
			}
		}
	}

	// simple case: just copy
	rhs := packageBuilder.expr(function, expr)
	if deferredStores != nil {
		deferredStores.store(loc, rhs, source)
	} else {
		loc.store(function, rhs, source)
	}
}

// expr lowers a single-result expression `expr` to IR form, emitting code
// to fn and returning the Value defined by the expression.
func (packageBuilder *builder) expr(function *Function, expr ast.Expr) Value {
	expr = ast.Unparen(expr)

	tv := function.info.Types[expr]

	// Is expression a constant?
	if tv.Value != nil {
		return NewConstant(tv.Value, function.typ(tv.Type), expr)
	}

	var value Value
	if tv.Addressable() {
		// Prefer pointer arithmetic ({Index,Field}Address) followed
		// by Load over subelement extraction (e.g. Index, Field),
		// to avoid large copies.
		value = packageBuilder.addr(function, expr, false).load(function, expr)
	} else {
		value = packageBuilder.expr0(function, expr, tv)
	}
	return value
}

func (packageBuilder *builder) expr0(function *Function, expr ast.Expr, tv types.TypeAndValue) Value {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		panic("non-constant BasicLit") // unreachable

	case *ast.FuncLit:
		/* function literal */
		fn2 := &Function{
			name:           fmt.Sprintf("%s$%d", function.Name(), 1+len(function.AnonymousFunctions)),
			Signature:      function.typeOf(typed.Type).(*types.Signature),
			position:       typed.Type.Func,
			parent:         function,
			anonIndex:      int32(len(function.AnonymousFunctions)),
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
		closure.setType(function.typ(tv.Type))
		for _, fv := range fn2.FreeVariables {
			closure.Bindings = append(closure.Bindings, fv.outer)
			fv.outer = nil
		}
		return function.emit(closure, typed)

	case *ast.TypeAssertExpr: // single-result form only
		return emitTypeAssert(function, packageBuilder.expr(function, typed.X), function.typ(tv.Type), typed)

	case *ast.CallExpr:
		if function.info.Types[typed.Fun].IsType() {
			// Explicit type conversion, e.g. string(x) or big.Int(x)
			x := packageBuilder.expr(function, typed.Args[0])
			y := emitConv(function, x, function.typ(tv.Type), typed)
			return y
		}
		// Call to "intrinsic" built-ins, e.g. new, make, panic.
		if id, ok := ast.Unparen(typed.Fun).(*ast.Ident); ok {
			if object, ok := function.info.Uses[id].(*types.Builtin); ok {
				if value := packageBuilder.builtin(function, object, typed.Args, function.typ(tv.Type), typed); value != nil {
					return value
				}
			}
		}
		// Regular function call.
		var call Call
		packageBuilder.setCall(function, typed, &call.Call)
		call.setType(function.typ(tv.Type))
		return emitCall(function, &call, typed)

	case *ast.UnaryExpr:
		switch typed.Op {
		case token.AND: // &X --- potentially escaping.
			addr := packageBuilder.addr(function, typed.X, true)
			if _, ok := ast.Unparen(typed.X).(*ast.StarExpr); ok {
				// &*p must panic if p is nil (https://golang.org/s/go12nil).
				// For simplicity, we'll just (suboptimally) rely
				// on the side effects of a load.
				// TODO(adonovan): emit dedicated nilcheck.
				addr.load(function, typed)
			}
			return addr.address(function)
		case token.ADD:
			return packageBuilder.expr(function, typed.X)
		case token.NOT, token.SUB, token.XOR: // ! <- - ^
			operation := &UnaryOperation{
				Operator: typed.Op,
				X:        packageBuilder.expr(function, typed.X),
			}
			operation.setType(function.typ(tv.Type))
			return function.emit(operation, typed)
		case token.ARROW:
			return emitReceive(function, packageBuilder.expr(function, typed.X), false, function.typ(tv.Type), typed)
		default:
			panic(typed.Op)
		}

	case *ast.BinaryExpr:
		switch typed.Op {
		case token.LAND, token.LOR:
			return packageBuilder.logicalBinop(function, typed)
		case token.SHL, token.SHR:
			fallthrough
		case token.ADD, token.SUB, token.MUL, token.QUO, token.REM, token.AND, token.OR, token.XOR, token.AND_NOT:
			return emitArith(function, typed.Op, packageBuilder.expr(function, typed.X), packageBuilder.expr(function, typed.Y), function.typ(tv.Type), typed)

		case token.EQL, token.NEQ, token.GTR, token.LSS, token.LEQ, token.GEQ:
			cmp := emitCompare(function, typed.Op, packageBuilder.expr(function, typed.X), packageBuilder.expr(function, typed.Y), typed)
			// The type of x==y may be UntypedBool.
			return emitConv(function, cmp, types.Default(function.typ(tv.Type)), typed)
		default:
			panic("illegal op in BinaryExpr: " + typed.Op.String())
		}

	case *ast.SliceExpr:
		var low, high, maximum Value
		var x Value
		xtyp := function.typeOf(typed.X)
		switch typeutil.CoreType(xtyp).(type) {
		case *types.Array:
			// Potentially escaping.
			x = packageBuilder.addr(function, typed.X, true).address(function)
		case *types.Basic, *types.Slice, *types.Pointer: // *array
			x = packageBuilder.expr(function, typed.X)
		default:
			// core type exception?
			if isBytestring(xtyp) {
				x = packageBuilder.expr(function, typed.X) // bytestring is handled as string and []byte.
			} else {
				panic("unexpected sequence type in SliceExpr")
			}
		}
		if typed.Low != nil {
			low = packageBuilder.expr(function, typed.Low)
		}
		if typed.High != nil {
			high = packageBuilder.expr(function, typed.High)
		}
		if typed.Slice3 {
			maximum = packageBuilder.expr(function, typed.Max)
		}
		slice := &Slice{
			X:       x,
			Low:     low,
			High:    high,
			Maximum: maximum,
		}
		slice.setType(function.typ(tv.Type))
		return function.emit(slice, typed)

	case *ast.Ident:
		object := function.info.Uses[typed]
		// Universal built-in or nil?
		switch typedObject := object.(type) {
		case *types.Builtin:
			return &Builtin{name: typedObject.Name(), sig: function.instanceType(typed).(*types.Signature)}
		case *types.Nil:
			return zeroConstant(function.instanceType(typed), typed)
		}

		// Package-level func or var?
		// (obj must belong to same package or a direct import.)
		if member := function.Program.packageLevelMember(object); member != nil {
			if global, ok := member.(*Global); ok {
				return emitLoad(function, global, typed) // var (address)
			}
			callee := member.(*Function) // (func)
			if callee.typeparams.Len() > 0 {
				targs := function.subtargs(typed)
				callee = callee.instance(nil, targs, packageBuilder)
			}
			return callee
		}
		// Local var.
		return emitLoad(function, function.lookup(object.(*types.Var), false), typed) // var (address)

	case *ast.SelectorExpr:
		sel := function.selection(typed)
		if sel == nil {
			// builtin unsafe.{Add,Slice}
			if object, ok := function.info.Uses[typed.Sel].(*types.Builtin); ok {
				return &Builtin{name: "Unsafe" + object.Name(), sig: function.typ(tv.Type).(*types.Signature)}
			}
			// qualified identifier
			return packageBuilder.expr(function, typed.Sel)
		}
		switch sel.kind {
		case types.MethodExpr:
			// (*T).f or T.f, the method f from the method-set of type T.
			// The result is a "thunk".
			targs := function.subtargs(typed.Sel)
			thunk := createThunk(function.Program, sel, targs)
			packageBuilder.enqueue(thunk)
			return thunk

		case types.MethodVal:
			// e.f where e is an expression and f is a method.
			// The result is a "bound".
			method := sel.object.(*types.Func)
			rt := function.typ(recvType(method))
			wantAddr := isPointer(rt)
			escaping := true
			value := packageBuilder.receiver(function, typed.X, wantAddr, escaping, sel, typed)

			if types.IsInterface(rt) {
				// If v may be an interface type I (after instantiating),
				// we must emit a check that v is non-nil.
				if recv, ok := types.Unalias(sel.recv).(*types.TypeParam); ok {
					// Emit a nil check if any possible instantiation of the
					// type parameter is an interface type.
					if !typeSetIsEmpty(recv) {
						// recv has a concrete term its typeset.
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
			indices := sel.index
			last := len(indices) - 1
			value := packageBuilder.expr(function, typed.X)
			value = emitImplicitSelections(function, value, indices[:last], typed)
			value = emitFieldSelection(function, value, indices[last], false, typed.Sel)
			return value
		}

		panic("unexpected expression-relative selector")

	case *ast.IndexListExpr:
		// f[X, Y] must be a generic function
		if !instance(function.info, typed.X) {
			panic("unexpected expression-could not match index list to instantiation")
		}
		return packageBuilder.expr(function, typed.X) // Handle instantiation within the *Ident or *SelectorExpr cases.

	case *ast.IndexExpr:
		if instance(function.info, typed.X) {
			return packageBuilder.expr(function, typed.X) // Handle instantiation within the *Ident or *SelectorExpr cases.
		}
		// not a generic instantiation.
		xt := function.typeOf(typed.X)
		switch et, mode := indexType(xt); mode {
		case ixVariable:
			// Addressable slice/array; use IndexAddress and Load.
			return packageBuilder.addr(function, typed, false).load(function, typed)

		case ixArrVariable, ixValue:
			// An array in a register, a string or a combined type that contains
			// either an [_]array (ixArrVar) or string (ixValue).

			// Note: for ixArrVar and CoreType(xt)==nil can be IndexAddress and Load.
			index := packageBuilder.expr(function, typed.Index)
			if isUntyped(index.Type()) {
				index = emitConv(function, index, tInt, typed.Index)
			}
			elementValue := &Index{
				X:     packageBuilder.expr(function, typed.X),
				Index: index,
			}
			elementValue.setType(et)
			return function.emit(elementValue, typed)

		case ixMap:
			ct := typeutil.CoreType(xt).(*types.Map)
			lookup := &MapLookup{
				X:     packageBuilder.expr(function, typed.X),
				Index: emitConv(function, packageBuilder.expr(function, typed.Index), ct.Key(), typed.Index),
			}
			lookup.setType(ct.Elem())
			return function.emit(lookup, typed)
		default:
			panic("unexpected container type in IndexExpr: " + xt.String())
		}

	case *ast.CompositeLit, *ast.StarExpr:
		// Addressable types (lvalues)
		return packageBuilder.addr(function, typed, false).load(function, typed)
	}

	panic(fmt.Sprintf("unexpected expr: %T", expr))
}

// stmtList emits to fn code for all statements in list.
func (packageBuilder *builder) stmtList(function *Function, list []ast.Stmt) {
	for _, stmt := range list {
		packageBuilder.stmt(function, stmt)
	}
}

// receiver emits to fn code for expression `expr` in the "receiver"
// position of selection e.f (where f may be a field or a method) and
// returns the effective receiver after applying the implicit field
// selections of sel.
//
// wantAddr requests that the result is an address.  If
// !sel.indirect, this may require that `expr` be built in addr() mode; it
// must thus be addressable.
//
// escaping is defined as per builder.addr().
func (packageBuilder *builder) receiver(
	function *Function,
	expr ast.Expr,
	wantAddr, escaping bool,
	sel *selection,
	source ast.Node,
) Value {
	var value Value
	if wantAddr && !sel.indirect && !isPointerCore(function.typeOf(expr)) {
		value = packageBuilder.addr(function, expr, escaping).address(function)
	} else {
		value = packageBuilder.expr(function, expr)
	}

	last := len(sel.index) - 1
	value = emitImplicitSelections(function, value, sel.index[:last], source)
	if types.IsInterface(value.Type()) {
		// When v is an interface, sel.Kind()==MethodValue and v.f is invoked.
		// So v is not loaded, even if v has a pointer core type.
	} else if !wantAddr && isPointerCore(value.Type()) {
		value = emitLoad(function, value, expr)
	}
	return value
}

// setCallFunc populates the function parts of a CallCommon structure
// (Function, Method, receiver, Arguments[0]) based on the kind of invocation
// occurring in `callExpr`.
func (packageBuilder *builder) setCallFunc(function *Function, callExpr *ast.CallExpr, common *CallCommon) {
	// Is this a (possibly generic) method call?
	calleeExpression := ast.Unparen(callExpr.Fun)
	switch typed := calleeExpression.(type) {
	case *ast.IndexExpr:
		calleeExpression = typed.X
	case *ast.IndexListExpr:
		calleeExpression = typed.X
	}
	if selector, ok := calleeExpression.(*ast.SelectorExpr); ok {
		sel := function.selection(selector)
		if sel != nil && sel.kind == types.MethodVal {
			object := sel.object.(*types.Func)
			recv := recvType(object)

			wantAddr := isPointer(recv)
			escaping := true
			value := packageBuilder.receiver(function, selector.X, wantAddr, escaping, sel, selector)
			if types.IsInterface(recv) {
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
		// For now, we evaluate it in the usual way.
		//
		// TODO(adonovan): opt: inline expr() here, to make the
		// call static and to avoid generation of wrappers.
		// It's somewhat tricky as it may consume the first
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
		// Suggested approach:
		// - consume the first actual parameter expression
		//   and build it with b.expr().
		// - apply implicit field selections.
		// - use MethodVal logic to populate fields of c.
	}

	// Evaluate the function operand in the usual way.
	common.Value = packageBuilder.expr(function, callExpr.Fun)
}

// emitCallArguments emits to f code for the actual parameters of call `expr` to
// a (possibly built-in) function of effective type sig.
// The argument values are appended to args, which is then returned.
func (packageBuilder *builder) emitCallArguments(
	function *Function,
	sig *types.Signature,
	expr *ast.CallExpr,
	arguments []Value,
) []Value {
	// f(x, y, z...): pass slice z straight through.
	if expr.Ellipsis != 0 {
		for index, argument := range expr.Args {
			value := emitConv(function, packageBuilder.expr(function, argument), sig.Params().At(index).Type(), argument)
			arguments = append(arguments, value)
		}
		return arguments
	}

	offset := len(arguments) // 1 if call has receiver, 0 otherwise

	// Evaluate actual parameter expressions.
	//
	// If this is a chained call of the form f(g()) where g has
	// multiple return values (MRV), they are flattened out into
	// args; a suffix of them may end up in a varargs slice.
	for _, argument := range expr.Args {
		value := packageBuilder.expr(function, argument)
		if ttuple, ok := value.Type().(*types.Tuple); ok { // MRV chain
			for index, length := 0, ttuple.Len(); index < length; index++ {
				arguments = append(arguments, emitExtract(function, value, index, argument))
			}
		} else {
			arguments = append(arguments, value)
		}
	}

	// Actual->formal assignability conversions for normal parameters.
	np := sig.Params().Len() // number of normal parameters
	if sig.Variadic() {
		np--
	}
	for index := 0; index < np; index++ {
		arguments[offset+index] = emitConv(
			function,
			arguments[offset+index],
			sig.Params().At(index).Type(),
			arguments[offset+index].Source(),
		)
	}

	// Actual->formal assignability conversions for variadic parameter,
	// and construction of slice.
	if sig.Variadic() {
		varargs := arguments[offset+np:]
		st := sig.Params().At(np).Type().(*types.Slice)
		vt := st.Elem()
		if len(varargs) == 0 {
			arguments = append(arguments, zeroConstant(st, nil))
		} else {
			// Replace a suffix of args with a slice containing it.
			at := types.NewArray(vt, int64(len(varargs)))
			allocation := emitNew(function, at, expr, "varargs")
			for index, argument := range varargs {
				iaddr := &IndexAddress{
					X:     allocation,
					Index: intConstant(int64(index), nil),
				}
				iaddr.setType(types.NewPointer(vt))
				function.emit(iaddr, expr)
				emitStore(function, iaddr, argument, argument.Source())
			}
			slice := &Slice{X: allocation}
			slice.setType(st)
			arguments[offset+np] = function.emit(slice, arguments[offset+np].Source())
			arguments = arguments[:offset+np+1]
		}
	}
	return arguments
}

// setCall emits to fn code to evaluate all the parameters of a function
// call `expr`, and populates *`common` with those values.
func (packageBuilder *builder) setCall(function *Function, expr *ast.CallExpr, common *CallCommon) {
	// First deal with the f(...) part and optional receiver.
	packageBuilder.setCallFunc(function, expr, common)

	// Then append the other actual parameters.
	sig, _ := typeutil.CoreType(function.typeOf(expr.Fun)).(*types.Signature)
	if sig == nil {
		panic(fmt.Sprintf("no signature for call of %s", expr.Fun))
	}
	common.Arguments = packageBuilder.emitCallArguments(function, sig, expr, common.Arguments)
}

// assignOp emits to fn code to perform loc <op>= val.
func (packageBuilder *builder) assignOp(function *Function, loc lvalue, value Value, op token.Token, source ast.Node) {
	loc.store(function, emitArith(function, op, loc.load(function, source), value, loc.typ(), source), source)
}

// localValueSpec emits to fn code to define all of the vars in the
// function-local ValueSpec, spec.
func (packageBuilder *builder) localValueSpec(function *Function, spec *ast.ValueSpec) {
	switch {
	case len(spec.Values) == len(spec.Names):
		// e.g. var x, y = 0, 1
		// 1:1 assignment
		for index, id := range spec.Names {
			if !isBlankIdent(id) {
				emitLocalVariable(function, identVariable(function, id), id)
			}
			lval := packageBuilder.addr(function, id, false) // non-escaping
			packageBuilder.assign(function, lval, spec.Values[index], true, nil, spec)
		}

	case len(spec.Values) == 0:
		// e.g. var x, y int
		// Locals are implicitly zero-initialized.
		for _, id := range spec.Names {
			if !isBlankIdent(id) {
				emitLocalVariable(function, identVariable(function, id), id)
			}
		}

	default:
		// e.g. var x, y = pos()
		tuple := packageBuilder.exprN(function, spec.Values[0])
		for index, id := range spec.Names {
			if !isBlankIdent(id) {
				emitLocalVariable(function, identVariable(function, id), id)
				lhs := packageBuilder.addr(function, id, false) // non-escaping
				lhs.store(function, emitExtract(function, tuple, index, id), id)
			}
		}
	}
}

// assignStmt emits code to fn for a parallel assignment of rhss to lhss.
// isDef is true if this is a short variable declaration (:=).
//
// Note the similarity with localValueSpec.
func (packageBuilder *builder) assignStmt(function *Function, lhss, rhss []ast.Expr, isDef bool, source ast.Node) {
	// Side effects of all LHSs and RHSs must occur in left-to-right order.
	lvals := make([]lvalue, len(lhss))
	isZero := make([]bool, len(lhss))
	for index, lhs := range lhss {
		var lval lvalue = blank{}
		if !isBlankIdent(lhs) {
			if isDef {
				if object, ok := function.info.Defs[lhs.(*ast.Ident)].(*types.Var); ok {
					emitLocalVariable(function, object, lhs)
					isZero[index] = true
				}
			}
			lval = packageBuilder.addr(function, lhs, false) // non-escaping
		}
		lvals[index] = lval
	}
	if len(lhss) == len(rhss) {
		// Simple assignment:   x     = f()        (!isDef)
		// Parallel assignment: x, y  = f(), g()   (!isDef)
		// or short var decl:   x, y := f(), g()   (isDef)
		//
		// In all cases, the RHSs may refer to the LHSs,
		// so we need a storebuf.
		var stores storebuf
		for index := range rhss {
			packageBuilder.assign(function, lvals[index], rhss[index], isZero[index], &stores, source)
		}
		stores.emit(function)
	} else {
		// e.g. x, y = pos()
		tuple := packageBuilder.exprN(function, rhss[0])
		for index, lval := range lvals {
			lval.store(function, emitExtract(function, tuple, index, source), source)
		}
	}
}

// arrayLength returns the length of the array whose composite literal elements are elts.
func (packageBuilder *builder) arrayLength(function *Function, elts []ast.Expr) int64 {
	var maximum int64 = -1
	var index int64 = -1
	for _, elt := range elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			index = packageBuilder.expr(function, kv.Key).(*Constant).Int64()
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
// address addr with type typ.
//
// Nested composite literals are recursively initialized in place
// where possible. If isZero is true, compLit assumes that addr
// holds the zero value for typ.
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
// In that case, addr must hold a T, not a *T.
func (packageBuilder *builder) compLit(function *Function, addr Value, lit *ast.CompositeLit, isZero bool, deferredStores *storebuf) {
	typ := deref(function.typeOf(lit)) // retain the named/alias/param type, if any
	switch typed := typeutil.CoreType(typ).(type) {
	case *types.Struct:
		lvalue := &address{addr: addr}
		if len(lit.Elts) == 0 {
			if !isZero {
				deferredStores.store(lvalue, zeroConstant(deref(addr.Type()), lit), lit)
			}
		} else {
			value := &CompositeValue{
				Values: make([]Value, typed.NumFields()),
			}
			for index := range typed.NumFields() {
				value.Values[index] = zeroConstant(typed.Field(index).Type(), lit)
			}
			value.setType(typ)

			type chainElement struct {
				children []chainElement
				cv       *CompositeValue
			}

			chain := chainElement{
				cv: value,
			}

			for eltIndex, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					fname := kv.Key.(*ast.Ident).Name
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
						chainCoreType = typeutil.CoreType(treeType).(*types.Struct)
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

				// XXX better e
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
			array = addr
		}

		var final Value
		if len(lit.Elts) == 0 {
			if !isZero {
				zc := zeroConstant(at, lit)
				final = zc
			}
		} else {
			if at.Len() == int64(len(lit.Elts)) {
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
						index = packageBuilder.expr(function, kv.Key).(*Constant)
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
					deferredStores.store(&address{array}, zeroConstant(deref(array.Type()), lit), lit)
				}

				var index *Constant
				for _, elt := range lit.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						index = packageBuilder.expr(function, kv.Key).(*Constant)
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
						packageBuilder.assign(function, &address{addr: iaddr}, elt, true, nil, elt)
					} else {
						packageBuilder.assign(function, &address{addr: iaddr}, elt, true, deferredStores, elt)
					}
				}
			}
		}
		if typed != at { // slice
			if final != nil {
				deferredStores.store(&address{addr: array}, final, lit)
			}
			slice := &Slice{X: array}
			slice.setType(typ)
			deferredStores.store(&address{addr: addr}, function.emit(slice, lit), lit)
		} else if final != nil {
			deferredStores.store(&address{addr: array}, final, lit)
		}

	case *types.Map:
		makeMap := &MakeMap{Reserve: intConstant(int64(len(lit.Elts)), lit)}
		makeMap.setType(typ)
		function.emit(makeMap, lit)
		for _, elt := range lit.Elts {
			expr := elt.(*ast.KeyValueExpr)

			// If a key expression in a map literal is  itself a
			// composite literal, the type may be omitted.
			// For example:
			//	map[*struct{}]bool{{}: true}
			// An &-operation may be implied:
			//	map[*struct{}]bool{&struct{}{}: true}
			wantAddr := false
			if _, ok := ast.Unparen(expr.Key).(*ast.CompositeLit); ok {
				wantAddr = isPointerCore(typed.Key())
			}

			var key Value
			if wantAddr {
				// A CompositeLit never evaluates to a pointer,
				// so if the type of the location is a pointer,
				// an &-operation is implied.
				key = packageBuilder.addr(function, expr.Key, true).address(function)
			} else {
				key = packageBuilder.expr(function, expr.Key)
			}

			loc := element{
				mapValue:    makeMap,
				key:         emitConv(function, key, typed.Key(), expr),
				elementType: typed.Elem(),
			}

			// We call assign() only because it takes care
			// of any &-operation required in the recursive
			// case, e.g.,
			// map[int]*struct{}{0: {}} implies &struct{}{}.
			// In-place update is of course impossible,
			// and no storebuf is needed.
			packageBuilder.assign(function, &loc, expr.Value, true, nil, expr)
		}
		deferredStores.store(&address{addr: addr}, makeMap, lit)

	default:
		panic("unexpected CompositeLit type: " + typ.String())
	}
}

func (packageBuilder *builder) switchStmt(function *Function, switchStmt *ast.SwitchStmt, label *lblock) {
	if switchStmt.Tag == nil {
		packageBuilder.switchStmtDynamic(function, switchStmt, label)
		return
	}
	dynamic := false
	for _, iclause := range switchStmt.Body.List {
		clause := iclause.(*ast.CaseClause)
		for _, cond := range clause.List {
			if function.info.Types[ast.Unparen(cond)].Value == nil {
				dynamic = true
				break
			}
		}
	}

	if dynamic {
		packageBuilder.switchStmtDynamic(function, switchStmt, label)
		return
	}

	if switchStmt.Init != nil {
		packageBuilder.stmt(function, switchStmt.Init)
	}

	entry := function.currentBlock
	tag := packageBuilder.expr(function, switchStmt.Tag)

	heads := make([]*BasicBlock, 0, len(switchStmt.Body.List))
	bodies := make([]*BasicBlock, len(switchStmt.Body.List))
	conds := make([]Value, 0, len(switchStmt.Body.List))

	hasDefault := false
	done := function.newBasicBlock("switch.done")
	if label != nil {
		label._break = done
	}
	for caseIndex, stmt := range switchStmt.Body.List {
		body := function.newBasicBlock(fmt.Sprintf("switch.body.%d", caseIndex))
		bodies[caseIndex] = body
		cas := stmt.(*ast.CaseClause)
		if cas.List == nil {
			// default branch
			hasDefault = true
			head := function.newBasicBlock(fmt.Sprintf("switch.head.%d", caseIndex))
			conds = append(conds, nil)
			heads = append(heads, head)
			function.currentBlock = head
			emitJump(function, body, cas)
		}
		for conditionIndex, cond := range stmt.(*ast.CaseClause).List {
			function.currentBlock = entry
			head := function.newBasicBlock(fmt.Sprintf("switch.head.%d.%d", caseIndex, conditionIndex))
			conds = append(conds, packageBuilder.expr(function, cond))
			heads = append(heads, head)
			function.currentBlock = head
			emitJump(function, body, cond)
		}
	}

	for index, stmt := range switchStmt.Body.List {
		clause := stmt.(*ast.CaseClause)
		body := bodies[index]
		function.currentBlock = body
		fallthru := done
		if index+1 < len(bodies) {
			fallthru = bodies[index+1]
		}
		function.targets = &targets{
			tail:         function.targets,
			_break:       done,
			_fallthrough: fallthru,
		}
		packageBuilder.stmtList(function, clause.Body)
		function.targets = function.targets.tail
		emitJump(function, done, stmt)
	}

	if !hasDefault {
		head := function.newBasicBlock("switch.head.implicit-default")
		body := function.newBasicBlock("switch.body.implicit-default")
		function.currentBlock = head
		emitJump(function, body, switchStmt)
		function.currentBlock = body
		emitJump(function, done, switchStmt)
		heads = append(heads, head)
		conds = append(conds, nil)
	}

	if len(heads) != len(conds) {
		panic(fmt.Sprintf("internal error: %d heads for %d conds", len(heads), len(conds)))
	}
	for _, head := range heads {
		addEdge(entry, head)
	}
	function.currentBlock = entry
	entry.emit(&ConstantSwitch{
		Tag:        tag,
		Conditions: conds,
	}, switchStmt)
	function.currentBlock = done
}

// switchStmt emits to fn code for the switch statement `stmt`, optionally
// labelled by label.
func (packageBuilder *builder) switchStmtDynamic(function *Function, stmt *ast.SwitchStmt, label *lblock) {
	// We treat SwitchStmt like a sequential if-else chain.
	// Multiway dispatch can be recovered later by irutil.Switches()
	// to those cases that are free of side effects.
	if stmt.Init != nil {
		packageBuilder.stmt(function, stmt.Init)
	}
	var tag Value = vTrue

	if stmt.Tag != nil {
		tag = packageBuilder.expr(function, stmt.Tag)
	}

	done := function.newBasicBlock("switch.done")
	if label != nil {
		label._break = done
	}
	// We pull the default case (if present) down to the end.
	// But each fallthrough label must point to the next
	// body block in source order, so we preallocate a
	// body block (fallthru) for the next case.
	// Unfortunately this makes for a confusing block order.
	var dfltBody *[]ast.Stmt
	var dfltFallthrough *BasicBlock
	var fallthru, dfltBlock *BasicBlock
	ncases := len(stmt.Body.List)
	for index, clause := range stmt.Body.List {
		body := fallthru
		if body == nil {
			body = function.newBasicBlock("switch.body") // first case only
		}

		// Preallocate body block for the next case.
		fallthru = done
		if index+1 < ncases {
			fallthru = function.newBasicBlock("switch.body")
		}

		cc := clause.(*ast.CaseClause)
		if cc.List == nil {
			// Default case.
			dfltBody = &cc.Body
			dfltFallthrough = fallthru
			dfltBlock = body
			continue
		}

		var nextCond *BasicBlock
		for _, cond := range cc.List {
			nextCond = function.newBasicBlock("switch.next")
			// For boolean switches, emit short-circuit control flow,
			// just like an if/else-chain.
			if tag == vTrue && !isNonTypeParamInterface(function.info.Types[cond].Type) {
				packageBuilder.cond(function, cond, body, nextCond)
			} else {
				cond := emitCompare(function, token.EQL, tag, packageBuilder.expr(function, cond), cond)
				emitIf(function, cond, body, nextCond, cond.Source())
			}

			function.currentBlock = nextCond
		}
		function.currentBlock = body
		function.targets = &targets{
			tail:         function.targets,
			_break:       done,
			_fallthrough: fallthru,
		}
		packageBuilder.stmtList(function, cc.Body)
		function.targets = function.targets.tail
		emitJump(function, done, stmt)
		function.currentBlock = nextCond
	}
	if dfltBlock != nil {
		// The lack of a Source for the jump doesn't matter, block
		// fusing will get rid of the jump later.

		emitJump(function, dfltBlock, stmt)
		function.currentBlock = dfltBlock
		function.targets = &targets{
			tail:         function.targets,
			_break:       done,
			_fallthrough: dfltFallthrough,
		}
		packageBuilder.stmtList(function, *dfltBody)
		function.targets = function.targets.tail
	}
	emitJump(function, done, stmt)
	function.currentBlock = done
}

// typeSwitchStmt emits to fn code for the type switch statement `stmt`, optionally
// labelled by label.
func (packageBuilder *builder) typeSwitchStmt(function *Function, stmt *ast.TypeSwitchStmt, label *lblock) {
	if stmt.Init != nil {
		packageBuilder.stmt(function, stmt.Init)
	}

	var tag Value
	switch typed := stmt.Assign.(type) {
	case *ast.ExprStmt: // x.(type)
		tag = packageBuilder.expr(function, ast.Unparen(typed.X).(*ast.TypeAssertExpr).X)
	case *ast.AssignStmt: // y := x.(type)
		tag = packageBuilder.expr(function, ast.Unparen(typed.Rhs[0]).(*ast.TypeAssertExpr).X)
	default:
		panic("unreachable")
	}

	// +1 in case there's no explicit default case
	heads := make([]*BasicBlock, 0, len(stmt.Body.List)+1)

	entry := function.currentBlock
	done := function.newBasicBlock("typeswitch.done")
	if label != nil {
		label._break = done
	}

	// set up type switch and constant switch, populate their conditions
	tswtch := &TypeSwitch{
		Tag:        tag,
		Conditions: make([]types.Type, 0, len(stmt.Body.List)+1),
	}
	cswtch := &ConstantSwitch{
		Conditions: make([]Value, 0, len(stmt.Body.List)+1),
	}

	rets := make([]types.Type, 0, len(stmt.Body.List)+1)
	index := 0
	var default_ *ast.CaseClause
	for _, clause := range stmt.Body.List {
		cc := clause.(*ast.CaseClause)
		if object, ok := function.info.Implicits[cc].(*types.Var); ok {
			emitLocalVariable(function, object, cc)
		}
		if cc.List == nil {
			// default case
			default_ = cc
		} else {
			for _, expr := range cc.List {
				tswtch.Conditions = append(tswtch.Conditions, function.typeOf(expr))
				cswtch.Conditions = append(cswtch.Conditions, intConstant(int64(index), expr))
				index++
			}
			if len(cc.List) == 1 {
				rets = append(rets, function.typeOf(cc.List[0]))
			} else {
				for range cc.List {
					rets = append(rets, tag.Type())
				}
			}
		}
	}

	// default branch
	rets = append(rets, tag.Type())

	var vars []*types.Var
	vars = append(vars, varIndex)
	for _, typ := range rets {
		vars = append(vars, anonVariable(typ))
	}
	tswtch.setType(types.NewTuple(vars...))
	// default branch
	function.currentBlock = entry
	function.emit(tswtch, stmt)
	cswtch.Conditions = append(cswtch.Conditions, intConstant(int64(-1), nil))
	cswtch.Tag = emitExtract(function, tswtch, 0, stmt)
	function.emit(cswtch, stmt)

	// build heads and bodies
	index = 0
	for _, clause := range stmt.Body.List {
		cc := clause.(*ast.CaseClause)
		if cc.List == nil {
			continue
		}

		body := function.newBasicBlock("typeswitch.body")
		for _, expr := range cc.List {
			head := function.newBasicBlock("typeswitch.head")
			heads = append(heads, head)
			function.currentBlock = head

			if object, ok := function.info.Implicits[cc].(*types.Var); ok {
				// In a switch y := x.(type), each case clause
				// implicitly declares a distinct object y.
				// In a single-type case, y has that type.
				// In multi-type cases, 'case nil' and default,
				// y has the same type as the interface operand.

				variable := function.vars[object]
				if rets[index] == tUntypedNil {
					emitStore(function, variable, nilConstant(tswtch.Tag.Type(), nil), stmt.Assign)
				} else {
					x := emitExtract(function, tswtch, index+1, stmt.Assign)
					emitStore(function, variable, x, nil)
				}
			}

			emitJump(function, body, expr)
			index++
		}
		function.currentBlock = body
		function.targets = &targets{
			tail:   function.targets,
			_break: done,
		}
		packageBuilder.stmtList(function, cc.Body)
		function.targets = function.targets.tail
		emitJump(function, done, clause)
	}

	if default_ == nil {
		// implicit default
		heads = append(heads, done)
	} else {
		body := function.newBasicBlock("typeswitch.default")
		heads = append(heads, body)
		function.currentBlock = body
		function.targets = &targets{
			tail:   function.targets,
			_break: done,
		}
		if object, ok := function.info.Implicits[default_].(*types.Var); ok {
			variable := function.vars[object]
			x := emitExtract(function, tswtch, index+1, stmt.Assign)
			emitStore(function, variable, x, stmt)
		}
		packageBuilder.stmtList(function, default_.Body)
		function.targets = function.targets.tail
		emitJump(function, done, stmt)
	}

	function.currentBlock = entry
	for _, head := range heads {
		addEdge(entry, head)
	}
	function.currentBlock = done
}

// selectStmt emits to fn code for the select statement `stmt`, optionally
// labelled by label.
func (packageBuilder *builder) selectStmt(function *Function, stmt *ast.SelectStmt, label *lblock) (noreturn bool) {
	if len(stmt.Body.List) == 0 {
		instr := &Select{Blocking: true}
		instr.setType(types.NewTuple(varIndex, varOk))
		function.emit(instr, stmt)
		function.emit(new(Unreachable), stmt)
		return true
	}

	// A blocking select of a single case degenerates to a
	// simple send or receive.
	// TODO(adonovan): opt: is this optimization worth its weight?
	if len(stmt.Body.List) == 1 {
		clause := stmt.Body.List[0].(*ast.CommClause)
		if clause.Comm != nil {
			packageBuilder.stmt(function, clause.Comm)
			done := function.newBasicBlock("select.done")
			if label != nil {
				label._break = done
			}
			function.targets = &targets{
				tail:   function.targets,
				_break: done,
			}
			packageBuilder.stmtList(function, clause.Body)
			function.targets = function.targets.tail
			emitJump(function, done, clause)
			function.currentBlock = done
			return false
		}
	}

	// First evaluate all channels in all cases, and find
	// the directions of each state.
	var states []*SelectState
	blocking := true
	for _, clause := range stmt.Body.List {
		var st *SelectState
		switch comm := clause.(*ast.CommClause).Comm.(type) {
		case nil: // default case
			blocking = false
			continue

		case *ast.SendStmt: // ch<- i
			channel := packageBuilder.expr(function, comm.Chan)
			st = &SelectState{
				Direction: types.SendOnly,
				Channel:   channel,
				Send: emitConv(function, packageBuilder.expr(function, comm.Value),
					typeutil.CoreType(function.typ(channel.Type())).(*types.Chan).Elem(), comm),
				Position: comm.Arrow,
			}

		case *ast.AssignStmt: // x := <-ch
			recv := ast.Unparen(comm.Rhs[0]).(*ast.UnaryExpr)
			st = &SelectState{
				Direction: types.RecvOnly,
				Channel:   packageBuilder.expr(function, recv.X),
				Position:  recv.OpPos,
			}

		case *ast.ExprStmt: // <-ch
			recv := ast.Unparen(comm.X).(*ast.UnaryExpr)
			st = &SelectState{
				Direction: types.RecvOnly,
				Channel:   packageBuilder.expr(function, recv.X),
				Position:  recv.OpPos,
			}
		}
		states = append(states, st)
	}

	// We dispatch on the (fair) result of Select using a
	// switch on the returned index.
	sel := &Select{
		States:   states,
		Blocking: blocking,
	}
	sel.source = stmt
	var vars []*types.Var
	vars = append(vars, varIndex, varOk)
	for _, st := range states {
		if st.Direction == types.RecvOnly {
			tElem := typeutil.CoreType(function.typ(st.Channel.Type())).(*types.Chan).Elem()
			vars = append(vars, anonVariable(tElem))
		}
	}
	sel.setType(types.NewTuple(vars...))
	function.emit(sel, stmt)
	index := emitExtract(function, sel, 0, stmt)

	done := function.newBasicBlock("select.done")
	if label != nil {
		label._break = done
	}

	entry := function.currentBlock
	swtch := &ConstantSwitch{
		Tag: index,
		// one condition per case
		Conditions: make([]Value, 0, len(stmt.Body.List)+1),
	}
	// note that we don't need heads; a select case can only have a single condition
	var bodies []*BasicBlock

	state := 0
	receivedValueIndex := 2 // index in 'sel' tuple of value; increments if st.Direction==RECV
	for _, cc := range stmt.Body.List {
		clause := cc.(*ast.CommClause)
		if clause.Comm == nil {
			body := function.newBasicBlock("select.default")
			function.currentBlock = body
			bodies = append(bodies, body)
			function.targets = &targets{
				tail:   function.targets,
				_break: done,
			}
			packageBuilder.stmtList(function, clause.Body)
			emitJump(function, done, stmt)
			function.targets = function.targets.tail
			swtch.Conditions = append(swtch.Conditions, intConstant(-1, nil))
			continue
		}
		swtch.Conditions = append(swtch.Conditions, intConstant(int64(state), nil))
		body := function.newBasicBlock("select.body")
		function.currentBlock = body
		bodies = append(bodies, body)
		function.targets = &targets{
			tail:   function.targets,
			_break: done,
		}
		switch comm := clause.Comm.(type) {
		case *ast.ExprStmt: // <-ch
			receivedValueIndex++

		case *ast.AssignStmt: // x := <-states[state].Channel
			if comm.Tok == token.DEFINE {
				id := comm.Lhs[0].(*ast.Ident)
				emitLocalVariable(function, identVariable(function, id), id)
			}
			x := packageBuilder.addr(function, comm.Lhs[0], false) // non-escaping
			value := emitExtract(function, sel, receivedValueIndex, comm)
			x.store(function, value, comm)

			if len(comm.Lhs) == 2 { // x, ok := ...
				if comm.Tok == token.DEFINE {
					id := comm.Lhs[1].(*ast.Ident)
					emitLocalVariable(function, identVariable(function, id), id)
				}
				ok := packageBuilder.addr(function, comm.Lhs[1], false) // non-escaping
				ok.store(function, emitExtract(function, sel, 1, comm), comm)
			}
			receivedValueIndex++
		}
		packageBuilder.stmtList(function, clause.Body)
		function.targets = function.targets.tail
		emitJump(function, done, stmt)
		state++
	}
	function.currentBlock = entry
	function.emit(swtch, stmt)
	for _, body := range bodies {
		addEdge(entry, body)
	}
	function.currentBlock = done
	return false
}

// forStmt emits to fn code for the for statement `stmt`, optionally
// labelled by label.
func (packageBuilder *builder) forStmt(function *Function, stmt *ast.ForStmt, label *lblock) {
	// Use forStmtGo122 instead if it applies.
	if stmt.Init != nil {
		if assign, ok := stmt.Init.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE {
			if goVersionAtLeast(function.goversion, "go1.22") {
				packageBuilder.forStmtGo122(function, stmt, label)
				return
			}
		}
	}

	//     ...init...
	//     jump loop
	// loop:
	//     if cond goto body else done
	// body:
	//     ...body...
	//     jump post
	// post:                                 (target of continue)
	//     ...post...
	//     jump loop
	// done:                                 (target of break)
	if stmt.Init != nil {
		packageBuilder.stmt(function, stmt.Init)
	}
	body := function.newBasicBlock("for.body")
	done := function.newBasicBlock("for.done") // target of 'break'
	loop := body                               // target of back-edge
	if stmt.Cond != nil {
		loop = function.newBasicBlock("for.loop")
	}
	cont := loop // target of 'continue'
	if stmt.Post != nil {
		cont = function.newBasicBlock("for.post")
	}
	if label != nil {
		label._break = done
		label._continue = cont
	}
	emitJump(function, loop, stmt)
	function.currentBlock = loop
	if loop != body {
		packageBuilder.cond(function, stmt.Cond, body, done)
		function.currentBlock = body
	}
	function.targets = &targets{
		tail:      function.targets,
		_break:    done,
		_continue: cont,
	}
	packageBuilder.stmt(function, stmt.Body)
	function.targets = function.targets.tail
	emitJump(function, cont, stmt)

	if stmt.Post != nil {
		function.currentBlock = cont
		packageBuilder.stmt(function, stmt.Post)
		emitJump(function, loop, stmt) // back-edge
	}
	function.currentBlock = done
}

// forStmtGo122 emits to fn code for the for statement `stmt`, optionally
// labelled by label. `stmt` must define its variables.
//
// This allocates once per loop iteration. This is only correct in
// GoVersions >= go1.22.
func (packageBuilder *builder) forStmtGo122(function *Function, stmt *ast.ForStmt, label *lblock) {
	//     i_outer = alloc[T]
	//     *i_outer = ...init...        // under objects[i] = i_outer
	//     jump loop
	// loop:
	//     i = phi [head: i_outer, loop: i_next]
	//     ...cond...                   // under objects[i] = i
	//     if cond goto body else done
	// body:
	//     ...body...                   // under objects[i] = i (same as loop)
	//     jump post
	// post:
	//     tmp = *i
	//     i_next = alloc[T]
	//     *i_next = tmp
	//     ...post...                   // under objects[i] = i_next
	//     goto loop
	// done:

	init := stmt.Init.(*ast.AssignStmt)
	startingBlocks := len(function.Blocks)

	pre := function.currentBlock               // current block before starting
	loop := function.newBasicBlock("for.loop") // target of back-edge
	body := function.newBasicBlock("for.body")
	post := function.newBasicBlock("for.post") // target of 'continue'
	done := function.newBasicBlock("for.done") // target of 'break'

	// For each of the n loop variables, we create five SSA values,
	// outer, phi, next, load, and store in pre, loop, and post.
	// There is no limit on n.
	type loopVariable struct {
		object *types.Var
		outer  *Allocation
		phi    *Phi
		load   *Load
		next   *Allocation
		store  *Store
	}
	vars := make([]loopVariable, len(init.Lhs))
	for index, lhs := range init.Lhs {
		variable := identVariable(function, lhs.(*ast.Ident))
		typ := function.typ(variable.Type())

		function.currentBlock = pre
		outer := emitLocal(function, typ, lhs, variable.Name())

		function.currentBlock = loop
		phi := &Phi{}
		phi.comment = variable.Name()
		phi.typ = outer.Type()
		function.emit(phi, lhs)

		function.currentBlock = post
		// If next is local, it reuses the address and zeroes the old value so
		// load before allocating next.
		load := emitLoad(function, phi, init)
		next := emitLocal(function, typ, lhs, variable.Name())
		store := emitStore(function, next, load, stmt)

		phi.Edges = []Value{outer, next} // pre edge is emitted before post edge.
		vars[index] = loopVariable{variable, outer, phi, load, next, store}
	}

	// ...init... under fn.objects[v] = i_outer
	function.currentBlock = pre
	for _, variable := range vars {
		function.vars[variable.object] = variable.outer
	}
	const isDef = false // assign to already-allocated outers
	packageBuilder.assignStmt(function, init.Lhs, init.Rhs, isDef, init)
	if label != nil {
		label._break = done
		label._continue = post
	}
	emitJump(function, loop, stmt)

	// ...cond... under fn.objects[v] = i
	function.currentBlock = loop
	for _, variable := range vars {
		function.vars[variable.object] = variable.phi
	}
	if stmt.Cond != nil {
		packageBuilder.cond(function, stmt.Cond, body, done)
	} else {
		emitJump(function, body, stmt)
	}

	// ...body... under fn.objects[v] = i
	function.currentBlock = body
	function.targets = &targets{
		tail:      function.targets,
		_break:    done,
		_continue: post,
	}
	packageBuilder.stmt(function, stmt.Body)
	function.targets = function.targets.tail
	emitJump(function, post, stmt)

	// ...post... under fn.objects[v] = i_next
	for _, variable := range vars {
		function.vars[variable.object] = variable.next
	}
	function.currentBlock = post
	if stmt.Post != nil {
		packageBuilder.stmt(function, stmt.Post)
	}
	emitJump(function, loop, stmt) // back-edge
	function.currentBlock = done

	// For each loop variable that does not escape,
	// (the common case), fuse its next cells into its
	// (local) outer cell as they have disjoint live ranges.
	//
	// It is sufficient to test whether i_next escapes,
	// because its Heap flag will be marked true if either
	// the cond or post expression causes i to escape
	// (because escape distributes over phi).
	var nlocals int
	for _, variable := range vars {
		if !variable.next.Heap {
			nlocals++
		}
	}
	if nlocals > 0 {
		replace := make(map[Value]Value, 2*nlocals)
		dead := make(map[Instruction]bool, 4*nlocals)
		for _, variable := range vars {
			if !variable.next.Heap {
				replace[variable.next] = variable.outer
				replace[variable.phi] = variable.outer
				dead[variable.phi], dead[variable.next], dead[variable.load], dead[variable.store] = true, true, true, true
			}
		}

		// Replace all uses of i_next and phi with i_outer.
		// Referrers have not been built for fn yet so only update Instruction operands.
		// We need only look within the blocks added by the loop.
		var operands []*Value // recycle storage
		for _, block := range function.Blocks[startingBlocks:] {
			for _, instr := range block.Instructions {
				operands = instr.Operands(operands[:0])
				for _, ptr := range operands {
					operand := *ptr
					if replacement := replace[operand]; replacement != nil {
						*ptr = replacement
					}
				}
			}
		}

		// Remove instructions for phi, load, and store.
		// lift() will remove the unused i_next *Allocation.
		isDead := func(instruction Instruction) bool { return dead[instruction] }
		loop.Instructions = slices.DeleteFunc(loop.Instructions, isDead)
		post.Instructions = slices.DeleteFunc(post.Instructions, isDead)
	}
}

// rangeIndexed emits to fn the header for an integer-indexed loop
// over array, *array or slice value x.
// The v result is defined only if tv is non-nil.
// forPos is the position of the "for" token.
func (packageBuilder *builder) rangeIndexed(
	function *Function,
	x Value,
	tv types.Type,
	source ast.Node,
) (key, value Value, loop, done *BasicBlock) {
	//
	//     length = len(x)
	//     index = -1
	// loop:                                     (target of continue)
	//     index++
	//     if index < length goto body else done
	// body:
	//     k = index
	//     v = x[index]
	//     ...body...
	//     jump loop
	// done:                                     (target of break)

	// Determine number of iterations.
	var length Value
	dt := deref(x.Type())
	if array, ok := typeutil.CoreType(dt).(*types.Array); ok {
		// For array or *array, the number of iterations is
		// known statically thanks to the type.  We avoid a
		// data dependence upon x, permitting later dead-code
		// elimination if x is pure, static unrolling, etc.
		// Ranging over a nil *array may have >0 iterations.
		// We still generate code for x, in case it has effects.
		length = intConstant(array.Len(), nil)
	} else {
		// length = len(x).
		var call Call
		call.Call.Value = makeLength(x.Type())
		call.Call.Arguments = []Value{x}
		call.setType(tInt)
		length = function.emit(&call, source)
	}

	index := emitLocal(function, tInt, source, "rangeindex")
	emitStore(function, index, intConstant(-1, nil), source)

	loop = function.newBasicBlock("rangeindex.loop")
	loop.RangeIndexLoop = true
	emitJump(function, loop, source)
	function.currentBlock = loop

	incr := &BinaryOperation{
		Operator: token.ADD,
		X:        emitLoad(function, index, source),
		Y:        vOne,
	}
	incr.setType(tInt)
	emitStore(function, index, function.emit(incr, source), source)

	body := function.newBasicBlock("rangeindex.body")
	done = function.newBasicBlock("rangeindex.done")
	emitIf(function, emitCompare(function, token.LSS, incr, length, source), body, done, source)
	function.currentBlock = body

	key = emitLoad(function, index, source)
	if tv != nil {
		switch typed := typeutil.CoreType(x.Type()).(type) {
		case *types.Array:
			instr := &Index{
				X:     x,
				Index: key,
			}
			instr.setType(typed.Elem())
			value = function.emit(instr, source)

		case *types.Pointer: // *array
			instr := &IndexAddress{
				X:     x,
				Index: key,
			}
			instr.setType(types.NewPointer(typed.Elem().Underlying().(*types.Array).Elem()))
			value = emitLoad(function, function.emit(instr, source), source)

		case *types.Slice:
			instr := &IndexAddress{
				X:     x,
				Index: key,
			}
			instr.setType(types.NewPointer(typed.Elem()))
			value = emitLoad(function, function.emit(instr, source), source)

		default:
			panic("rangeIndexed x:" + typed.String())
		}
	}
	return
}

// rangeIter emits to fn the header for a loop using
// Range/Next/Extract to iterate over map or string value x.
// tk and tv are the types of the key/value results k and v, or nil
// if the respective component is not wanted.
func (packageBuilder *builder) rangeIter(
	function *Function,
	x Value,
	tk, tv types.Type,
	source ast.Node,
) (key, value Value, loop, done *BasicBlock) {
	//
	//	it = range x
	// loop:                                   (target of continue)
	//	okv = next it                      (ok, key, value)
	//  	ok = extract okv #0
	// 	if ok goto body else done
	// body:
	// 	k = extract okv #1
	// 	v = extract okv #2
	//      ...body...
	// 	jump loop
	// done:                                   (target of break)
	//

	var ak, av types.Type
	isString := false
	if mapping, ok := typeutil.CoreType(x.Type()).(*types.Map); ok {
		ak, av = mapping.Key(), mapping.Elem()
	} else {
		isString = true
		ak, av = tInt, tRune
	}
	if tk == nil {
		ak = tInvalid
	}
	if tv == nil {
		av = tInvalid
	}

	rng := &Range{X: x}
	rng.setType(typeutil.NewIterator(types.NewTuple(
		varOk,
		newVariable("k", ak),
		newVariable("v", av),
	)))
	it := function.emit(rng, source)

	loop = function.newBasicBlock("rangeiter.loop")
	emitJump(function, loop, source)
	function.currentBlock = loop

	okv := &Next{
		Iterator: it,
		IsString: isString,
	}
	okv.setType(rng.typ.(*typeutil.Iterator).Elem())
	function.emit(okv, source)

	body := function.newBasicBlock("rangeiter.body")
	done = function.newBasicBlock("rangeiter.done")
	emitIf(function, emitExtract(function, okv, 0, source), body, done, source)
	function.currentBlock = body

	// The assignment may widen a map or string
	// key/value to a variable's interface type
	// (cases #1 and #2 of go.dev/issue/78110).
	if tk != nil {
		key = emitConv(function, emitExtract(function, okv, 1, source), tk, source)
	}
	if tv != nil {
		value = emitConv(function, emitExtract(function, okv, 2, source), tv, source)
	}
	return
}

// rangeChan emits to fn the header for a loop that receives from
// channel x until it fails.
// tk is the channel's element type, or nil if the `value` result is
// not wanted
// pos is the position of the '=' or ':=' token.
func (packageBuilder *builder) rangeChannel(
	function *Function,
	x Value,
	tk types.Type,
	source ast.Node,
) (value Value, loop, done *BasicBlock) {
	//
	// loop:                                   (target of continue)
	//      ko = <-x                           (key, ok)
	//      ok = extract ko #1
	//      if ok goto body else done
	// body:
	//      k = extract ko #0
	//      ...
	//      goto loop
	// done:                                   (target of break)

	loop = function.newBasicBlock("rangechan.loop")
	emitJump(function, loop, source)
	function.currentBlock = loop

	retv := emitReceive(
		function,
		x,
		true,
		types.NewTuple(newVariable("k", typeutil.CoreType(x.Type()).(*types.Chan).Elem()), varOk),
		source,
	)

	body := function.newBasicBlock("rangechan.body")
	done = function.newBasicBlock("rangechan.done")
	emitIf(function, emitExtract(function, retv, 1, source), body, done, source)
	function.currentBlock = body
	if tk != nil {
		value = emitExtract(function, retv, 0, source)
	}
	return
}

// rangeInt emits to fn the header for a range loop with an integer operand.
// tk is the key value's type, or nil if the `value` result is not wanted.
// pos is the position of the "for" token.
func (packageBuilder *builder) rangeInt(
	function *Function,
	x Value,
	tk types.Type,
	source ast.Node,
) (value Value, loop, done *BasicBlock) {
	//
	//     iter = 0
	//     if 0 < x goto body else done
	// loop:                                   (target of continue)
	//     iter++
	//     if iter < x goto body else done
	// body:
	//     k = x
	//     ...body...
	//     jump loop
	// done:                                   (target of break)

	if basic, ok := x.Type().(*types.Basic); ok && basic.Info()&types.IsUntyped != 0 {
		x = emitConv(function, x, tInt, source)
	}

	counterType := x.Type()
	iter := emitLocal(function, counterType, source, "rangeint.iter")
	// x may be unsigned. Avoid initializing x to -1.

	body := function.newBasicBlock("rangeint.body")
	done = function.newBasicBlock("rangeint.done")
	emitIf(function, emitCompare(function, token.LSS, zeroConstant(counterType, source), x, source), body, done, source)

	loop = function.newBasicBlock("rangeint.loop")
	function.currentBlock = loop

	incr := &BinaryOperation{
		Operator: token.ADD,
		X:        emitLoad(function, iter, source),
		Y:        emitConv(function, intConstant(1, source), counterType, source),
	}
	incr.setType(counterType)
	emitStore(function, iter, function.emit(incr, source), source)
	emitIf(function, emitCompare(function, token.LSS, incr, x, source), body, done, source)
	function.currentBlock = body

	if tk != nil {
		// Integer types (int, uint8, etc.) are named and
		// we know that k is assignable to x when tk != nil.
		// This implies tk and T are identical so no conversion is needed.
		value = emitLoad(function, iter, source)
	}

	return
}

// rangeStmt emits to fn code for the range statement `stmt`, optionally
// labelled by label.
func (packageBuilder *builder) rangeStmt(function *Function, stmt *ast.RangeStmt, label *lblock, source ast.Node) {
	var tk, tv types.Type
	if stmt.Key != nil && !isBlankIdent(stmt.Key) {
		tk = function.typeOf(stmt.Key)
	}
	if stmt.Value != nil && !isBlankIdent(stmt.Value) {
		tv = function.typeOf(stmt.Value)
	}

	// create locals for s.Key and s.Value
	createVariables := func() {
		// Unlike a short variable declaration, a RangeStmt
		// using := never redeclares an existing variable; it
		// always creates a new one.
		if tk != nil {
			id := stmt.Key.(*ast.Ident)
			emitLocalVariable(function, identVariable(function, id), id)
		}
		if tv != nil {
			id := stmt.Value.(*ast.Ident)
			emitLocalVariable(function, identVariable(function, id), id)
		}
	}

	afterGo122 := goVersionAtLeast(function.goversion, "go1.22")
	if stmt.Tok == token.DEFINE && !afterGo122 {
		// pre-go1.22: If iteration variables are defined (:=), this
		// occurs once outside the loop.
		createVariables()
	}

	x := packageBuilder.expr(function, stmt.X)

	var key, value Value
	var loop, done *BasicBlock
	switch rt := typeutil.CoreType(x.Type()).(type) {
	case *types.Slice, *types.Array, *types.Pointer: // *array
		key, value, loop, done = packageBuilder.rangeIndexed(function, x, tv, source)

	case *types.Chan:
		key, loop, done = packageBuilder.rangeChannel(function, x, tk, source)

	case *types.Map:
		key, value, loop, done = packageBuilder.rangeIter(function, x, tk, tv, source)

	case *types.Basic:
		switch {
		case rt.Info()&types.IsString != 0:
			key, value, loop, done = packageBuilder.rangeIter(function, x, tk, tv, source)

		case rt.Info()&types.IsInteger != 0:
			key, loop, done = packageBuilder.rangeInt(function, x, tk, source)

		default:
			panic("Cannot range over basic type: " + rt.String())
		}

	case *types.Signature:
		// Special case rewrite (fn.goversion >= go1.23):
		//      for x := range f { ... }
		// into
		//      f(func(x T) bool { ... })
		packageBuilder.rangeFunc(function, x, stmt, label)
		return

	default:
		panic("Cannot range over: " + rt.String())
	}

	if stmt.Tok == token.DEFINE && afterGo122 {
		// go1.22: If iteration variables are defined (:=), this occurs inside the loop.
		createVariables()
	}

	// Evaluate both LHS expressions before we update either.
	var kl, vl lvalue
	if tk != nil {
		kl = packageBuilder.addr(function, stmt.Key, false) // non-escaping
	}
	if tv != nil {
		vl = packageBuilder.addr(function, stmt.Value, false) // non-escaping
	}
	if tk != nil {
		kl.store(function, key, stmt)
	}
	if tv != nil {
		vl.store(function, value, stmt)
	}

	if label != nil {
		label._break = done
		label._continue = loop
	}

	function.targets = &targets{
		tail:      function.targets,
		_break:    done,
		_continue: loop,
	}
	packageBuilder.stmt(function, stmt.Body)
	function.targets = function.targets.tail
	emitJump(function, loop, source) // back-edge
	function.currentBlock = done
}

// rangeFunc emits to fn code for the range-over-func rng.Body of the iterator
// function x, optionally labelled by label. It creates a new anonymous function
// yield for rng and builds the function.
func (packageBuilder *builder) rangeFunc(function *Function, x Value, rng *ast.RangeStmt, label *lblock) {
	// Consider the SSA code for the outermost range-over-func in fn:
	//
	//   func fn(...) (ret R) {
	//     ...
	//     for k, v = range x {
	//           ...
	//     }
	//     ...
	//   }
	//
	// The code emitted into fn will look something like this.
	//
	// loop:
	//     jump := READY
	//     y := make closure yield [ret, deferstack, jump, k, v]
	//     x(y)
	//     switch jump {
	//        [see resuming execution]
	//     }
	//     goto done
	// done:
	//     ...
	//
	// where yield is a new synthetic yield function:
	//
	// func yield(_k tk, _v tv) bool
	//   free variables: [ret, stack, jump, k, v]
	// {
	//    entry:
	//      if jump != READY then goto invalid else valid
	//    invalid:
	//      panic("iterator called when it is not in a ready state")
	//    valid:
	//      jump = BUSY
	//      k = _k
	//      v = _v
	//    ...
	//    cont:
	//      jump = READY
	//      return true
	// }
	//
	// Yield state:
	//
	// Each range loop has an associated jump variable that records
	// the state of the iterator. A yield function is initially
	// in a READY (0) and callable state.  If the yield function is called
	// and is not in READY state, it panics. When it is called in a callable
	// state, it becomes BUSY. When execution reaches the end of the body
	// of the loop (or a continue statement targeting the loop is executed),
	// the yield function returns true and resumes being in a READY state.
	// After the iterator function x(y) returns, then if the yield function
	// is in a READY state, the yield enters the DONE state.
	//
	// Each lowered control statement (break X, continue X, goto Z, or return)
	// that exits the loop sets the variable to a unique positive EXIT value,
	// before returning false from the yield function.
	//
	// If the yield function returns abruptly due to a panic or GoExit,
	// it remains in a BUSY state. The generated code asserts that, after
	// the iterator call x(y) returns normally, the jump variable state
	// is DONE.
	//
	// Resuming execution:
	//
	// The code generated for the range statement checks the jump
	// variable to determine how to resume execution.
	//
	//    switch jump {
	//    case BUSY:  panic("...")
	//    case DONE:  goto done
	//    case READY: state = DONE; goto done
	//    case 123:   ... // action for exit 123.
	//    case 456:   ... // action for exit 456.
	//    ...
	//    }
	//
	// Forward goto statements within a yield are jumps to labels that
	// have not yet been traversed in fn. They may be in the Body of the
	// function. What we emit for these is:
	//
	//    goto target
	//  target:
	//    ...
	//
	// We leave an unresolved exit in yield.exits to check at the end
	// of building yield if it encountered target in the body. If it
	// encountered target, no additional work is required. Otherwise,
	// the yield emits a new early exit in the basic block for target.
	// We expect that blockopt will fuse the early exit into the case
	// block later. The unresolved exit is then added to yield.parent.exits.

	loop := function.newBasicBlock("rangefunc.loop")
	done := function.newBasicBlock("rangefunc.done")

	// These are targets within y.
	function.targets = &targets{
		tail:   function.targets,
		_break: done,
		// _continue is within y.
	}
	if label != nil {
		label._break = done
		// _continue is within y
	}

	emitJump(function, loop, nil)
	function.currentBlock = loop

	// loop:
	//     jump := READY

	anonIndex := len(function.AnonymousFunctions)

	jump := newVariable(fmt.Sprintf("jump$%d", anonIndex+1), tInt)
	emitLocalVariable(function, jump, nil) // zero value is READY

	xsig := typeutil.CoreType(x.Type()).(*types.Signature)
	ysig := typeutil.CoreType(xsig.Params().At(0).Type()).(*types.Signature)

	/* synthetic yield function for body of range-over-func loop */
	y := &Function{
		name:           fmt.Sprintf("%s$%d", function.Name(), anonIndex+1),
		Signature:      ysig,
		Synthetic:      "range-over-func yield",
		position:       rng.Range,
		parent:         function,
		anonIndex:      int32(len(function.AnonymousFunctions)),
		Package:        function.Package,
		Program:        function.Program,
		syntax:         rng,
		info:           function.info,
		build:          (*builder).buildYieldFunc,
		topLevelOrigin: nil,
		typeparams:     function.typeparams,
		typeargs:       function.typeargs,
	}
	y.goversion = function.goversion
	y.jump = jump
	y.deferstack = function.deferstack
	y.returnVariables = function.returnVariables // use the parent's return variables
	y.uniq = function.uniq                       // start from parent's unique values

	// If the RangeStmt has a label, this is how it is passed to buildYieldFunc.
	if label != nil {
		y.lblocks = map[*types.Label]*lblock{label.label: nil}
	}
	function.AnonymousFunctions = append(function.AnonymousFunctions, y)

	// Build y immediately. It may:
	// * cause fn's locals to escape, and
	// * create new exit nodes in exits.
	// (y is not marked 'built' until the end of the enclosing FuncDecl.)
	unresolved := len(function.exits)
	y.build(packageBuilder, y)
	function.uniq = y.uniq // resume after y's unique values

	// Emit the call of y.
	//   c := MakeClosure y
	//   x(c)
	closure := &MakeClosure{Function: y}
	closure.setType(ysig)
	closure.comment = "yield"
	for _, fv := range y.FreeVariables {
		closure.Bindings = append(closure.Bindings, fv.outer)
		fv.outer = nil
	}
	function.emit(closure, nil)
	call := Call{
		Call: CallCommon{
			Value:     x,
			Arguments: []Value{closure},
		},
	}
	call.setType(xsig.Results())
	function.emit(&call, nil)

	exits := function.exits[unresolved:]
	packageBuilder.buildYieldResume(function, jump, exits, done)

	function.currentBlock = done
	// pop the stack for the range-over-func
	function.targets = function.targets.tail
}

// buildYieldResume emits to fn code for how to resume execution once a call to
// the iterator function over the yield function returns x(y). It does this by building
// a switch over the value of jump for when it is READY, BUSY, or EXIT(id).
func (packageBuilder *builder) buildYieldResume(function *Function, jump *types.Var, exits []*exit, done *BasicBlock) {
	//    v := *jump
	//    switch v {
	//    case BUSY:    panic("...")
	//    case READY:   jump = DONE; goto done
	//    case EXIT(a): ...
	//    case EXIT(b): ...
	//    ...
	//    }
	load := emitLoad(function, function.lookup(jump, false), nil)

	entry := function.currentBlock
	bodies := make([]*BasicBlock, 2, 2+len(exits))
	bodies[0] = function.newBasicBlock("rangefunc.resume.busy")
	bodies[1] = function.newBasicBlock("rangefunc.resume.ready")

	conds := make([]Value, 2, 2+len(exits))
	conds[0] = jBusy
	conds[1] = jReady

	function.currentBlock = bodies[0]
	function.emit(
		&Panic{
			X: emitConv(function, jDroppedPanic, tEface, nil),
		},
		nil,
	)

	function.currentBlock = bodies[1]
	storeVariable(function, jump, jDone, nil)
	emitJump(function, done, nil)

	for _, exit := range exits {
		body := function.newBasicBlock(fmt.Sprintf("rangefunc.resume.exit.%d", exit.id))
		bodies = append(bodies, body)
		id := intConstant(exit.id, nil)
		conds = append(conds, id)

		function.currentBlock = body
		switch {
		case exit.label != nil: // forward goto?
			// case EXIT(id): goto lb // label
			lb := function.lblockOf(exit.label)
			// Do not mark lb as resolved.
			// If fn does not contain label, lb remains unresolved and
			// fn must itself be a range-over-func function. lb will be:
			//   lb:
			//     fn.jump = id
			//     return false
			emitJump(function, lb._goto, exit.source)

		case exit.to != function: // e jumps to an ancestor of fn?
			// case EXIT(id): { fn.jump = id; return false }
			// fn is a range-over-func function.

			storeVariable(function, function.jump, id, exit.source)
			vFalse := NewConstant(constant.MakeBool(false), tBool, exit.source)
			function.emit(&Return{Results: []Value{vFalse}}, exit.source)

		case exit.block == nil && exit.label == nil: // return from fn?
			// case EXIT(id): { return ... }
			function.emit(new(RunDefers), exit.source)
			results := make([]Value, len(function.results))
			for index, result := range function.results {
				results[index] = emitLoad(function, result, exit.source)
			}
			function.emit(&Return{Results: results}, exit.source)

		case exit.block != nil:
			// case EXIT(id): goto block
			emitJump(function, exit.block, exit.source)

		default:
			panic("unreachable")
		}

	}

	function.currentBlock = entry
	// Note that this switch does not have an implicit default case. This wouldn't be
	// valid for a user-provided switch statement, but for range-over-func we know all
	// possible values and we can avoid the impossible branch.
	swtch := &ConstantSwitch{
		Tag:        load,
		Conditions: conds,
	}
	function.emit(swtch, nil)
	for _, body := range bodies {
		addEdge(entry, body)
	}
}

// stmt lowers statement s to IR form, emitting code to fn.
func (packageBuilder *builder) stmt(function *Function, statement ast.Stmt) {
	// The label of the current statement.  If non-nil, its _goto
	// target is always set; its _break and _continue are set only
	// within the body of switch/typeswitch/select/for/range.
	// It is effectively an additional default-nil parameter of stmt().
	var label *lblock
start:
	switch typed := statement.(type) {
	case *ast.EmptyStmt:
		// ignore.  (Usually removed by gofmt.)

	case *ast.DeclStmt: // Con, Var or Typ
		decl := typed.Decl.(*ast.GenDecl)
		if decl.Tok == token.VAR {
			for _, spec := range decl.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					packageBuilder.localValueSpec(function, vs)
				}
			}
		}

	case *ast.LabeledStmt:
		if typed.Label.Name == "_" {
			// Blank labels can't be the target of a goto, break,
			// or continue statement, so we don't need a new block.
			statement = typed.Stmt
			goto start
		}
		label = function.lblockOf(function.label(typed.Label))
		label.resolved = true
		emitJump(function, label._goto, typed)
		function.currentBlock = label._goto
		statement = typed.Stmt
		goto start // effectively: tailcall stmt(fn, s.Stmt, label)

	case *ast.ExprStmt:
		packageBuilder.expr(function, typed.X)

	case *ast.SendStmt:
		instr := &Send{
			Channel: packageBuilder.expr(function, typed.Chan),
			X: emitConv(function, packageBuilder.expr(function, typed.Value),
				typeutil.CoreType(function.typeOf(typed.Chan)).(*types.Chan).Elem(), typed),
		}
		function.emit(instr, typed)

	case *ast.IncDecStmt:
		op := token.ADD
		if typed.Tok == token.DEC {
			op = token.SUB
		}
		loc := packageBuilder.addr(function, typed.X, false)
		packageBuilder.assignOp(function, loc, NewConstant(constant.MakeInt64(1), loc.typ(), typed), op, typed)

	case *ast.AssignStmt:
		switch typed.Tok {
		case token.ASSIGN, token.DEFINE:
			packageBuilder.assignStmt(function, typed.Lhs, typed.Rhs, typed.Tok == token.DEFINE, statement)

		default: // +=, etc.
			op := typed.Tok + token.ADD - token.ADD_ASSIGN
			packageBuilder.assignOp(function, packageBuilder.addr(function, typed.Lhs[0], false), packageBuilder.expr(function, typed.Rhs[0]), op, typed)
		}

	case *ast.GoStmt:
		// The "intrinsics" new/make/len/cap are forbidden here.
		// panic is treated like an ordinary function call.
		goInstruction := Go{}
		packageBuilder.setCall(function, typed.Call, &goInstruction.Call)
		function.emit(&goInstruction, typed)

	case *ast.DeferStmt:
		// The "intrinsics" new/make/len/cap are forbidden here.
		// panic is treated like an ordinary function call.
		deferstack := emitLoad(function, function.lookup(function.deferstack, false), typed)
		deferInstruction := Defer{DeferStack: deferstack}
		packageBuilder.setCall(function, typed.Call, &deferInstruction.Call)
		function.emit(&deferInstruction, typed)

		// A deferred call can cause recovery from panic,
		// and control resumes at the Recover block.
		createRecoverBlock(function.source)

	case *ast.ReturnStmt:
		packageBuilder.returnStmt(function, typed)

	case *ast.BranchStmt:
		packageBuilder.branchStmt(function, typed)

	case *ast.BlockStmt:
		packageBuilder.stmtList(function, typed.List)

	case *ast.IfStmt:
		if typed.Init != nil {
			packageBuilder.stmt(function, typed.Init)
		}
		then := function.newBasicBlock("if.then")
		done := function.newBasicBlock("if.done")
		els := done
		if typed.Else != nil {
			els = function.newBasicBlock("if.else")
		}
		instr := packageBuilder.cond(function, typed.Cond, then, els)
		instr.source = typed
		function.currentBlock = then
		packageBuilder.stmt(function, typed.Body)
		emitJump(function, done, typed)

		if typed.Else != nil {
			function.currentBlock = els
			packageBuilder.stmt(function, typed.Else)
			emitJump(function, done, typed)
		}

		function.currentBlock = done

	case *ast.SwitchStmt:
		packageBuilder.switchStmt(function, typed, label)

	case *ast.TypeSwitchStmt:
		packageBuilder.typeSwitchStmt(function, typed, label)

	case *ast.SelectStmt:
		if packageBuilder.selectStmt(function, typed, label) {
			// the select has no cases, it blocks forever
			function.currentBlock = function.newBasicBlock("unreachable")
		}

	case *ast.ForStmt:
		packageBuilder.forStmt(function, typed, label)

	case *ast.RangeStmt:
		packageBuilder.rangeStmt(function, typed, label, typed)

	default:
		panic(fmt.Sprintf("unexpected statement kind: %T", typed))
	}
}

func (packageBuilder *builder) branchStmt(function *Function, stmt *ast.BranchStmt) {
	var block *BasicBlock
	if stmt.Label == nil {
		block = targetedBlock(function, stmt.Tok)
	} else {
		target := function.label(stmt.Label)
		block = labelledBlock(function, target, stmt.Tok)
		if block == nil { // forward goto
			lb := function.lblockOf(target)
			block = lb._goto // jump to lb._goto
			if function.jump != nil {
				// fn is a range-over-func and the goto may exit fn.
				// Create an exit and resolve it at the end of
				// builder.buildYieldFunc.
				labelExit(function, target, stmt)
			}
		}
	}
	to := block.parent

	if to == function {
		emitJump(function, block, stmt)
	} else { // break outside of fn.
		// fn must be a range-over-func
		exit := blockExit(function, block, stmt)
		id := intConstant(exit.id, stmt)
		storeVariable(function, function.jump, id, stmt)
		vFalse := NewConstant(constant.MakeBool(false), tBool, stmt)
		function.emit(&Return{Results: []Value{vFalse}}, exit.source)
	}
	function.currentBlock = function.newBasicBlock("unreachable")
}

func (packageBuilder *builder) returnStmt(function *Function, stmt *ast.ReturnStmt) {
	// TODO(dh): we could emit tighter position information by
	// using the ith returned expression

	var results []Value

	sig := function.source.Signature // signature of the enclosing source function

	// Convert return operands to result type.
	if len(stmt.Results) == 1 && sig.Results().Len() > 1 {
		// Return of one expression in a multi-valued function.
		tuple := packageBuilder.exprN(function, stmt.Results[0])
		ttuple := tuple.Type().(*types.Tuple)
		for index, length := 0, ttuple.Len(); index < length; index++ {
			results = append(results,
				emitConv(function, emitExtract(function, tuple, index, stmt),
					sig.Results().At(index).Type(), stmt))
		}
	} else {
		// 1:1 return, or no-arg return in non-void function.
		for index, result := range stmt.Results {
			value := emitConv(function, packageBuilder.expr(function, result), sig.Results().At(index).Type(), stmt)
			results = append(results, value)
		}
	}

	// Store the results.
	for index, value := range results {
		var result Value // fn.sourceFn.result[i] conceptually
		if function == function.source {
			result = function.results[index]
		} else { // lookup needed?
			result = function.lookup(function.returnVariables[index], false)
		}
		emitStore(function, result, value, stmt)
	}

	if function.jump != nil {
		// Return from body of a range-over-func.
		// The return statement is syntactically within the loop,
		// but the generated code is in the 'switch jump {...}' after it.
		exit := returnExit(function, stmt)
		id := intConstant(exit.id, exit.source)
		storeVariable(function, function.jump, id, exit.source)
		vFalse := NewConstant(constant.MakeBool(false), tBool, exit.source)
		function.emit(&Return{Results: []Value{vFalse}}, exit.source)
		function.currentBlock = function.newBasicBlock("unreachable")
		return
	}

	// Run function calls deferred in this
	// function when explicitly returning from it.
	function.emit(new(RunDefers), stmt)
	// Reload (potentially) named result variables to form the result tuple.
	results = results[:0]
	for _, nr := range function.results {
		results = append(results, emitLoad(function, nr, stmt))
	}

	function.emit(&Return{Results: results}, stmt)
	function.currentBlock = function.newBasicBlock("unreachable")
}

// A buildFunc is a strategy for building the SSA body for a function.
type buildFunc = func(*builder, *Function)

// iterate causes all created but unbuilt functions to be built. As
// this may create new methods, the process is iterated until it
// converges.
//
// Waits for any dependencies to finish building.
func (packageBuilder *builder) iterate() {
	for ; packageBuilder.finished < len(packageBuilder.fns); packageBuilder.finished++ {
		function := packageBuilder.fns[packageBuilder.finished]
		packageBuilder.buildFunction(function)
	}

	packageBuilder.buildshared.markDone()
	packageBuilder.buildshared.wait()
}

// buildFunction builds IR code for the body of function fn.  Idempotent.
func (packageBuilder *builder) buildFunction(function *Function) {
	if function.build != nil {
		assert(function.parent == nil, "anonymous functions should not be built by buildFunction()")

		function.build(packageBuilder, function)
		function.done()
	}
}

// buildParamsOnly builds fn.Parameters from fn.Signature, but does not build fn.Body.
func (packageBuilder *builder) buildParamsOnly(function *Function) {
	// For external (C, asm) functions or functions loaded from
	// export data, we must set fn.Parameters even though there is no
	// body code to reference them.
	if recv := function.Signature.Recv(); recv != nil {
		// TODO(dh): should we synthesize a node so we have position info?
		function.addParamVariable(recv, nil)
	}
	parameters := function.Signature.Params()
	for index, length := 0, parameters.Len(); index < length; index++ {
		// TODO(dh): should we synthesize a node so we have position info?
		function.addParamVariable(parameters.At(index), nil)
	}
}

// buildFromSyntax builds fn.Body from fn.syntax, which must be non-nil.
func (packageBuilder *builder) buildFromSyntax(function *Function) {
	var (
		recvField *ast.FieldList
		body      *ast.BlockStmt
		functype  *ast.FuncType
	)
	switch syntax := function.syntax.(type) {
	case *ast.FuncDecl:
		functype = syntax.Type
		recvField = syntax.Recv
		body = syntax.Body
		if body == nil {
			packageBuilder.buildParamsOnly(function) // no body (non-Go function)
			return
		}
	case *ast.FuncLit:
		functype = syntax.Type
		body = syntax.Body
	case nil:
		panic("no syntax")
	default:
		panic(syntax) // unexpected syntax
	}
	function.source = function
	function.startBody()
	function.createSyntacticParameters(recvField, functype)
	function.createDeferStack()
	packageBuilder.stmt(function, body)
	if cb := function.currentBlock; cb != nil &&
		(cb == function.Blocks[0] || cb == function.Recover || cb.Predecessors != nil) {
		// Control fell off the end of the function's body block.
		//
		// Block optimizations eliminate the current block, if
		// unreachable.  It is a builder invariant that
		// if this no-arg return is ill-typed for
		// fn.Signature.Results, this block must be
		// unreachable.  The sanity checker checks this.
		function.emit(new(RunDefers), nil)
		function.emit(new(Return), nil)
	}
	function.finishBody()
}

// buildYieldFunc builds the body of the yield function created
// from a range-over-func *ast.RangeStmt.
func (packageBuilder *builder) buildYieldFunc(function *Function) {
	// See builder.rangeFunc for detailed documentation on how fn is set up.
	//
	// In pseudo-Go this roughly builds:
	// func yield(_k tk, _v tv) bool {
	// 	   if jump != READY { panic("yield function called after range loop exit") }
	//     jump = BUSY
	//     k, v = _k, _v // assign the iterator variable (if needed)
	//     ... // rng.Body
	//   continue:
	//     jump = READY
	//     return true
	// }
	stmt := function.syntax.(*ast.RangeStmt)
	function.source = function.parent.source
	function.startBody()
	parameters := function.Signature.Params()
	for variable := range parameters.Variables() {
		function.addParamVariable(variable, nil)
	}

	// Initial targets
	ycont := function.newBasicBlock("yield-continue")
	// lblocks is either {} or is {label: nil} where label is the label of syntax.
	for label := range function.lblocks {
		function.lblocks[label] = &lblock{
			label:     label,
			resolved:  true,
			_goto:     ycont,
			_continue: ycont,
			// `break label` statement targets fn.parent.targets._break
		}
	}
	function.targets = &targets{
		tail:      function.targets,
		_continue: ycont,
		// `break` statement targets fn.parent.targets._break.
	}

	// continue:
	//   jump = READY
	//   return true
	saved := function.currentBlock
	function.currentBlock = ycont
	storeVariable(function, function.jump, jReady, stmt.Body)
	// A yield function's own deferstack is always empty, so rundefers is not needed.
	function.emit(&Return{Results: []Value{vTrue}}, nil)

	// Emit header:
	//
	//   if jump != READY { panic("yield iterator accessed after exit") }
	//   jump = BUSY
	//   k, v = _k, _v
	function.currentBlock = saved
	yloop := function.newBasicBlock("yield-loop")
	invalid := function.newBasicBlock("yield-invalid")

	jumpValue := emitLoad(function, function.lookup(function.jump, true), nil)
	emitIf(function, emitCompare(function, token.EQL, jumpValue, jReady, nil), yloop, invalid, nil)
	function.currentBlock = invalid
	function.emit(
		&Panic{
			X: emitConv(function, jLateYield, tEface, nil),
		},
		nil,
	)

	function.currentBlock = yloop
	storeVariable(function, function.jump, jBusy, stmt.Body)

	// Initialize k and v from params.
	var tk, tv types.Type
	if stmt.Key != nil && !isBlankIdent(stmt.Key) {
		tk = function.typeOf(stmt.Key) // fn.parent.typeOf is identical
	}
	if stmt.Value != nil && !isBlankIdent(stmt.Value) {
		tv = function.typeOf(stmt.Value)
	}
	if stmt.Tok == token.DEFINE {
		if tk != nil {
			emitLocalVariable(function, identVariable(function, stmt.Key.(*ast.Ident)), stmt.Key)
		}
		if tv != nil {
			emitLocalVariable(function, identVariable(function, stmt.Value.(*ast.Ident)), stmt.Value)
		}
	}
	var key, value Value
	if len(function.Parameters) > 0 {
		key = function.Parameters[0]
	}
	if len(function.Parameters) > 1 {
		value = function.Parameters[1]
	}
	var kl, vl lvalue
	if tk != nil {
		kl = packageBuilder.addr(function, stmt.Key, false) // non-escaping
	}
	if tv != nil {
		vl = packageBuilder.addr(function, stmt.Value, false) // non-escaping
	}
	if tk != nil {
		kl.store(function, key, stmt.Key)
	}
	if tv != nil {
		vl.store(function, value, stmt.Value)
	}

	// Build the body of the range loop.
	packageBuilder.stmt(function, stmt.Body)
	if cb := function.currentBlock; cb != nil &&
		(cb == function.Blocks[0] || cb == function.Recover || cb.Predecessors != nil) {
		// Control fell off the end of the function's body block.
		// Block optimizations eliminate the current block, if
		// unreachable.
		emitJump(function, ycont, nil)
	}
	// pop the stack for the yield function
	function.targets = function.targets.tail

	// Clean up exits and promote any unresolved exits to fn.parent.
	for _, exit := range function.exits {
		if exit.label != nil {
			lb := function.lblocks[exit.label]
			if lb.resolved {
				// label was resolved. Do not turn lb into an exit.
				// e does not need to be handled by the parent.
				continue
			}

			// _goto becomes an exit.
			//   _goto:
			//     jump = id
			//     return false
			function.currentBlock = lb._goto
			id := intConstant(exit.id, exit.source)
			storeVariable(function, function.jump, id, exit.source)
			vFalse := NewConstant(constant.MakeBool(false), tBool, exit.source)
			function.emit(&Return{Results: []Value{vFalse}}, exit.source)
		}

		if exit.to != function { // e needs to be handled by the parent too.
			function.parent.exits = append(function.parent.exits, exit)
		}
	}

	function.finishBody()
}

// cpuLimit is a counting semaphore to limit CPU parallelism.
var cpuLimit = make(chan unit, runtime.GOMAXPROCS(0))

// buildPackageInit builds fn.Body for the synthetic package initializer.
func (packageBuilder *builder) buildPackageInit(init *Function) {
	irPackage := init.Package
	init.startBody()

	// Make init() skip if package is already initialized.
	initguard := irPackage.Variable("init$guard")
	doinit := init.newBasicBlock("init.start")
	done := init.newBasicBlock("init.done")
	emitIf(init, emitLoad(init, initguard, nil), done, doinit, nil)
	init.currentBlock = doinit
	emitStore(init, initguard, vTrue, nil)

	// Call the init() function of each package we import.
	for _, importsPackage := range irPackage.Types.Imports() {
		prereq := irPackage.Program.packages[importsPackage]
		if prereq == nil {
			panic(fmt.Sprintf(
				"Package(%q).Build(): unsatisfied import: Program.CreatePackage(%q) was not called",
				irPackage.Types.Path(),
				importsPackage.Path(),
			))
		}
		var call Call
		call.Call.Value = prereq.init
		call.setType(types.NewTuple())
		init.emit(&call, nil)
	}

	// Initialize package-level vars in correct order.
	if len(irPackage.info.InitOrder) > 0 && len(irPackage.files) == 0 {
		panic("no source files provided for package. cannot initialize globals")
	}

	for _, varinit := range irPackage.info.InitOrder {
		// Initializers for global vars are evaluated in dependency
		// order, but may come from arbitrary files of the package
		// with different versions, so we transiently update
		// init.goversion for each one. (Since init is a synthetic
		// function it has no syntax of its own that needs a version.)
		init.goversion = irPackage.initVersion[varinit.Rhs]
		if len(varinit.Lhs) == 1 {
			// 1:1 initialization: var x, y = a(), b()
			var lval lvalue
			if variable := varinit.Lhs[0]; variable.Name() != "_" {
				lval = &address{addr: irPackage.values[variable].(*Global)}
			} else {
				lval = blank{}
			}
			// TODO(dh): do emit position information
			packageBuilder.assign(init, lval, varinit.Rhs, true, nil, nil)
		} else {
			// n:1 initialization: var x, y :=  f()
			tuple := packageBuilder.exprN(init, varinit.Rhs)
			for index, variable := range varinit.Lhs {
				if variable.Name() == "_" {
					continue
				}
				emitStore(init, irPackage.values[variable].(*Global), emitExtract(init, tuple, index, nil), nil)
			}
		}
	}

	// The rest of the init function is synthetic:
	// no syntax, info, goversion.
	init.info = nil
	init.goversion = ""

	// Call all of the declared init() functions in source order.
	for _, file := range irPackage.files {
		for _, decl := range file.Decls {
			if decl, ok := decl.(*ast.FuncDecl); ok {
				id := decl.Name
				if !isBlankIdent(id) && id.Name == "init" && decl.Recv == nil {
					declaredInit := irPackage.values[irPackage.info.Defs[id]].(*Function)
					var call Call
					call.Call.Value = declaredInit
					call.setType(types.NewTuple())
					irPackage.init.emit(&call, nil)
				}
			}
		}
	}

	// Finish up init().
	emitJump(init, done, nil)
	init.currentBlock = done
	init.emit(new(Return), nil)
	init.finishBody()
}
