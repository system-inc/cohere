// cohere:copied golang.org/x/tools@v0.50.0 internal/gcimporter/bimport.go
// cohere:until golang.org/x/tools/go/gcexportdata writes and reads shallow
// export data, as its internal gcimporter's IExportShallow and IImportShallow
// do for gopls. Then cohere-go calls gcexportdata again and this copy goes
// away.
//
// cohere: an internal package of x/tools cannot be imported from this module,
// so cohere-go carries its own copy of the shallow export and import it
// caches types with, one file to each upstream file: iexport.go and
// iimport.go, predeclared.go and the vestiges of bimport.go they call, and
// the parts of internal/aliases and internal/typesinternal they call, in
// subpackages of the same names. The unified reader, the archive reading
// and pkgbits are not copied, and neither is any deep export or import, so
// every read of cohere-go's export data is shallow, against packages the
// reader already holds.

// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains the remaining vestiges of
// $GOROOT/src/go/internal/gcimporter/bimport.go.

package export_data

// cohere: upstream's package is gcimporter, named here for its folder.

import (
	"fmt"
	"go/token"
	"go/types"
	"sync"
)

func errorf(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}

const deltaNewFile = -64 // see cmd/compile/internal/gc/bexport.go

// Synthesize a token.Pos
type fakeFileSet struct {
	fset  *token.FileSet
	files map[string]*fileInfo
}

type fileInfo struct {
	file     *token.File
	lastline int
}

const maxlines = 64 * 1024

func (s *fakeFileSet) pos(file string, line, column int) token.Pos {
	_ = column // TODO(mdempsky): Make use of column.

	// Since we don't know the set of needed file positions, we reserve maxlines
	// positions per file. We delay calling token.File.SetLines until all
	// positions have been calculated (by way of fakeFileSet.setLines), so that
	// we can avoid setting unnecessary lines. See also golang/go#46586.
	f := s.files[file]
	if f == nil {
		f = &fileInfo{file: s.fset.AddFile(file, -1, maxlines)}
		s.files[file] = f
	}
	if line > maxlines {
		line = 1
	}
	if line > f.lastline {
		f.lastline = line
	}

	// Return a fake position assuming that f.file consists only of newlines.
	return token.Pos(f.file.Base() + line - 1)
}

func (s *fakeFileSet) setLines() {
	fakeLinesOnce.Do(func() {
		fakeLines = make([]int, maxlines)
		for i := range fakeLines {
			fakeLines[i] = i
		}
	})
	for _, f := range s.files {
		f.file.SetLines(fakeLines[:f.lastline])
	}
}

var (
	fakeLines     []int
	fakeLinesOnce sync.Once
)

func chanDir(d int) types.ChanDir {
	// tag values must match the constants in cmd/compile/internal/gc/go.go
	switch d {
	case 1 /* Crecv */ :
		return types.RecvOnly
	case 2 /* Csend */ :
		return types.SendOnly
	case 3 /* Cboth */ :
		return types.SendRecv
	default:
		errorf("unexpected channel dir %d", d)
		return 0
	}
}
