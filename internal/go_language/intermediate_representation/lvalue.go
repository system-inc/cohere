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
	storedType() types.Type                                 // returns the type of the location
}
