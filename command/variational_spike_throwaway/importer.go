package main

import (
	"github.com/system-inc/cohere/internal/variational_types/types"
)

// resolvingImporter imports for one of ours: it spells each import path as the package's build resolved it (a vendored
// package of std), and answers unsafe itself.
type resolvingImporter struct {
	importer  types.Importer
	importMap map[string]string
}

func (resolving resolvingImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if resolved, mapped := resolving.importMap[path]; mapped {
		path = resolved
	}
	return resolving.importer.Import(path)
}
