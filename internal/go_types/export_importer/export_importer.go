// Package export_importer is go/importer's gc importer over cohere's fork of go/types (#cs9qm4x): it reads the
// compiler's export data into the fork's types, which go/importer cannot, since what it returns is the standard
// library's. It reads through the fork's own reader, the one go/importer wraps, which knows the export data of the
// toolchain the fork is taken from.
//
// The fork's reader reads an object the first time it is looked up, and a universe of packages is read from one
// goroutine at a time. A checker that looks an imported object up reads the universe from its own goroutine, outside any
// lock its importer holds, so checks on many goroutines sharing one importer once panicked on a nil type, half read.
// This importer reads each import whole before handing it back, as go/importer did, and leaves no lookup anything to
// read.
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
	// namesRead is how many names of each package of the universe were read whole. A package first reached through
	// another's export data holds only the names that data names, and gains the rest when it is imported itself, so
	// a package is read again whenever it holds more names than were read.
	namesRead map[*types.Package]int
}

// New is go/importer's ForCompiler(fileSet, "gc", lookup) over the fork. A nil lookup finds each package's export data
// as go/importer's does, through the go command. Like go/importer's, it is not safe for concurrent use.
func New(fileSet *token.FileSet, lookup Lookup) types.Importer {
	return &importer{
		fileSet:   fileSet,
		universe:  gcimporter.NewUniverse(),
		lookup:    lookup,
		namesRead: map[*types.Package]int{},
	}
}

// Default is go/importer's Default over the fork: the gc importer with a file set of its own, finding export data
// through the go command.
func Default() types.Importer {
	return New(token.NewFileSet(), nil)
}

// Import reads the package at path, as go/importer's does, with no vendoring, and then whatever its universe holds
// unread.
func (importer *importer) Import(path string) (*types.Package, error) {
	imported, importError := gcimporter.Import(importer.fileSet, importer.universe, path, "", importer.lookup)
	if importError != nil {
		return nil, importError
	}
	importer.readWhole()
	return imported, nil
}

// readWhole reads every object of every package the universe holds that it hasn't read, and each named type's
// declaration and methods, until a pass finds nothing unread: reading one package's objects can bring in another, or
// more names of one already read.
func (importer *importer) readWhole() {
	for {
		var unread []*types.Package
		for _, universePackage := range importer.universe.Packages {
			if universePackage.Scope().Len() != importer.namesRead[universePackage] {
				unread = append(unread, universePackage)
			}
		}
		if len(unread) == 0 {
			return
		}
		for _, universePackage := range unread {
			scope := universePackage.Scope()
			importer.namesRead[universePackage] = scope.Len()
			for _, name := range scope.Names() {
				typeName, isTypeName := scope.Lookup(name).(*types.TypeName)
				if !isTypeName {
					continue
				}
				named, isNamed := typeName.Type().(*types.Named)
				if !isNamed {
					continue
				}
				named.Underlying()
				for index := range named.NumMethods() {
					named.Method(index)
				}
			}
		}
	}
}
