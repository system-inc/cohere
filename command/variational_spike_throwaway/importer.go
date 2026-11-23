package main

import (
	"time"

	"github.com/system-inc/cohere/internal/variational_types/types"
)

// mergingImporter is the shape of internal/variational_types/merged's Importer, which another hand is writing: every
// package not ours, its builds' export data merged into one package whose objects hold their answers by build.
type mergingImporter interface {
	Import(path string) (*types.Package, error)
	// Add gives the importer one of ours once it is checked, so the packages of ours that import it get it.
	Add(checked *types.Package)
	// Plain is build's own export data, unmerged, for the baselines.
	Plain(build int) types.Importer
	// Timing is how long the importer spent decoding export data and merging builds' packages.
	Timing() (decode time.Duration, merge time.Duration)
}

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
