// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines the Go version a file is checked at, and the comparison of such versions.

import (
	"go/ast"
	"go/types"
	"go/version"
)

// fileGoVersion is the Go version the type checker recorded for file, or the empty string, an unknown
// future version, when it recorded none that is valid. go/ir calls x/tools' internal
// versions.FileVersion here, which a module outside x/tools can't import; this is that function on
// go/version.
func fileGoVersion(info *types.Info, file *ast.File) string {
	if fileVersion := info.FileVersions[file]; version.IsValid(fileVersion) {
		return fileVersion
	}
	return ""
}

// goVersionAtLeast reports whether the file version `value` is release or later, where the empty string is an
// unknown future version and so always later. It is x/tools' internal versions.AtLeast on go/version.
func goVersionAtLeast(value, release string) bool {
	if value == "" {
		return true
	}
	return version.Compare(version.Lang(value), version.Lang(release)) >= 0
}
