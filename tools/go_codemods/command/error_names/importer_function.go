package main

import "go/types"

// importerFunction adapts a function to types.Importer.
type importerFunction func(path string) (*types.Package, error)

func (importer importerFunction) Import(path string) (*types.Package, error) { return importer(path) }
