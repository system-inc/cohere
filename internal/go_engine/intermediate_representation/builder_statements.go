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
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/core_type"
)

// stmtList emits to fn code for all statements in list.
func (packageBuilder *builder) stmtList(function *Function, list []ast.Stmt) {
	for _, stmt := range list {
		packageBuilder.stmt(function, stmt)
	}
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
				local := emitLocalVariable(function, identVariable(function, id), id)
				if function.debugInformation() {
					emitDebugReference(function, id, local, true)
				}
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
				id, isIdent := lhs.(*ast.Ident)
				if !isIdent {
					// Invariant: go/types accepts only identifiers on the left of :=.
					panic(fmt.Sprintf("short variable declaration of %T, not an identifier", lhs))
				}
				if object, ok := function.info.Defs[id].(*types.Var); ok {
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
		emitDebugReference(function, rhss[0], tuple, false)
		for index, lval := range lvals {
			lval.store(function, emitExtract(function, tuple, index, source), source)
		}
	}
}

// switchStmt emits to fn code for the switch statement `switchStmt`, optionally
// labelled by label. A switch whose cases are all constant becomes a ConstantSwitch.
//
// Mutates: function gains the blocks of the switch, and its targets and current block follow them
// Mutates: label takes the switch's done block as its break target
func (packageBuilder *builder) switchStmt(function *Function, switchStmt *ast.SwitchStmt, label *lblock) {
	if switchStmt.Tag == nil {
		packageBuilder.switchStmtDynamic(function, switchStmt, label)
		return
	}
	dynamic := false
	for _, iclause := range switchStmt.Body.List {
		clause, isCaseClause := iclause.(*ast.CaseClause)
		if !isCaseClause {
			// Invariant: go/ast holds only case clauses in the body of an expression or type switch.
			panic(fmt.Sprintf("switch body holds %T, not a case clause", iclause))
		}
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
		cas, isCaseClause := stmt.(*ast.CaseClause)
		if !isCaseClause {
			// Invariant: go/ast holds only case clauses in the body of an expression or type switch.
			panic(fmt.Sprintf("switch body holds %T, not a case clause", stmt))
		}
		if cas.List == nil {
			// default branch
			hasDefault = true
			head := function.newBasicBlock(fmt.Sprintf("switch.head.%d", caseIndex))
			conds = append(conds, nil)
			heads = append(heads, head)
			function.currentBlock = head
			emitJump(function, body, cas)
		}
		for conditionIndex, cond := range cas.List {
			function.currentBlock = entry
			head := function.newBasicBlock(fmt.Sprintf("switch.head.%d.%d", caseIndex, conditionIndex))
			conds = append(conds, packageBuilder.expr(function, cond))
			heads = append(heads, head)
			function.currentBlock = head
			emitJump(function, body, cond)
		}
	}

	for index, stmt := range switchStmt.Body.List {
		clause, isCaseClause := stmt.(*ast.CaseClause)
		if !isCaseClause {
			// Invariant: go/ast holds only case clauses in the body of an expression or type switch.
			panic(fmt.Sprintf("switch body holds %T, not a case clause", stmt))
		}
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
		// Invariant: every head appended above comes with exactly one condition.
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
//
// Mutates: function gains the blocks of the switch, and its targets and current block follow them
// Mutates: label takes the switch's done block as its break target
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
	var fallthru *BasicBlock
	var dfltBlock *BasicBlock
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

		cc, isCaseClause := clause.(*ast.CaseClause)
		if !isCaseClause {
			// Invariant: go/ast holds only case clauses in the body of an expression or type switch.
			panic(fmt.Sprintf("switch body holds %T, not a case clause", clause))
		}
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
//
// Mutates: function gains the blocks of the type switch, and its targets and current block follow them
// Mutates: label takes the type switch's done block as its break target
func (packageBuilder *builder) typeSwitchStmt(function *Function, stmt *ast.TypeSwitchStmt, label *lblock) {
	if stmt.Init != nil {
		packageBuilder.stmt(function, stmt.Init)
	}

	var tag Value
	switch typed := stmt.Assign.(type) {
	case *ast.ExprStmt: // x.(type)
		guard, isTypeAssert := ast.Unparen(typed.X).(*ast.TypeAssertExpr)
		if !isTypeAssert {
			// Invariant: the parser accepts only x.(type) as a type switch's guard.
			panic(fmt.Sprintf("type switch guard %T is not a type assertion", typed.X))
		}
		tag = packageBuilder.expr(function, guard.X)
	case *ast.AssignStmt: // y := x.(type)
		guard, isTypeAssert := ast.Unparen(typed.Rhs[0]).(*ast.TypeAssertExpr)
		if !isTypeAssert {
			// Invariant: the parser accepts only y := x.(type) as a type switch's assigning guard.
			panic(fmt.Sprintf("type switch guard %T is not a type assertion", typed.Rhs[0]))
		}
		tag = packageBuilder.expr(function, guard.X)
	default:
		// Invariant: go/ast documents a type switch's Assign as x.(type) or y := x.(type), each handled above.
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
		cc, isCaseClause := clause.(*ast.CaseClause)
		if !isCaseClause {
			// Invariant: go/ast holds only case clauses in the body of an expression or type switch.
			panic(fmt.Sprintf("type switch body holds %T, not a case clause", clause))
		}
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
		cc, isCaseClause := clause.(*ast.CaseClause)
		if !isCaseClause {
			// Invariant: go/ast holds only case clauses in the body of an expression or type switch.
			panic(fmt.Sprintf("type switch body holds %T, not a case clause", clause))
		}
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
//
// Mutates: function gains the blocks of the select, and its targets and current block follow them
// Mutates: label takes the select's done block as its break target
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
	// Whether this optimization is worth its weight has not been measured.
	if len(stmt.Body.List) == 1 {
		clause, isCommClause := stmt.Body.List[0].(*ast.CommClause)
		if !isCommClause {
			// Invariant: go/ast holds only comm clauses in the body of a select.
			panic(fmt.Sprintf("select body holds %T, not a comm clause", stmt.Body.List[0]))
		}
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
	// receives are each state's receive expression, nil for a send, for the debug references a package built
	// with SetDebugMode records. go/ir keeps it on the state as DebugNode.
	var receives []*ast.UnaryExpr
	blocking := true
	for _, clause := range stmt.Body.List {
		var st *SelectState
		commClause, isCommClause := clause.(*ast.CommClause)
		if !isCommClause {
			// Invariant: go/ast holds only comm clauses in the body of a select.
			panic(fmt.Sprintf("select body holds %T, not a comm clause", clause))
		}
		switch comm := commClause.Comm.(type) {
		case nil: // default case
			blocking = false
			continue

		case *ast.SendStmt: // ch<- i
			channel := packageBuilder.expr(function, comm.Chan)
			channelType, isChannel := core_type.Of(function.typ(channel.Type())).(*types.Chan)
			if !isChannel {
				// Invariant: go/types accepts a send only on an operand whose core type is a channel.
				panic(fmt.Sprintf("send on %s, which has no channel core type", channel.Type()))
			}
			st = &SelectState{
				Direction: types.SendOnly,
				Channel:   channel,
				Send: emitConv(function, packageBuilder.expr(function, comm.Value),
					channelType.Elem(), comm),
				Position: comm.Arrow,
			}
			receives = append(receives, nil)

		case *ast.AssignStmt: // x := <-ch
			recv, isUnary := ast.Unparen(comm.Rhs[0]).(*ast.UnaryExpr)
			if !isUnary {
				// Invariant: the parser accepts an assignment as a select case only when it receives, <-ch.
				panic(fmt.Sprintf("select case assigns %T, not a receive", comm.Rhs[0]))
			}
			st = &SelectState{
				Direction: types.RecvOnly,
				Channel:   packageBuilder.expr(function, recv.X),
				Position:  recv.OpPos,
			}
			receives = append(receives, recv)

		case *ast.ExprStmt: // <-ch
			recv, isUnary := ast.Unparen(comm.X).(*ast.UnaryExpr)
			if !isUnary {
				// Invariant: the parser accepts an expression as a select case only when it receives, <-ch.
				panic(fmt.Sprintf("select case evaluates %T, not a receive", comm.X))
			}
			st = &SelectState{
				Direction: types.RecvOnly,
				Channel:   packageBuilder.expr(function, recv.X),
				Position:  recv.OpPos,
			}
			receives = append(receives, recv)
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
			channelType, isChannel := core_type.Of(function.typ(st.Channel.Type())).(*types.Chan)
			if !isChannel {
				// Invariant: go/types accepts a receive only from an operand whose core type is a channel.
				panic(fmt.Sprintf("receive from %s, which has no channel core type", st.Channel.Type()))
			}
			tElem := channelType.Elem()
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
	receivedValueIndex := 2 // index in 'sel' tuple of value; increments if st.Direction==types.RecvOnly
	for _, cc := range stmt.Body.List {
		clause, isCommClause := cc.(*ast.CommClause)
		if !isCommClause {
			// Invariant: go/ast holds only comm clauses in the body of a select.
			panic(fmt.Sprintf("select body holds %T, not a comm clause", cc))
		}
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
			if function.debugInformation() {
				value := emitExtract(function, sel, receivedValueIndex, comm)
				emitDebugReference(function, receives[state], value, false)
			}
			receivedValueIndex++

		case *ast.AssignStmt: // x := <-states[state].Channel
			if comm.Tok == token.DEFINE {
				id, isIdent := comm.Lhs[0].(*ast.Ident)
				if !isIdent {
					// Invariant: go/types accepts only identifiers on the left of :=.
					panic(fmt.Sprintf("short variable declaration of %T, not an identifier", comm.Lhs[0]))
				}
				emitLocalVariable(function, identVariable(function, id), id)
			}
			x := packageBuilder.addr(function, comm.Lhs[0], false) // non-escaping
			value := emitExtract(function, sel, receivedValueIndex, comm)
			if function.debugInformation() {
				emitDebugReference(function, receives[state], value, false)
			}
			x.store(function, value, comm)

			if len(comm.Lhs) == 2 { // x, ok := ...
				if comm.Tok == token.DEFINE {
					id, isIdent := comm.Lhs[1].(*ast.Ident)
					if !isIdent {
						// Invariant: go/types accepts only identifiers on the left of :=.
						panic(fmt.Sprintf("short variable declaration of %T, not an identifier", comm.Lhs[1]))
					}
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
//
// Mutates: function gains the blocks of the loop, and its targets and current block follow them
// Mutates: label takes the loop's done and continue blocks as its break and continue targets
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
//
// Mutates: function gains the blocks of the loop, and its variables, targets and current block follow them
// Mutates: label takes the loop's done and post blocks as its break and continue targets
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

	init, isAssign := stmt.Init.(*ast.AssignStmt)
	if !isAssign {
		// Invariant: forStmt calls forStmtGo122 only when the loop's init is a := assignment.
		panic(fmt.Sprintf("for loop init %T is not an assignment", stmt.Init))
	}
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
		id, isIdent := lhs.(*ast.Ident)
		if !isIdent {
			// Invariant: go/types accepts only identifiers on the left of :=.
			panic(fmt.Sprintf("short variable declaration of %T, not an identifier", lhs))
		}
		variable := identVariable(function, id)
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
				dead[variable.phi], dead[variable.next], dead[variable.load], dead[variable.store] =
					true, true, true, true
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

// stmt lowers statement s to IR form, emitting code to fn.
//
// Mutates: function gains the blocks and instructions for the statement
func (packageBuilder *builder) stmt(function *Function, statement ast.Stmt) {
	// The label of the current statement.  If non-nil, its _goto
	// target is always set; its _break and _continue are set only
	// within the body of switch/typeswitch/select/for/range.
	// It is effectively an additional default-nil parameter of stmt().
	var label *lblock
	// current is the statement being lowered: each label in front of it is peeled first, outermost first. A named
	// label becomes the current label, its goto block entered by a jump, and a blank one keeps the label before it,
	// since a blank label can't be the target of a goto, break or continue and needs no block.
	current := statement
	for {
		labeled, isLabeled := current.(*ast.LabeledStmt)
		if !isLabeled {
			break
		}
		if labeled.Label.Name != "_" {
			label = function.lblockOf(function.label(labeled.Label))
			label.resolved = true
			emitJump(function, label._goto, labeled)
			function.currentBlock = label._goto
		}
		current = labeled.Stmt
	}
	switch typed := current.(type) {
	case *ast.EmptyStmt:
		// ignore.  (Usually removed by gofmt.)

	case *ast.DeclStmt: // Con, Var or Typ
		decl, isGenDecl := typed.Decl.(*ast.GenDecl)
		if !isGenDecl {
			// Invariant: go/ast documents a DeclStmt's Decl as a GenDecl, since functions are declared only at
			// package level.
			panic(fmt.Sprintf("declaration statement holds %T, not a GenDecl", typed.Decl))
		}
		if decl.Tok == token.VAR {
			for _, spec := range decl.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					packageBuilder.localValueSpec(function, vs)
				}
			}
		}

	case *ast.ExprStmt:
		packageBuilder.expr(function, typed.X)

	case *ast.SendStmt:
		channelType, isChannel := core_type.Of(function.typeOf(typed.Chan)).(*types.Chan)
		if !isChannel {
			// Invariant: go/types accepts a send only on an operand whose core type is a channel.
			panic(fmt.Sprintf("send on %s, which has no channel core type", function.typeOf(typed.Chan)))
		}
		instr := &Send{
			Channel: packageBuilder.expr(function, typed.Chan),
			X: emitConv(function, packageBuilder.expr(function, typed.Value),
				channelType.Elem(), typed),
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
			packageBuilder.assignStmt(function, typed.Lhs, typed.Rhs, typed.Tok == token.DEFINE, current)

		default: // +=, etc.
			op := typed.Tok + token.ADD - token.ADD_ASSIGN
			packageBuilder.assignOp(
				function,
				packageBuilder.addr(function, typed.Lhs[0], false),
				packageBuilder.expr(function, typed.Rhs[0]),
				op,
				typed,
			)
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
		// Invariant: the cases above cover every statement go/ast has but BadStmt, which go/types rejects, and the
		// case and comm clauses, which their switch or select builds.
		panic(fmt.Sprintf("unexpected statement kind: %T", typed))
	}
}

// branchStmt emits to fn the jump for the break, continue, goto or fallthrough statement `stmt`.
//
// Mutates: function moves on to an unreachable block after the jump
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

// returnStmt emits to fn code for the return statement `stmt`.
//
// Mutates: function moves on to an unreachable block after the return
func (packageBuilder *builder) returnStmt(function *Function, stmt *ast.ReturnStmt) {
	// Each stored result takes the position of the whole return
	// statement, not that of its own returned expression.

	var results []Value

	sig := function.source.Signature // signature of the enclosing source function

	// Convert return operands to result type.
	if len(stmt.Results) == 1 && sig.Results().Len() > 1 {
		// Return of one expression in a multi-valued function.
		tuple := packageBuilder.exprN(function, stmt.Results[0])
		ttuple, isTuple := tuple.Type().(*types.Tuple)
		if !isTuple {
			// Invariant: exprN returns a value whose type is a tuple.
			panic(fmt.Sprintf("returned %s has type %s, not a tuple", tuple, tuple.Type()))
		}
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
