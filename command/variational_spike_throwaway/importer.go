package main

import (
	"github.com/system-inc/cohere/internal/variational_types/types"
)

// resolvingImporter imports for one of ours: it spells each import path as the package's build resolved it (a vendored
// package of std), answers unsafe itself, and answers from checked, the packages of ours a plain baseline checked from
// source, before importer.
type resolvingImporter struct {
	importer  types.Importer
	importMap map[string]string
	checked   map[string]*types.Package
}

func (resolving resolvingImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if resolved, mapped := resolving.importMap[path]; mapped {
		path = resolved
	}
	if checked, isChecked := resolving.checked[path]; isChecked {
		return checked, nil
	}
	return resolving.importer.Import(path)
}
