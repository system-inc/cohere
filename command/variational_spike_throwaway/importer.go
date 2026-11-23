package main

import (
	"github.com/system-inc/go-types/types"
)

// resolvingImporter imports for one of ours: it spells each import path as the package's build resolved it (a vendored
// package of std), answers unsafe itself, and answers from checked, the packages of ours a plain baseline checked from
// source, before importer. checked is nil for the variational check, whose importer holds ours itself.
type resolvingImporter struct {
	importer  types.Importer
	importMap map[string]string
	checked   *oursChecked
}

// pseudoPackage says whether path names a package no export data holds: unsafe, which the checker knows itself, and
// C, which go list names among a cgo package's imports though the files a build that runs cgo checks are cgo's
// generated ones, which import no C.
func pseudoPackage(path string) bool {
	return path == "unsafe" || path == "C"
}

func (resolving resolvingImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	resolvedPath := path
	if resolved, mapped := resolving.importMap[path]; mapped {
		resolvedPath = resolved
	}
	if checked, isChecked := resolving.checked.get(resolvedPath); isChecked {
		return checked, nil
	}
	return resolving.importer.Import(resolvedPath)
}
