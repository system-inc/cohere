// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

import (
	"go/ast"
	"go/token"
	"go/types"
)

// A debugReference maps a source-level expression, Expression, to the IR value X that is the value of that
// expression, or its address when IsAddress is set. It is go/ir's debugRef: a pseudo-instruction with no dynamic
// effect, emitted only in a package built with SetDebugMode, renamed by lifting with the rest, and stripped by
// finishBody into the function's expression map. No reader ever sees one.
//
// Pos() returns Expression.Pos(), the start of the expression, not its designated token.
//
// An identifier referring to a constant or a predeclared identifier gets none, since they are trivial and
// numerous, and neither does a parenthesized expression.
//
// Unlike go/ir's debugRef it holds no object: go/ir's is for its Object method, which ours does not have (#npb86tz).
type debugReference struct {
	anInstruction
	Expression ast.Expr // the referring expression (never *ast.ParenExpr)
	IsAddress  bool     // Expression is addressable and X is the address it denotes
	X          Value    // the value or address of Expression
}

func (reference *debugReference) Pos() token.Pos { return reference.Expression.Pos() }

func (reference *debugReference) Operands(operands []*Value) []*Value {
	return append(operands, &reference.X)
}

// emitDebugReference emits to function a debug reference associating expression with value, in a package built
// with SetDebugMode.
func emitDebugReference(function *Function, expression ast.Expr, value Value, isAddress bool) {
	reference := makeDebugReference(function, expression, value, isAddress)
	if reference == nil {
		return
	}
	function.emit(reference, nil)
}

// makeDebugReference is the debug reference emitDebugReference emits, or nil when the function records no
// expressions or the expression is one that gets none.
func makeDebugReference(function *Function, expression ast.Expr, value Value, isAddress bool) *debugReference {
	if !function.debugInformation() {
		return nil // debugging not enabled
	}
	if value == nil || expression == nil {
		// Invariant: every caller hands the expression it lowered and the value it lowered it to.
		panic("a debug reference with no expression or no value")
	}
	unparenthesized := ast.Unparen(expression)
	if id, isIdent := unparenthesized.(*ast.Ident); isIdent {
		if isBlankIdent(id) {
			return nil
		}
		switch function.objectOf(id).(type) {
		case *types.Nil, *types.Const, *types.Builtin:
			return nil
		}
	}
	return &debugReference{
		X:          value,
		Expression: unparenthesized,
		IsAddress:  isAddress,
	}
}
