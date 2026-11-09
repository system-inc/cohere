// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file keeps the part of go/ir's printer that Const.String and the members' RelString need:
// a constant prints as go/ir prints it, value:type, which readers' messages repeat. The rest of the
// printer, each instruction's String and the function and package disassembly, is not carried.

import (
	"fmt"
	"go/types"
)

func relType(t types.Type, from *types.Package) string {
	return types.TypeString(t, types.RelativeTo(from))
}

func relString(m Member, from *types.Package) string {
	// NB: not all globals have an Object (e.g. init$guard),
	// so use Package().Object not Object.Package().
	if pkg := m.Package().Pkg; pkg != nil && pkg != from {
		return fmt.Sprintf("%s.%s", pkg.Path(), m.Name())
	}
	return m.Name()
}
