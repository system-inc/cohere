// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines the len builtin specialized to one operand type.

import "go/types"

var lenResults = types.NewTuple(anonVariable(tInt))

// makeLength returns the len builtin specialized to type func(T)int.
func makeLength(operandType types.Type) *Builtin {
	lenParameters := types.NewTuple(anonVariable(operandType))
	return &Builtin{
		name: "len",
		sig:  types.NewSignatureType(nil, nil, nil, lenParameters, lenResults, false),
	}
}
