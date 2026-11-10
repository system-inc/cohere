// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines what the type checker records of a generic instantiation.

import (
	"go/ast"
	"go/types"
)

// instance returns whether an expression is a simple or qualified identifier
// that is a generic instantiation.
func instance(info *types.Info, expression ast.Expr) bool {
	// Compare the logic here against go/types.instantiatedIdent,
	// which also handles  *IndexExpr and *IndexListExpr.
	var id *ast.Ident
	switch x := expression.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return false
	}
	_, ok := info.Instances[id]
	return ok
}

// instanceArguments returns the Instance[id].TypeArgs as a slice.
func instanceArguments(info *types.Info, id *ast.Ident) []types.Type {
	targList := info.Instances[id].TypeArgs
	if targList == nil {
		return nil
	}

	targs := make([]types.Type, targList.Len())
	for index, length := 0, targList.Len(); index < length; index++ {
		targs[index] = targList.At(index)
	}
	return targs
}
