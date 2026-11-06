package load

import "go/types"

// importerFunc is a function as a types.Importer.
type importerFunc func(path string) (*types.Package, error)

func (function importerFunc) Import(path string) (*types.Package, error) { return function(path) }
