package merged

import (
	"fmt"

	"github.com/system-inc/go-types/types"
)

// plainImporter imports one build's packages from its export data, unmerged.
type plainImporter struct {
	build *build // nil for a build outside the importer's builds
}

func (plain plainImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if plain.build == nil {
		return nil, fmt.Errorf("no build of this importer is asked for %q", path)
	}
	var importedPackage *types.Package
	if failure := plain.build.read(func() (importError error) {
		importedPackage, importError = plain.build.source.Import(path)
		return importError
	}); failure != nil {
		return nil, fmt.Errorf("build %d: %w", plain.build.index, failure)
	}
	if importedPackage == nil {
		return nil, fmt.Errorf("build %d doesn't compile %q", plain.build.index, path)
	}
	return importedPackage, nil
}
