package go_types

import "github.com/system-inc/go-types/types"

type importerFunction func(path string) (*types.Package, error)

func (function importerFunction) Import(path string) (*types.Package, error) { return function(path) }
