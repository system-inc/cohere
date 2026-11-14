// Package export_data writes a package's types as shallow export data and reads them back: the package's own
// declarations only, each object of another package they reach named by that package's path and the object's path in
// it (objectpath), so a read needs every package the data names already in hand. Deep export data, which carries every
// declaration of a dependency the package's API reaches, is twenty times the bytes on cohere's own root, and every
// reader of the engine's export data holds each dependency complete anyway.
//
// Every other file of the package, and of its subpackages, is a copy of x/tools' internal gcimporter and what it calls
// (see iexport.go's header). This one is the engine's one writer and one reader over the copy.
package export_data

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"strings"
)

// debug and trace are gcimporter.go's, which declares them beside Import, the compiler's unified reader, which is not
// copied. Both off, a panic in the writer or the reader is recovered into an error, and nothing is traced.
const (
	debug = false
	trace = false
)

// errorReported is a read the reader reported a bug in, as an object it could not find, though it returned a package.
var errorReported = errors.New("the reader reported what it could not read")

// Write is written's types as shallow export data, with the positions fileSet holds of its own declarations. The
// writer panics on some types it cannot carry, as types a faked package "C" left invalid may be, so a caller that must
// not crash recovers.
func Write(fileSet *token.FileSet, written *types.Package) ([]byte, error) {
	// No report function: the writer reports only a panic, just before it returns it as an error or panics again.
	return IExportShallow(fileSet, written, nil)
}

// Read is the package at path read from data, shallow export data as Write wrote it, its positions added to fileSet,
// against closure, which holds every package its declarations reach, by path, complete. The read makes the package
// itself, named as the data names it, and puts it in closure, where the data's references to its own objects find it,
// so closure must not hold a package of path already. A path closure lacks is an error naming it, never a package made
// for the read: that would be a second package of the path beside the one the caller holds, a second universe whose
// objects are no other package's. An object of another package the data names that its package in closure does not
// hold is an error too, where the reader alone would leave it nil.
//
// Mutates: closure gains the package at path, read whole when the read succeeds
func Read(fileSet *token.FileSet, closure map[string]*types.Package, data []byte, path string) (*types.Package, error) {
	if _, held := closure[path]; held {
		return nil, fmt.Errorf("reading the export data of %s: the packages it is read against hold one of its path "+
			"already", path)
	}
	var reports []string
	read, readError := IImportShallow(fileSet, func(items []GetPackagesItem) error {
		return fillFromClosure(closure, path, items)
	}, data, path, func(format string, arguments ...any) {
		reports = append(reports, fmt.Sprintf(format, arguments...))
	})
	if readError != nil {
		return nil, readError
	}
	if len(reports) > 0 {
		return nil, fmt.Errorf("reading the export data of %s: %w: %s", path, errorReported,
			strings.Join(reports, "; "))
	}
	return read, nil
}

// fillFromClosure is Read's GetPackagesFunc: each item, a package the export data of the package at path names, is
// closure's package of the item's path, but for the package at path itself, which it makes and puts in closure, or an
// error naming the first path closure lacks.
//
// Mutates: closure gains the package at path
// Mutates: items each gains its package, as a GetPackagesFunc fills them
func fillFromClosure(closure map[string]*types.Package, path string, items []GetPackagesItem) error {
	for index := range items {
		if items[index].Path == path {
			closure[path] = types.NewPackage(items[index].Path, items[index].Name)
			items[index].Pkg = closure[path]
			continue
		}
		found, held := closure[items[index].Path]
		if !held {
			return fmt.Errorf("reading the export data of %s: it names %s, which is not among the packages it is "+
				"read against", path, items[index].Path)
		}
		items[index].Pkg = found
	}
	return nil
}
