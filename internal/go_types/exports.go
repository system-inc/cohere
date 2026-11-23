package go_types

import (
	"fmt"
	"go/token"
	"io"
	"os"

	"github.com/system-inc/go-types/gcimporter"
	"github.com/system-inc/go-types/types"
)

// Exports is a found product: the export data file of each package it holds.
type Exports struct {
	name     string
	key      string
	patterns []string
	files    map[string]string
}

// Key is the product's key.
func (exports *Exports) Key() string { return exports.key }

// importer reads the product's export data through the fork's reader, the one go/types' own go/importer wraps, so
// the packages it returns are the fork's types the rest of the load checks with. It reads each object the first time
// it is looked up, and is not safe for concurrent use.
func (exports *Exports) importer(fileSet *token.FileSet) types.Importer {
	return &exportsImporter{exports: exports, fileSet: fileSet, universe: gcimporter.NewUniverse()}
}

// exportsImporter is go/importer's ForCompiler for "gc", over the fork: one universe of the packages read so far, and
// a lookup that opens the product's export data file for a path.
type exportsImporter struct {
	exports  *Exports
	fileSet  *token.FileSet
	universe *gcimporter.Universe
}

// Import reads the package at path, as go/importer's does, with no vendoring.
func (importer *exportsImporter) Import(path string) (*types.Package, error) {
	return gcimporter.Import(importer.fileSet, importer.universe, path, "", importer.open)
}

// open is the lookup: the product's export data file for path, or an error naming the product when it holds none.
func (importer *exportsImporter) open(path string) (io.ReadCloser, error) {
	file := importer.exports.files[path]
	if file == "" {
		return nil, fmt.Errorf(
			"the %s product (%s) holds no export data for %s",
			importer.exports.name,
			importer.exports.key[:12],
			path,
		)
	}
	return os.Open(file)
}
