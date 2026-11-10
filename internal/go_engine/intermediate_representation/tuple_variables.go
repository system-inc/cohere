// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines the variables a types.Tuple is made of.

import (
	"go/token"
	"go/types"
)

// newVariable creates a 'var' for use in a types.Tuple.
func newVariable(name string, typ types.Type) *types.Var {
	return types.NewParam(token.NoPos, nil, name, typ)
}

// anonVariable creates an anonymous 'var' for use in a types.Tuple.
func anonVariable(typ types.Type) *types.Var {
	return newVariable("", typ)
}
