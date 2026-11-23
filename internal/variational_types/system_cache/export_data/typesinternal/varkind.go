// cohere:copied golang.org/x/tools@v0.50.0 internal/typesinternal/varkind.go
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

//go:build go1.25

package typesinternal

// cohere: the fork's go/types, which checks every build at once.
import "github.com/system-inc/go-types/types"

type VarKind = types.VarKind

const (
	PackageVar = types.PackageVar
	LocalVar   = types.LocalVar
	RecvVar    = types.RecvVar
	ParamVar   = types.ParamVar
	ResultVar  = types.ResultVar
	FieldVar   = types.FieldVar
)

func GetVarKind(v *types.Var) VarKind       { return v.Kind() }
func SetVarKind(v *types.Var, kind VarKind) { v.SetKind(kind) }
