// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file keeps the part of go/ir's printer that Constant.String and the members' RelativeString need:
// a constant prints as go/ir prints it, value:type, which readers' messages repeat. The rest of the
// printer, each instruction's String and the function and package disassembly, is not carried.

import (
	"fmt"
	"go/types"
)

func relativeType(t types.Type, from *types.Package) string {
	return types.TypeString(t, types.RelativeTo(from))
}

// relString is m's name, qualified by the path of pkg, its package, when that is not from. go/ir reads
// the package through Member's Package method; ours has none, since Function and Global hold theirs
// as a field, so each member passes its own.
func relativeString(m Member, pkg *Package, from *types.Package) string {
	// NB: not all globals have an Object (e.g. init$guard),
	// so use the member's Package, not Object().Pkg().
	if pkg := pkg.Types; pkg != nil && pkg != from {
		return fmt.Sprintf("%s.%s", pkg.Path(), m.Name())
	}
	return m.Name()
}
