// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines the AST test for the blank identifier.

import "go/ast"

// isBlankIdent returns true iff `expr` is an Ident with name "_".
// They have no associated types.Object, and thus no type.
func isBlankIdent(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "_"
}
