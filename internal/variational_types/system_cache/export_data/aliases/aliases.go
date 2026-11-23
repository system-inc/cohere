// cohere:copied golang.org/x/tools@v0.50.0 internal/aliases/aliases.go
// cohere:until golang.org/x/tools/go/gcexportdata writes and reads shallow
// export data, as its internal gcimporter's IExportShallow and IImportShallow
// do for gopls. Then the engine calls gcexportdata again and this copy goes
// away.
//
// cohere: this file is the engine's copy, in internal/go_engine/export_data,
// with go/types rewritten to the fork's, github.com/system-inc/go-types/types,
// so one walk reads the system cache's export data as the fork's types.
// Re-copy it whenever the engine's changes and rewrite the imports again;
// nothing else differs.
//
// cohere: an internal package of x/tools cannot be imported from this module,
// so the engine carries its own copy of the shallow export and import it
// caches types with, one file to each upstream file: iexport.go and
// iimport.go, predeclared.go and the vestiges of bimport.go they call, and
// the parts of internal/aliases and internal/typesinternal they call, in
// subpackages of the same names. The unified reader, the archive reading
// and pkgbits are not copied, and neither is any deep export or import, so
// every read of the engine's export data is shallow, against packages the
// reader already holds.

// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aliases

import (
	"go/token"
	// cohere: go/types is the fork's, imported below.

	// cohere: the fork's go/types, which checks every build at once.
	"github.com/system-inc/go-types/types"
)

// New creates a new TypeName in Package pkg that
// is an alias for the type rhs.
func New(pos token.Pos, pkg *types.Package, name string, rhs types.Type, tparams []*types.TypeParam) *types.TypeName {
	tname := types.NewTypeName(pos, pkg, name, nil)
	types.NewAlias(tname, rhs).SetTypeParams(tparams)
	return tname
}
