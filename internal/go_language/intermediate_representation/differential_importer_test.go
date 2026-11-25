package intermediate_representation_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// differentialImporter loads the packages source imports through go/packages, for their types alone.
func differentialImporter(test *testing.T, source string) types.Importer {
	test.Helper()
	file, parseError := parser.ParseFile(token.NewFileSet(), "", source, parser.ImportsOnly)
	if parseError != nil {
		test.Fatal(parseError)
	}
	imported := map[string]*types.Package{}
	var paths []string
	for _, spec := range file.Imports {
		paths = append(paths, strings.Trim(spec.Path.Value, `"`))
	}
	if len(paths) > 0 {
		loaded, loadError := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedTypes}, paths...)
		if loadError != nil {
			test.Fatal(loadError)
		}
		for _, each := range loaded {
			imported[each.PkgPath] = each.Types
		}
	}
	return differentialImporterFunction(func(path string) (*types.Package, error) {
		if path == "unsafe" {
			return types.Unsafe, nil
		}
		if found, isFound := imported[path]; isFound {
			return found, nil
		}
		return nil, fmt.Errorf("the differential loaded no package %q", path)
	})
}

type differentialImporterFunction func(path string) (*types.Package, error)

func (importer differentialImporterFunction) Import(path string) (*types.Package, error) {
	return importer(path)
}
