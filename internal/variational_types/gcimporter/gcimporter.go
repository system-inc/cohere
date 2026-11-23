// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gcimporter implements Import for gc-generated object files.
package gcimporter

import (
	"bufio"
	"fmt"
	"github.com/system-inc/cohere/internal/variational_types/exportdata"
	"github.com/system-inc/cohere/internal/variational_types/pkgbits"
	"github.com/system-inc/cohere/internal/variational_types/types"
	"go/token"
	"io"
	"os"
)

// Import imports a gc-generated package given its import path and srcDir, adds
// the corresponding package object to the universe's packages, and returns the object.
// The universe must contain all packages already imported. (cohere's: the package's
// objects are read the first time each is looked up, into the same universe.)
func Import(fset *token.FileSet, universe *Universe, path, srcDir string, lookup func(path string) (io.ReadCloser, error)) (pkg *types.Package, err error) {
	var rc io.ReadCloser
	var id string
	if lookup != nil {
		// With custom lookup specified, assume that caller has
		// converted path to a canonical import path for use in the map.
		if path == "unsafe" {
			return types.Unsafe, nil
		}
		id = path

		// No need to re-import if the package was imported completely before.
		if pkg = universe.Packages[id]; pkg != nil && pkg.Complete() {
			return
		}
		f, err := lookup(path)
		if err != nil {
			return nil, err
		}
		rc = f
	} else {
		var filename string
		filename, id, err = exportdata.FindPkg(path, srcDir)
		if filename == "" {
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			return nil, err
		}

		// no need to re-import if the package was imported completely before
		if pkg = universe.Packages[id]; pkg != nil && pkg.Complete() {
			return
		}

		// open file
		f, err := os.Open(filename)
		if err != nil {
			return nil, err
		}
		defer func() {
			if err != nil {
				// add file name to error
				err = fmt.Errorf("%s: %v", filename, err)
			}
		}()
		rc = f
	}
	defer rc.Close()

	buf := bufio.NewReader(rc)
	data, err := exportdata.ReadUnified(buf)
	if err != nil {
		err = fmt.Errorf("import %q: %v", path, err)
		return
	}
	s := string(data)

	input := pkgbits.NewPkgDecoder(id, s)
	pkg = readUnifiedPackage(fset, nil, universe, input)

	return
}
