package main

import "github.com/system-inc/go-types/types"

// importerFunction adapts a function to types.Importer.
type importerFunction func(path string) (*types.Package, error)

func (importer importerFunction) Import(path string) (*types.Package, error) { return importer(path) }
