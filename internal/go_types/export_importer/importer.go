// Package export_importer is go/importer's gc importer over cohere's fork of go/types (#cs9qm4x): it reads the
// compiler's export data into the fork's types, which go/importer cannot, since what it returns is the standard
// library's. It reads through the fork's own reader, the one go/importer wraps, which knows the export data of the
// toolchain the fork is taken from.
//
// The fork's reader reads an object the first time it is looked up, and a lazy universe of packages is read from one
// goroutine at a time. A checker that looks an imported object up reads the universe from its own goroutine, outside
// any lock its importer holds, so checks on many goroutines sharing one importer once panicked on a nil type, half
// read. So the universe is eager: the reader reads each package whole before Import hands it back, as go/importer
// did, and leaves no lookup anything to read.
package export_importer

import (
	"go/token"
	"io"

	"github.com/system-inc/go-types/gcimporter"
	"github.com/system-inc/go-types/types"
)

// Lookup opens the export data of the package at an import path, as go/importer's Lookup does.
type Lookup func(path string) (io.ReadCloser, error)

// importer is one universe of the packages read so far, and how to find a package's export data.
type importer struct {
	fileSet  *token.FileSet
	universe *gcimporter.Universe
	lookup   Lookup
}

// New is go/importer's ForCompiler(fileSet, "gc", lookup) over the fork. A nil lookup finds each package's export data
// as go/importer's does, through the go command. Like go/importer's, it is not safe for concurrent use.
func New(fileSet *token.FileSet, lookup Lookup) types.Importer {
	universe := gcimporter.NewUniverse()
	universe.Eager = true
	return &importer{fileSet: fileSet, universe: universe, lookup: lookup}
}

// Default is go/importer's Default over the fork: the gc importer with a file set of its own, finding export data
// through the go command.
func Default() types.Importer {
	return New(token.NewFileSet(), nil)
}

// Import reads the package at path, whole, as go/importer's does, with no vendoring.
func (importer *importer) Import(path string) (*types.Package, error) {
	return gcimporter.Import(importer.fileSet, importer.universe, path, "", importer.lookup)
}
