// cohere:copied golang.org/x/tools@v0.50.0 internal/typesinternal/recv.go
// cohere:until golang.org/x/tools/go/gcexportdata writes and reads shallow
// export data, as its internal gcimporter's IExportShallow and IImportShallow
// do for gopls. Then the engine calls gcexportdata again and this copy goes
// away.
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

package typesinternal

import (
	"go/types"
)

// ReceiverNamed returns the named type (if any) associated with the
// type of recv, which may be of the form N or *N, or aliases thereof.
// It also reports whether a Pointer was present.
//
// The named result may be nil if recv is from a method on an
// anonymous interface or struct types or in ill-typed code.
func ReceiverNamed(recv *types.Var) (isPtr bool, named *types.Named) {
	t := recv.Type()
	if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
		isPtr = true
		t = ptr.Elem()
	}
	named, _ = types.Unalias(t).(*types.Named)
	return
}

// cohere: Unpointer is dropped, since no copy beside this one calls it.
