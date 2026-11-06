package gotypes

import "go/types"

type importerFunc func(path string) (*types.Package, error)

func (function importerFunc) Import(path string) (*types.Package, error) { return function(path) }
