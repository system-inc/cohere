package go_types

import (
	"fmt"
	"go/token"
	"io"
	"os"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_types/export_importer"
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

// importer reads the product's export data into the fork's types, as go/importer's gc importer did into the standard
// library's. It is not safe for concurrent use.
func (exports *Exports) importer(fileSet *token.FileSet) types.Importer {
	return export_importer.New(fileSet, exports.open)
}

// open is the importer's lookup: the product's export data file for path, or an error naming the product when it holds
// none.
func (exports *Exports) open(path string) (io.ReadCloser, error) {
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
}
