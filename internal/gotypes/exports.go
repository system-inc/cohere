package gotypes

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
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

// importer reads the product's export data. It is not safe for concurrent use.
func (exports *Exports) importer(fileSet *token.FileSet) types.Importer {
	return importer.ForCompiler(fileSet, "gc", func(path string) (io.ReadCloser, error) {
		file := exports.files[path]
		if file == "" {
			return nil, fmt.Errorf(
				"the %s product (%s) holds no export data for %s",
				exports.name,
				exports.key[:12],
				path,
			)
		}
		return os.Open(file)
	})
}
