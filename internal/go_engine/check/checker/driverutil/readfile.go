// cohere:copied golang.org/x/tools@v0.50.0 internal/analysis/driverutil/readfile.go
// cohere:until the copy of go/analysis/checker beside this package goes away,
// since it is the only caller (see checker.go's cohere:until).
//
// cohere: an internal package of x/tools cannot be imported from this module,
// so the copy of checker.go carries its own copy of the driverutil helpers it
// calls, one file to each upstream file. The one change is marked in
// CheckedReadFile: a pass's own file names are read once.

// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package driverutil

// This file defines helpers for implementing [analysis.Pass.ReadFile].

import (
	"fmt"
	"slices"
	// cohere: for CheckedReadFile's sync.OnceValue.
	"sync"

	"golang.org/x/tools/go/analysis"
)

// A ReadFileFunc is a function that returns the
// contents of a file, such as [os.ReadFile].
type ReadFileFunc = func(filename string) ([]byte, error)

// CheckedReadFile returns a wrapper around a Pass.ReadFile
// function that performs the appropriate checks.
func CheckedReadFile(pass *analysis.Pass, readFile ReadFileFunc) ReadFileFunc {
	// cohere: CheckReadable asks the pass's shared file set for every one
	// of pass.Files on every call, until a name matches, so a rule that
	// reads each of a package's files, as the long-line rule does, asks it
	// a number of times that grows with the square of the files, and every
	// ask writes the file set's last-file cache, which all of the engine's
	// goroutines share. Here the names of pass.Files are read once for the
	// pass, on the first call that needs them, under sync.OnceValue since
	// the dispatcher may call from several goroutines at once. A name among
	// them is one CheckReadable would pass, as would one of OtherFiles or
	// IgnoredFiles, which are looked at first so that a read of those
	// never builds the names. Any other name still goes to CheckReadable,
	// so a refusal is its own, in its own words.
	//
	// The names are the pass's own, never another's, and they outlive
	// pass.Files: once letGoOfSyntax in checker.go sets pass.Files to nil,
	// CheckReadable would refuse a name the pass held, where this reads it.
	// No reader reaches that: the six that call pass.ReadFile, the
	// long-line rule (rules/consistency_no_long_line), time_format through
	// rule.Context.ReadFile, and build_tag, directive, frame_pointer and
	// asm_decl (all under rules/vet), read in their own pass's run, and a
	// package is let go of only once every action on it has finished.
	fileNames := sync.OnceValue(func() map[string]bool {
		names := make(map[string]bool, len(pass.Files))
		for _, f := range pass.Files {
			names[pass.Fset.File(f.FileStart).Name()] = true
		}
		return names
	})
	return func(filename string) ([]byte, error) {
		// cohere: CheckReadable only for a name the names above miss.
		if !slices.Contains(pass.OtherFiles, filename) &&
			!slices.Contains(pass.IgnoredFiles, filename) &&
			!fileNames()[filename] {
			if err := CheckReadable(pass, filename); err != nil {
				return nil, err
			}
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
