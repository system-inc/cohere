package check

import (
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"runtime"
	"sync"
	"testing"
	"weak"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

// A kept package's export data reader holds the package's types and nothing else of the host's analysis: its syntax
// and type maps are collected while the reader lives, and the reader still writes the types, once however many loads
// read at the same moment, as export data that imports back to the same package.
//
// Not parallel: whether the type maps were collected is read after runtime.GC, which another test's allocations could
// keep from finishing what this one asks.
func TestKeptExportDataHoldsOnlyThePackagesTypes(test *testing.T) {
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "kept.go", "package kept\n\ntype Counter struct{ count int }\n\n"+
		"func (counter *Counter) Add() { counter.count++ }\n", parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	information := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}}
	checked, checkError := (&types.Config{Importer: importer.Default()}).Check("example.com/kept", fileSet,
		[]*ast.File{file}, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	kept := &packages.Package{ID: "example.com/kept", PkgPath: "example.com/kept", Types: checked, Fset: fileSet,
		Syntax: []*ast.File{file}, TypesInfo: information}
	typeMaps := weak.Make(information)

	readExportData := lazyExportData(kept)
	kept, information, file = nil, nil, nil
	runtime.GC()
	if typeMaps.Value() != nil {
		test.Error("the export data reader kept the package's type maps alive, want only its types held")
	}

	written := map[string]bool{}
	var lock sync.Mutex
	var reading sync.WaitGroup
	for range 16 {
		reading.Go(func() {
			exportData, readError := readExportData()
			lock.Lock()
			defer lock.Unlock()
			written[string(exportData)] = true
			if readError != nil {
				test.Errorf("a load read the export data with %v, want none", readError)
			}
		})
	}
	reading.Wait()
	if len(written) != 1 {
		test.Fatalf("16 loads read %d different export data, want one write they all read", len(written))
	}
	for exportData := range written {
		imported, importError := gcexportdata.Read(bytes.NewReader([]byte(exportData)), token.NewFileSet(),
			map[string]*types.Package{}, "example.com/kept")
		if importError != nil || imported.Scope().Lookup("Counter") == nil {
			test.Errorf("the export data imported as %v with %v, want the package with Counter", imported, importError)
		}
	}
}

// Release lets go of every entry and rendering the host kept once no build is left to ask, and keeps the count the
// record writes; a run that kept nothing releases nothing.
func TestReleasingTheHostKeepsOnlyItsCounts(test *testing.T) {
	test.Parallel()

	host := &HostRun{entries: map[string]*hostEntry{"a": {}, "b": {}}, renderings: NewReadsBuild(), kept: 2}
	host.Release()
	if host.entries != nil || host.renderings != nil {
		test.Errorf("after Release the host holds %d entries and renderings %v, want none", len(host.entries),
			host.renderings)
	}
	if host.EntriesKept() != 2 {
		test.Errorf("after Release the host counts %d entries kept, want 2", host.EntriesKept())
	}
	var none *HostRun
	none.Release()
}
