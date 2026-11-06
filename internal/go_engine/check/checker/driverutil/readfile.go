// cohere:copied golang.org/x/tools@v0.50.0 internal/analysis/driverutil/readfile.go
// cohere:until the copy of go/analysis/checker beside this package goes away,
// since it is the only caller (see checker.go's cohere:until).
//
// cohere: an internal package of x/tools cannot be imported from this module,
// so the copy of checker.go carries its own copy of the driverutil helpers it
// calls, one file to each upstream file. The code is unchanged.

// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package driverutil

// This file defines helpers for implementing [analysis.Pass.ReadFile].

import (
	"fmt"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// A ReadFileFunc is a function that returns the
// contents of a file, such as [os.ReadFile].
type ReadFileFunc = func(filename string) ([]byte, error)

// CheckedReadFile returns a wrapper around a Pass.ReadFile
// function that performs the appropriate checks.
func CheckedReadFile(pass *analysis.Pass, readFile ReadFileFunc) ReadFileFunc {
	return func(filename string) ([]byte, error) {
		if err := CheckReadable(pass, filename); err != nil {
			return nil, err
		}
		return readFile(filename)
	}
}

// CheckReadable enforces the access policy defined by the ReadFile field of [analysis.Pass].
func CheckReadable(pass *analysis.Pass, filename string) error {
	if slices.Contains(pass.OtherFiles, filename) ||
		slices.Contains(pass.IgnoredFiles, filename) {
		return nil
	}
	for _, f := range pass.Files {
		if pass.Fset.File(f.FileStart).Name() == filename {
			return nil
		}
	}
	return fmt.Errorf("Pass.ReadFile: %s is not among OtherFiles, IgnoredFiles, or names of Files", filename)
}
