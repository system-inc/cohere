package parity

import "github.com/system-inc/go-types/types"

// importerFunction is a function as a types.Importer.
type importerFunction func(path string) (*types.Package, error)

// Import is the package at path.
func (function importerFunction) Import(path string) (*types.Package, error) {
	return function(path)
}
