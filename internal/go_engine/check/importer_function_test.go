package check

import "go/types"

// importerFunction is a function as a types.Importer, for a test that type-checks packages of its own in memory.
type importerFunction func(path string) (*types.Package, error)

// Import is the function's package at path.
func (function importerFunction) Import(path string) (*types.Package, error) { return function(path) }
