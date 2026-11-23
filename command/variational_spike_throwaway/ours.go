package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/system-inc/go-types/types"
)

// ourPackage is one package the patterns matched, over every build: the union of each build's compiled files, each
// with the builds that compile it, and the union of each build's imports.
type ourPackage struct {
	path      string
	goVersion string
	// builds are the builds that compile any file of the package.
	builds    types.Mask
	fileNames []string
	files     []*ast.File
	masks     map[*ast.File]types.Mask
	// imports are the resolved import paths any build's files import, sorted, and importMap the source path each was
	// spelled as, when they differ (a vendored package of std).
	imports   []string
	importMap map[string]string
}

// collectOurs gathers the packages any build's patterns matched, the union of every build's files and imports for
// each. A package's files are each build's compiled Go files, as go list -compiled gives them: a build that runs cgo
// gives cgo's generated files in its cgo files' stead, each in that build's place in the cache, so a generated file
// holds that build's bit alone, as go/packages hands the oracle the same files.
func collectOurs(builds []*listedBuild) (map[string]*ourPackage, error) {
	ours := map[string]*ourPackage{}
	fileMasks := map[string]map[string]types.Mask{}
	importSets := map[string]map[string]bool{}
	for index, build := range builds {
		bit := types.Mask(1) << index
		for _, path := range build.matched {
			listed := build.packages[path]
			if listed.Error != nil && !strings.Contains(listed.Error.Err, noFilesError) {
				return nil, fmt.Errorf("%s in %s: %s", path, build.configuration.target(build.cgoEnabled),
					listed.Error.Err)
			}
			our := ours[path]
			if our == nil {
				our = &ourPackage{path: path, importMap: map[string]string{}}
				ours[path] = our
				fileMasks[path] = map[string]types.Mask{}
				importSets[path] = map[string]bool{}
			}
			if listed.Module != nil && our.goVersion == "" {
				our.goVersion = listed.Module.GoVersion
			}
			if len(listed.CompiledGoFiles) != 0 {
				our.builds |= bit
			}
			for _, file := range listed.CompiledGoFiles {
				fileMasks[path][file] |= bit
			}
			for _, imported := range listed.Imports {
				importSets[path][imported] = true
			}
			for spelled, resolved := range listed.ImportMap {
				our.importMap[spelled] = resolved
			}
		}
	}
	for path, our := range ours {
		for file := range fileMasks[path] {
			our.fileNames = append(our.fileNames, file)
		}
		// By name, the order go list gives a build's Go files.
		sort.Strings(our.fileNames)
		for imported := range importSets[path] {
			our.imports = append(our.imports, imported)
		}
		sort.Strings(our.imports)
	}
	return ours, nil
}

// parseOurs parses every file of ours once, into fileSet, and gives each its builds.
//
// Mutates: ours each get their files and masks.
func parseOurs(fileSet *token.FileSet, ours map[string]*ourPackage, builds []*listedBuild) error {
	for _, our := range ours {
		our.masks = map[*ast.File]types.Mask{}
		for _, name := range our.fileNames {
			parsed, parseError := parser.ParseFile(fileSet, name, nil, parser.ParseComments|parser.SkipObjectResolution)
			if parseError != nil {
				return fmt.Errorf("parsing %s: %w", name, parseError)
			}
			our.files = append(our.files, parsed)
			our.masks[parsed] = fileMaskOf(name, our.path, builds)
		}
	}
	return nil
}

// fileMaskOf is the builds whose listing of path compiles file.
func fileMaskOf(file string, path string, builds []*listedBuild) types.Mask {
	mask := types.Mask(0)
	for index, build := range builds {
		listed := build.packages[path]
		if listed == nil {
			continue
		}
		for _, compiled := range listed.CompiledGoFiles {
			if compiled == file {
				mask |= types.Mask(1) << index
				break
			}
		}
	}
	return mask
}

// dependencyOrder is ours with each package after every one of ours it imports in any build, ties broken by path.
func dependencyOrder(ours map[string]*ourPackage) ([]*ourPackage, error) {
	paths := make([]string, 0, len(ours))
	for path := range ours {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	// A path's state is zero until it is visited.
	const (
		visiting = iota + 1
		visited
	)
	states := map[string]int{}
	ordered := make([]*ourPackage, 0, len(ours))
	var visit func(path string, from []string) error
	visit = func(path string, from []string) error {
		switch states[path] {
		case visited:
			return nil
		case visiting:
			return fmt.Errorf("our packages import each other in a cycle over every build's imports: %s",
				strings.Join(append(from, path), " -> "))
		}
		states[path] = visiting
		for _, imported := range ours[path].imports {
			if _, isOurs := ours[imported]; isOurs {
				if visitError := visit(imported, append(from, path)); visitError != nil {
					return visitError
				}
			}
		}
		states[path] = visited
		ordered = append(ordered, ours[path])
		return nil
	}
	for _, path := range paths {
		if visitError := visit(path, nil); visitError != nil {
			return nil, visitError
		}
	}
	return ordered, nil
}

// filesIn is our's files that build compiles.
func (our *ourPackage) filesIn(build int) []*ast.File {
	var built []*ast.File
	for _, file := range our.files {
		if our.masks[file]&(types.Mask(1)<<build) != 0 {
			built = append(built, file)
		}
	}
	return built
}
