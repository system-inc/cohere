package go_types

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/test_functions"
)

// Configuration says where and how to load.
type Configuration struct {
	// Directory is where go list runs and names the source module: the module of the go.mod at or above it.
	Directory string

	// Environment is added to every go command's environment, such as `GOOS` and `GOARCH` for another target.
	Environment []string

	// Overlay is read in place of the files it names, by absolute path. Every file it names must be one of
	// the source packages' files, since go list answered from the disk: anything else is refused.
	Overlay map[string][]byte

	// Tests type-checks each named package's tests too, as go test builds them: the package with its
	// in-package test files, its external test package against that, and every package between them
	// type-checked again against it (go list's "q [p.test]"), with each test function's signature checked as
	// go test checks it before it builds the test's main.
	Tests bool

	// Exports, when set, gives the types of every package outside the source module that imports nothing of
	// it, from a product's export data (Product.Find). Unset, every package is type-checked from source,
	// which needs no product and reads every file of the whole program.
	Exports *Exports
}

// Package is one loaded package. Syntax and TypesInfo are kept for the packages the patterns name; a
// dependency read from source keeps its types, and one read from export data has only its types.
type Package struct {
	ImportPath string
	Name       string
	Fset       *token.FileSet

	// Files are the absolute paths of the files Syntax holds, in the same order.
	Files     []string
	Syntax    []*ast.File
	Types     *types.Package
	TypesInfo *types.Info

	// Errors are what go list, the parser and the type checker reported for this package.
	Errors []error
}

// Load lists the packages the patterns name and everything they import, and type-checks them: the source
// packages from source, with their syntax kept for the named ones, and the rest from config.Exports. It
// returns an error only when it cannot load at all; what the packages themselves get wrong is each
// package's Errors.
func Load(configuration Configuration, patterns ...string) (*Loaded, error) {
	root, sourceModule, moduleAtError := moduleAt(configuration.Directory)
	if moduleAtError != nil {
		return nil, moduleAtError
	}
	listed, listError := list(configuration.Directory, configuration.Environment, patterns)
	if listError != nil {
		return nil, listError
	}
	roots := map[string]bool{}
	for _, listedPackage := range listed.packages {
		if !listedPackage.DepOnly {
			roots[listedPackage.ImportPath] = true
		}
	}
	if configuration.Tests {
		// go list -deps follows no test's imports, and -test is a flag a runner refuses, so what only the tests
		// import is listed by naming it beside the patterns.
		testImports := map[string]bool{}
		for _, listedPackage := range listed.packages {
			if !roots[listedPackage.ImportPath] {
				continue
			}
			for _, imported := range append(
				append([]string{}, listedPackage.TestImports...),
				listedPackage.XTestImports...,
			) {
				if imported = canonical(
					listedPackage,
					imported,
				); listed.byPath[imported] == nil && imported != "C" && !roots[imported] {
					testImports[imported] = true
				}
			}
		}
		if len(testImports) > 0 {
			extended := append([]string{}, patterns...)
			for imported := range testImports {
				extended = append(extended, imported)
			}
			sort.Strings(extended[len(patterns):])
			if listed, listError = list(
				configuration.Directory,
				configuration.Environment,
				extended,
			); listError != nil {
				return nil, listError
			}
		}
	}
	environment, goEnvironmentValuesError := goEnvironmentValues(configuration.Directory, configuration.Environment)
	if goEnvironmentValuesError != nil {
		return nil, goEnvironmentValuesError
	}
	sizes := types.SizesFor("gc", environment["GOARCH"])
	if sizes == nil {
		return nil, fmt.Errorf("go/types knows no sizes for GOARCH %s", environment["GOARCH"])
	}

	fromExports := map[string]bool{}
	if configuration.Exports != nil {
		fromExports = exportSet(listed, sourceModule)
		for path := range fromExports {
			if path != "unsafe" && configuration.Exports.files[path] == "" {
				return nil, fmt.Errorf(
					"the %s product (%s) holds no export data for %s, which %s imports from %s: the product "+
						"was listed from %s, so this load reaches a package it does not; widen the product's patterns",
					configuration.Exports.name,
					configuration.Exports.key[:12],
					path,
					strings.Join(patterns, " "),
					root,
					strings.Join(configuration.Exports.patterns, " "),
				)
			}
		}
	}

	// Every overlaid file must be one the source packages read, or the load would claim to have read it.
	sourceFiles := map[string]bool{}
	for _, listedPackage := range listed.packages {
		if fromExports[listedPackage.ImportPath] {
			continue
		}
		names := listedPackage.GoFiles
		if configuration.Tests && roots[listedPackage.ImportPath] {
			names = append(
				append(append([]string{}, names...), listedPackage.TestGoFiles...),
				listedPackage.XTestGoFiles...,
			)
		}
		for _, name := range names {
			sourceFiles[filepath.Join(listedPackage.Directory, name)] = true
		}
		if len(listedPackage.CgoFiles) > 0 {
			return nil, fmt.Errorf(
				"%s has cgo files (%s), whose Go only a cgo run produces, and that compiles; "+
					"nothing here can type-check it from source",
				listedPackage.ImportPath,
				strings.Join(listedPackage.CgoFiles, ", "),
			)
		}
	}
	if refuseUnreadOverlayError := refuseUnreadOverlay(
		configuration.Overlay,
		sourceFiles,
	); refuseUnreadOverlayError != nil {
		return nil, refuseUnreadOverlayError
	}

	fileSet := token.NewFileSet()
	loader := &loader{
		configuration: configuration,
		fileSet:       fileSet,
		sizes:         sizes,
		listed:        listed,
		roots:         roots,
		fromExports:   fromExports,
		checked:       map[string]*Package{},
		done:          map[string]chan struct{}{},
	}
	if configuration.Exports != nil {
		loader.exportImporter = configuration.Exports.importer(fileSet)
	}
	return loader.load()
}

// Parse lists the packages the patterns name and parses their files, with any overlay read in place of the
// files it names, and type-checks nothing: for a test that reads syntax alone. Each package's Errors are what
// go list and the parser reported for it.
func Parse(configuration Configuration, patterns ...string) ([]*Package, error) {
	listed, listError := list(configuration.Directory, configuration.Environment, patterns)
	if listError != nil {
		return nil, listError
	}
	fileSet := token.NewFileSet()
	reader := &loader{configuration: configuration, fileSet: fileSet}
	var parsed []*Package
	sourceFiles := map[string]bool{}
	for _, listedPackage := range listed.packages {
		if listedPackage.DepOnly {
			continue
		}
		if len(listedPackage.CgoFiles) > 0 {
			return nil, fmt.Errorf(
				"%s has cgo files (%s), whose Go only a cgo run produces, and that compiles; "+
					"nothing here can read it as go build would",
				listedPackage.ImportPath,
				strings.Join(listedPackage.CgoFiles, ", "),
			)
		}
		for _, name := range listedPackage.GoFiles {
			sourceFiles[filepath.Join(listedPackage.Directory, name)] = true
		}
		read := &Package{ImportPath: listedPackage.ImportPath, Name: listedPackage.Name, Fset: fileSet}
		reader.parse(listedPackage, listedPackage.GoFiles, true, read)
		parsed = append(parsed, read)
	}
	if refuseUnreadOverlayError := refuseUnreadOverlay(
		configuration.Overlay,
		sourceFiles,
	); refuseUnreadOverlayError != nil {
		return nil, refuseUnreadOverlayError
	}
	return parsed, nil
}

// refuseUnreadOverlay refuses an overlay that names a file the load does not read, which it would otherwise
// claim to have read.
func refuseUnreadOverlay(overlay map[string][]byte, sourceFiles map[string]bool) error {
	var unread []string
	for path := range overlay {
		if !sourceFiles[path] {
			unread = append(unread, path)
		}
	}
	if len(unread) == 0 {
		return nil
	}
	sort.Strings(unread)
	return fmt.Errorf("the overlay replaces %s, which no package this load reads from source holds (go list "+
		"answered from the disk, and a package read from export data is read as built), so the load would not read it",
		strings.Join(unread, ", "))
}

// canonical is the import path a package's import of path resolves to.
func canonical(importer *listedPackage, path string) string {
	if resolved, ok := importer.ImportMap[path]; ok {
		return resolved
	}
	return path
}

// filesNamed is the files among parsed whose base names are among names.
func filesNamed(fileSet *token.FileSet, parsed []*ast.File, names []string) []*ast.File {
	var files []*ast.File
	for _, file := range parsed {
		if slices.Contains(names, filepath.Base(fileSet.Position(file.Pos()).Filename)) {
			files = append(files, file)
		}
	}
	return files
}

// testFunctionErrors is what go test refuses in test files before it builds a test's main
// (test_functions.Problems), each as go test words it, its position first.
func testFunctionErrors(fileSet *token.FileSet, files []*ast.File) []error {
	var found []error
	for _, problem := range test_functions.Problems(fileSet, files) {
		found = append(found, fmt.Errorf("%s: %s", problem.Position, problem.Message))
	}
	return found
}
