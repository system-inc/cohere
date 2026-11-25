package cgo_call

import "go/types"

// importerFunction is a types.Importer that is a function. x/tools' importerFunc in cgocall.
type importerFunction func(path string) (*types.Package, error)

// Import is the package at path, as the function finds it.
func (importer importerFunction) Import(path string) (*types.Package, error) {
	return importer(path)
}
