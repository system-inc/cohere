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

	"honnef.co/go/tools/go/types/typeutil"
)

// rangeIndexed emits to fn the header for an integer-indexed loop
// over array, *array or slice value x.
// The v result is defined only if tv is non-nil.
// forPos is the position of the "for" token.
//
// Mutates: function gains the loop header's blocks, and its current block moves into the body
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
			array, isArray := typed.Elem().Underlying().(*types.Array)
			if !isArray {
				// Invariant: go/types accepts ranging over a pointer only when it points to an array.
				panic("rangeIndexed over a pointer to " + typed.Elem().String() + ", not to an array")
			}
			instr.setType(types.NewPointer(array.Elem()))
			value = emitLoad(function, function.emit(instr, source), source)

		case *types.Slice:
			instr := &IndexAddress{
				X:     x,
				Index: key,
			}
			instr.setType(types.NewPointer(typed.Elem()))
			value = emitLoad(function, function.emit(instr, source), source)

		default:
			// Invariant: rangeStmt calls rangeIndexed only for slice, array and pointer core types.
			panic("rangeIndexed x:" + typed.String())
		}
	}
	return
}

// rangeIter emits to fn the header for a loop using
// Range/Next/Extract to iterate over map or string value x.
// tk and tv are the types of the key/value results k and v, or nil
// if the respective component is not wanted.
//
// Mutates: function gains the loop header's blocks, and its current block moves into the body
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

	var ak types.Type
	var av types.Type
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
	iterator, isIterator := rng.typ.(*typeutil.Iterator)
	if !isIterator {
		// Invariant: rng's type was set to an Iterator above.
		panic(fmt.Sprintf("range has type %s, not an iterator", rng.typ))
	}
	okv.setType(iterator.Elem())
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
//
// Mutates: function gains the loop header's blocks, and its current block moves into the body
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

	channelType, isChannel := typeutil.CoreType(x.Type()).(*types.Chan)
	if !isChannel {
		// Invariant: rangeStmt calls rangeChannel only for a channel core type.
		panic(fmt.Sprintf("rangeChannel over %s, which has no channel core type", x.Type()))
	}
	retv := emitReceive(
		function,
		x,
		true,
		types.NewTuple(newVariable("k", channelType.Elem()), varOk),
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
//
// Mutates: function gains the loop header's blocks, and its current block moves into the body
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

	limit := x
	if basic, ok := x.Type().(*types.Basic); ok && basic.Info()&types.IsUntyped != 0 {
		limit = emitConv(function, x, tInt, source)
	}

	counterType := limit.Type()
	iter := emitLocal(function, counterType, source, "rangeint.iter")
	// x may be unsigned. Avoid initializing x to -1.

	body := function.newBasicBlock("rangeint.body")
	done = function.newBasicBlock("rangeint.done")
	emitIf(
		function,
		emitCompare(function, token.LSS, zeroConstant(counterType, source), limit, source),
		body,
		done,
		source,
	)

	loop = function.newBasicBlock("rangeint.loop")
	function.currentBlock = loop

	incr := &BinaryOperation{
		Operator: token.ADD,
		X:        emitLoad(function, iter, source),
		Y:        emitConv(function, intConstant(1, source), counterType, source),
	}
	incr.setType(counterType)
	emitStore(function, iter, function.emit(incr, source), source)
	emitIf(function, emitCompare(function, token.LSS, incr, limit, source), body, done, source)
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
//
// Mutates: function gains the blocks of the loop, and its targets and current block follow them
// Mutates: label takes the loop's done and loop blocks as its break and continue targets
func (packageBuilder *builder) rangeStmt(function *Function, stmt *ast.RangeStmt, label *lblock, source ast.Node) {
	var tk types.Type
	var tv types.Type
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
			id, isIdent := stmt.Key.(*ast.Ident)
			if !isIdent {
				// Invariant: createVariables runs only for a range clause with :=, and go/types accepts only
				// identifiers on its left.
				panic(fmt.Sprintf("range key %T is not an identifier", stmt.Key))
			}
			emitLocalVariable(function, identVariable(function, id), id)
		}
		if tv != nil {
			id, isIdent := stmt.Value.(*ast.Ident)
			if !isIdent {
				// Invariant: createVariables runs only for a range clause with :=, and go/types accepts only
				// identifiers on its left.
				panic(fmt.Sprintf("range value %T is not an identifier", stmt.Value))
			}
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

	var key Value
	var value Value
	var loop *BasicBlock
	var done *BasicBlock
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
			// Invariant: go/types accepts ranging over a basic type only for strings and integers.
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
		// Invariant: go/types accepts ranging only over slices, arrays, pointers to arrays, channels, maps, strings,
		// integers and functions, each handled above.
		panic("Cannot range over: " + rt.String())
	}

	if stmt.Tok == token.DEFINE && afterGo122 {
		// go1.22: If iteration variables are defined (:=), this occurs inside the loop.
		createVariables()
	}

	// Evaluate both LHS expressions before we update either.
	var kl lvalue
	var vl lvalue
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
//
// Mutates: function gains the loop's blocks and its yield function, and its targets, unique values and current
// block follow them
// Mutates: label takes the loop's done block as its break target
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
	//     jump := jReady
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
	//      if jump != jReady then goto invalid else valid
	//    invalid:
	//      panic("iterator called when it is not in a ready state")
	//    valid:
	//      jump = jBusy
	//      k = _k
	//      v = _v
	//    ...
	//    cont:
	//      jump = jReady
	//      return true
	// }
	//
	// Yield state:
	//
	// Each range loop has an associated jump variable that records
	// the state of the iterator. A yield function is initially
	// in the ready (0) and callable state.  If the yield function is called
	// and is not in the ready state, it panics. When it is called in a callable
	// state, it becomes busy. When execution reaches the end of the body
	// of the loop (or a continue statement targeting the loop is executed),
	// the yield function returns true and resumes being in the ready state.
	// After the iterator function x(y) returns, then if the yield function
	// is in the ready state, the yield enters the done state.
	//
	// Each lowered control statement (break X, continue X, goto Z, or return)
	// that exits the loop sets the variable to a unique positive exit value,
	// before returning false from the yield function.
	//
	// If the yield function returns abruptly due to a panic or GoExit,
	// it remains in the busy state. The generated code asserts that, after
	// the iterator call x(y) returns normally, the jump variable state
	// is done.
	//
	// Resuming execution:
	//
	// The code generated for the range statement checks the jump
	// variable to determine how to resume execution.
	//
	//    switch jump {
	//    case jBusy:  panic("...")
	//    case jDone:  goto done
	//    case jReady: state = jDone; goto done
	//    case 123:    ... // action for exit 123.
	//    case 456:    ... // action for exit 456.
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
	//     jump := jReady

	anonIndex := len(function.AnonymousFunctions)

	jump := newVariable(fmt.Sprintf("jump$%d", anonIndex+1), tInt)
	emitLocalVariable(function, jump, nil) // zero value is jReady

	xsig, isSignature := typeutil.CoreType(x.Type()).(*types.Signature)
	if !isSignature {
		// Invariant: rangeStmt calls rangeFunc only for a signature core type.
		panic(fmt.Sprintf("rangeFunc over %s, which has no signature core type", x.Type()))
	}
	ysig, isYieldSignature := typeutil.CoreType(xsig.Params().At(0).Type()).(*types.Signature)
	if !isYieldSignature {
		// Invariant: go/types accepts ranging over a function only when its one parameter is a yield function.
		panic(fmt.Sprintf("rangeFunc iterator %s takes %s, not a yield function", x.Type(), xsig.Params().At(0).Type()))
	}

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
// a switch over the value of jump for when it is ready, busy, or an exit id.
//
// Mutates: function gains the resume switch and its bodies, and its current block returns to the entry
func (packageBuilder *builder) buildYieldResume(function *Function, jump *types.Var, exits []*exit, done *BasicBlock) {
	//    v := *jump
	//    switch v {
	//    case jBusy:   panic("...")
	//    case jReady:  jump = jDone; goto done
	//    case exit(a): ...
	//    case exit(b): ...
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
			// case exit(id): goto lb // label
			lb := function.lblockOf(exit.label)
			// Do not mark lb as resolved.
			// If fn does not contain label, lb remains unresolved and
			// fn must itself be a range-over-func function. lb will be:
			//   lb:
			//     fn.jump = id
			//     return false
			emitJump(function, lb._goto, exit.source)

		case exit.to != function: // e jumps to an ancestor of fn?
			// case exit(id): { fn.jump = id; return false }
			// fn is a range-over-func function.

			storeVariable(function, function.jump, id, exit.source)
			vFalse := NewConstant(constant.MakeBool(false), tBool, exit.source)
			function.emit(&Return{Results: []Value{vFalse}}, exit.source)

		case exit.block == nil && exit.label == nil: // return from fn?
			// case exit(id): { return ... }
			function.emit(new(RunDefers), exit.source)
			results := make([]Value, len(function.results))
			for index, result := range function.results {
				results[index] = emitLoad(function, result, exit.source)
			}
			function.emit(&Return{Results: results}, exit.source)

		case exit.block != nil:
			// case exit(id): goto block
			emitJump(function, exit.block, exit.source)

		default:
			// Invariant: the cases above cover every exit, one with a label, one to an ancestor, one returning,
			// which has neither label nor block, and one with a block.
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
