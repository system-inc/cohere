// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package checker

// This file is copied from golang.org/x/tools@v0.50.0's internal/analysis/driverutil, the parts checker.go
// calls: readfile.go (Copyright 2020), url.go (Copyright 2023) and validatefix.go (Copyright 2025), each
// The Go Authors, under the same BSD-style license. An internal package of x/tools cannot be imported from
// this module, so the copy of checker.go carries its own. The code is unchanged.
//
// ValidateFixes is the largest of them and is kept whole because it guards our byte offsets: check turns each
// suggested fix's edits into 0-based offsets into a file, and an edit that runs backwards, spans two files or
// ends past the end of its file would become offsets that cut the wrong bytes. Upstream panics on such a fix, the
// recover in check turns that into a crash naming the analyzer, and no wrong offset reaches a finding.

import (
	"cmp"
	"fmt"
	"go/token"
	"net/url"
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

// ResolveURL resolves the URL field for a Diagnostic from an Analyzer
// and returns the URL. See Diagnostic.URL for details.
func ResolveURL(a *analysis.Analyzer, d analysis.Diagnostic) (string, error) {
	if d.URL == "" && d.Category == "" && a.URL == "" {
		return "", nil // do nothing
	}
	raw := d.URL
	if d.URL == "" && d.Category != "" {
		raw = "#" + d.Category
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid Diagnostic.URL %q: %s", raw, err)
	}
	base, err := url.Parse(a.URL)
	if err != nil {
		return "", fmt.Errorf("invalid Analyzer.URL %q: %s", a.URL, err)
	}
	return base.ResolveReference(u).String(), nil
}

// ValidateFixes validates the set of fixes for a single diagnostic.
// Any error indicates a bug in the originating analyzer.
//
// It updates fixes so that fixes[*].End.IsValid().
//
// It may be used as part of an analysis driver implementation.
func ValidateFixes(fset *token.FileSet, a *analysis.Analyzer, fixes []analysis.SuggestedFix) error {
	fixMessages := make(map[string]bool)
	for i := range fixes {
		fix := &fixes[i]
		if fixMessages[fix.Message] {
			return fmt.Errorf("analyzer %q suggests two fixes with same Message (%s)", a.Name, fix.Message)
		}
		fixMessages[fix.Message] = true
		if err := validateFix(fset, fix); err != nil {
			return fmt.Errorf("analyzer %q suggests invalid fix (%s): %v", a.Name, fix.Message, err)
		}
	}
	return nil
}

// validateFix validates a single fix.
// Any error indicates a bug in the originating analyzer.
//
// It updates fix so that fix.End.IsValid().
func validateFix(fset *token.FileSet, fix *analysis.SuggestedFix) error {

	// Stably sort edits by Pos. This ordering puts insertions
	// (end = start) before deletions (end > start) at the same
	// point, but uses a stable sort to preserve the order of
	// multiple insertions at the same point.
	slices.SortStableFunc(fix.TextEdits, func(x, y analysis.TextEdit) int {
		if sign := cmp.Compare(x.Pos, y.Pos); sign != 0 {
			return sign
		}
		return cmp.Compare(x.End, y.End)
	})

	var prev *analysis.TextEdit
	for i := range fix.TextEdits {
		edit := &fix.TextEdits[i]

		// Validate edit individually.
		start := edit.Pos
		file := fset.File(start)
		if file == nil {
			return fmt.Errorf("no token.File for TextEdit.Pos (%v)", edit.Pos)
		}
		fileEnd := token.Pos(file.Base() + file.Size())
		if end := edit.End; end.IsValid() {
			if end < start {
				return fmt.Errorf("TextEdit.Pos (%v) > TextEdit.End (%v)", edit.Pos, edit.End)
			}
			endFile := fset.File(end)
			if endFile != file && end < fileEnd+10 {
				// Relax the checks below in the special case when the end position
				// is only slightly beyond EOF, as happens when End is computed
				// (as in ast.{Struct,Interface}Type) rather than based on
				// actual token positions. In such cases, truncate end to EOF.
				//
				// This is a workaround for #71659; see:
				// https://github.com/golang/go/issues/71659#issuecomment-2651606031
				// A better fix would be more faithful recording of token
				// positions (or their absence) in the AST.
				edit.End = fileEnd
				continue
			}
			if endFile == nil {
				return fmt.Errorf("no token.File for TextEdit.End (%v; File(start).FileEnd is %d)", end, file.Base()+file.Size())
			}
			if endFile != file {
				return fmt.Errorf("edit #%d spans files (%v and %v)",
					i, file.Position(edit.Pos), endFile.Position(edit.End))
			}
		} else {
			edit.End = start // update the SuggestedFix
		}
		if eof := fileEnd; edit.End > eof {
			return fmt.Errorf("end is (%v) beyond end of file (%v)", edit.End, eof)
		}

		// Validate the sequence of edits:
		// properly ordered, no overlapping deletions
		if prev != nil && edit.Pos < prev.End {
			xpos := fset.Position(prev.Pos)
			xend := fset.Position(prev.End)
			ypos := fset.Position(edit.Pos)
			yend := fset.Position(edit.End)
			return fmt.Errorf("overlapping edits to %s (%d:%d-%d:%d and %d:%d-%d:%d)",
				xpos.Filename,
				xpos.Line, xpos.Column,
				xend.Line, xend.Column,
				ypos.Line, ypos.Column,
				yend.Line, yend.Column,
			)
		}
		prev = edit
	}

	return nil
}
