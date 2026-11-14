package check

import (
	"fmt"
	"go/token"
	"go/types"
	"maps"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/export_data"
)

// exportDataSource is one standard-library package's export data, nil for unsafe, with the paths of the packages it
// imports, which its shallow read needs read before it.
type exportDataSource struct {
	imports    []string
	exportData []byte
}

// StandardLibraryExportData reads the shallow export data of a set of standard-library packages, a table's or a run's,
// each against the packages it imports, read first from the same set, so every read holds every package its
// declarations reach, complete, as a shallow read needs (export_data.Read). The code that renames a table's files and
// the code that digests a table's entries have no load whose packages they could read against, only the set itself.
// Each package is read once, into a file set of its own, which a rename copies file by file (renameExportDataFiles). A
// package whose export data does not read, or that two of the set hold, or that the set does not hold, is an error,
// and so is each package that reaches it.
type StandardLibraryExportData struct {
	sources map[string]exportDataSource

	// ambiguous are the paths the set holds more than once, which no read can choose between.
	ambiguous map[string]bool

	// read is every package read so far, by path, unsafe as the type checker's own, and fileSets each one's file
	// set, which holds its positions and no other package's.
	read     map[string]*types.Package
	fileSets map[string]*token.FileSet

	// failed is each package whose read failed, with why.
	failed map[string]error
}

// newStandardLibraryExportData is an empty set, which add fills.
func newStandardLibraryExportData() *StandardLibraryExportData {
	return &StandardLibraryExportData{
		sources:   map[string]exportDataSource{},
		ambiguous: map[string]bool{},
		read:      map[string]*types.Package{"unsafe": types.Unsafe},
		fileSets:  map[string]*token.FileSet{},
		failed:    map[string]error{},
	}
}

// StandardLibraryExportDataOf is the set of entries, a table's entries of one variant, each read by the path and
// imports it decodes to. An entry that does not decode is left out, so a read that reaches it fails naming its path,
// as StandardLibraryDigestsOf of the entry itself fails on its decode.
func StandardLibraryExportDataOf(entries []cache.StandardLibraryEntry) *StandardLibraryExportData {
	set := newStandardLibraryExportData()
	for _, entry := range entries {
		decoded, decodeError := decodeStandardLibraryEntry(entry.Entry)
		if decodeError != nil {
			continue
		}
		set.add(decoded.PackagePath, exportDataSource{imports: decoded.Imports, exportData: entry.ExportData})
	}
	return set
}

// add puts the package at path in the set, before anything is read. A path added twice is ambiguous, and no read of
// it, or of anything that reaches it, succeeds.
func (set *StandardLibraryExportData) add(path string, source exportDataSource) {
	if _, held := set.sources[path]; held {
		set.ambiguous[path] = true
	}
	set.sources[path] = source
}

// readPackage is the package at path read from the set, with the file set its positions are in, nil for unsafe,
// reading first every package it imports that is not read yet.
func (set *StandardLibraryExportData) readPackage(path string) (*types.Package, *token.FileSet, error) {
	if read, done := set.read[path]; done {
		return read, set.fileSets[path], nil
	}
	if failed, done := set.failed[path]; done {
		return nil, nil, failed
	}
	read, fileSet, readError := set.readOnce(path)
	if readError != nil {
		set.failed[path] = readError
		return nil, nil, readError
	}
	set.fileSets[path] = fileSet
	return read, fileSet, nil
}

// readOnce reads the package at path, which neither read nor failed holds: its imports, then its own export data.
func (set *StandardLibraryExportData) readOnce(path string) (*types.Package, *token.FileSet, error) {
	if set.ambiguous[path] {
		return nil, nil, fmt.Errorf("reading the export data of %s: the set holds two packages of its path", path)
	}
	source, held := set.sources[path]
	if !held {
		return nil, nil, fmt.Errorf("reading the export data of %s: the set does not hold it", path)
	}
	if importsError := set.readImports(path, source.imports); importsError != nil {
		return nil, nil, importsError
	}
	if source.exportData == nil {
		return nil, nil, fmt.Errorf("reading the export data of %s: it holds none", path)
	}
	fileSet := token.NewFileSet()
	read, readError := export_data.Read(fileSet, set.read, source.exportData, path)
	if readError != nil {
		// The read put the package in the set's read even when it failed.
		delete(set.read, path)
		return nil, nil, readError
	}
	return read, fileSet, nil
}

// readImports reads each of imports, the packages the package at path imports, or is the first error, naming it.
func (set *StandardLibraryExportData) readImports(path string, imports []string) error {
	for _, imported := range imports {
		if _, _, importError := set.readPackage(imported); importError != nil {
			return fmt.Errorf("reading the export data of %s, which imports %s: %w", path, imported, importError)
		}
	}
	return nil
}

// readAgainst is exportData, the export data of the package at path, which imports imports, read against the set's
// packages, each import read first, into fileSet. It reads whether or not the set holds a package of path, and leaves
// the set's own read of it alone, so an entry is read by its own bytes, never by those the set holds of its path.
func (set *StandardLibraryExportData) readAgainst(
	fileSet *token.FileSet,
	path string,
	imports []string,
	exportData []byte,
) (*types.Package, error) {
	if importsError := set.readImports(path, imports); importsError != nil {
		return nil, importsError
	}
	closure := maps.Clone(set.read)
	delete(closure, path)
	return export_data.Read(fileSet, closure, exportData, path)
}
