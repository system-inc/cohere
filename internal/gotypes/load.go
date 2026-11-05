package gotypes

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/testfunctions"
)

// Config says where and how to load.
type Config struct {
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

// Loaded is one load: the packages the patterns name, and every package read from source, the named ones
// among them, in dependency order, followed by any test variants Config.Tests asked for.
type Loaded struct {
	Roots  []*Package
	Source []*Package
}

// ErrorsAnywhere is every error of every package read from source, each once, in dependency order.
func (loaded *Loaded) ErrorsAnywhere() []error {
	var found []error
	for _, loadedPackage := range loaded.Source {
		found = append(found, loadedPackage.Errors...)
	}
	return found
}

// Load lists the packages the patterns name and everything they import, and type-checks them: the source
// packages from source, with their syntax kept for the named ones, and the rest from config.Exports. It
// returns an error only when it cannot load at all; what the packages themselves get wrong is each
// package's Errors.
func Load(configuration Config, patterns ...string) (*Loaded, error) {
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
			sourceFiles[filepath.Join(listedPackage.Dir, name)] = true
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
		config:      configuration,
		fileSet:     fileSet,
		sizes:       sizes,
		listed:      listed,
		roots:       roots,
		fromExports: fromExports,
		checked:     map[string]*Package{},
		done:        map[string]chan struct{}{},
	}
	if configuration.Exports != nil {
		loader.exportImporter = configuration.Exports.importer(fileSet)
	}
	return loader.load()
}

// Parse lists the packages the patterns name and parses their files, with any overlay read in place of the
// files it names, and type-checks nothing: for a test that reads syntax alone. Each package's Errors are what
// go list and the parser reported for it.
func Parse(configuration Config, patterns ...string) ([]*Package, error) {
	listed, listError := list(configuration.Directory, configuration.Environment, patterns)
	if listError != nil {
		return nil, listError
	}
	fileSet := token.NewFileSet()
	reader := &loader{config: configuration, fileSet: fileSet}
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
			sourceFiles[filepath.Join(listedPackage.Dir, name)] = true
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

type loader struct {
	config      Config
	fileSet     *token.FileSet
	sizes       types.Sizes
	listed      *listing
	roots       map[string]bool
	fromExports map[string]bool

	// exportImporter reads export data, and is not safe for concurrent use, so exportLock guards it.
	exportImporter types.Importer
	exportLock     sync.Mutex

	checkedLock sync.Mutex
	checked     map[string]*Package
	done        map[string]chan struct{}
}

func (loader *loader) load() (*Loaded, error) {
	var sourcePackages []*listedPackage
	for _, listedPackage := range loader.listed.packages {
		if loader.fromExports[listedPackage.ImportPath] || listedPackage.ImportPath == "unsafe" {
			continue
		}
		sourcePackages = append(sourcePackages, listedPackage)
		loader.done[listedPackage.ImportPath] = make(chan struct{})
	}

	// Each source package is checked once its source imports are, as many at once as there are cores. go
	// list prints a package's dependencies before it, so a package waits only on those printed earlier: an
	// import cycle, which go list reports as the package's error, cannot leave two waiting on each other.
	order := map[string]int{}
	for index, listedPackage := range loader.listed.packages {
		order[listedPackage.ImportPath] = index
	}
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	var group sync.WaitGroup
	for _, listedPackage := range sourcePackages {
		group.Add(1)
		go func() {
			defer group.Done()
			defer close(loader.done[listedPackage.ImportPath])
			for _, dependency := range listedPackage.Imports {
				dependency = canonical(listedPackage, dependency)
				if waiting, ok := loader.done[dependency]; ok && order[dependency] < order[listedPackage.ImportPath] {
					<-waiting
				}
			}
			slots <- struct{}{}
			checked, _ := loader.check(
				listedPackage,
				listedPackage.ImportPath,
				listedPackage.ImportPath,
				listedPackage.GoFiles,
				nil,
			)
			<-slots
			loader.checkedLock.Lock()
			loader.checked[listedPackage.ImportPath] = checked
			loader.checkedLock.Unlock()
		}()
	}
	group.Wait()

	loaded := &Loaded{}
	for _, listedPackage := range sourcePackages {
		checked := loader.checked[listedPackage.ImportPath]
		loaded.Source = append(loaded.Source, checked)
		if loader.roots[listedPackage.ImportPath] {
			loaded.Roots = append(loaded.Roots, checked)
		}
	}
	if loader.config.Tests {
		loaded.Source = append(loaded.Source, loader.testVariants(sourcePackages, slots)...)
	}
	for _, listedPackage := range loader.listed.packages {
		if loader.roots[listedPackage.ImportPath] && loader.fromExports[listedPackage.ImportPath] {
			// A named package read from export data has its types and nothing else.
			imported, importExportedError := loader.importExported(listedPackage.ImportPath)
			if importExportedError != nil {
				return nil, importExportedError
			}
			loaded.Roots = append(loaded.Roots, &Package{
				ImportPath: listedPackage.ImportPath,
				Name:       listedPackage.Name,
				Fset:       loader.fileSet,
				Types:      imported,
			})
		}
	}
	return loaded, nil
}

// canonical is the import path a package's import of path resolves to.
func canonical(importer *listedPackage, path string) string {
	if resolved, ok := importer.ImportMap[path]; ok {
		return resolved
	}
	return path
}

func (loader *loader) importExported(path string) (*types.Package, error) {
	loader.exportLock.Lock()
	defer loader.exportLock.Unlock()
	return loader.exportImporter.Import(path)
}

// check parses and type-checks the named files of one source package as packagePath, whose source imports
// are already checked, and returns it, named name, with the files it parsed: the package itself (its
// GoFiles, under its own path and name), or one of its test variants. overrides, when set, stands in for the
// packages it names, as a test variant does for the packages go test builds again.
func (loader *loader) check(
	listedPackage *listedPackage,
	packagePath, name string,
	names []string,
	overrides map[string]*types.Package,
) (*Package, []*ast.File) {
	variant := name != listedPackage.ImportPath
	checked := &Package{ImportPath: name, Name: listedPackage.Name, Fset: loader.fileSet}
	loader.parse(listedPackage, names, !variant, checked)

	info := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
	configuration := &types.Config{
		Importer: importerFunc(
			func(path string) (*types.Package, error) { return loader.resolve(listedPackage, path, overrides) },
		),
		Sizes: loader.sizes,
		Error: func(typeCheckError error) { checked.Errors = append(checked.Errors, typeCheckError) },
	}
	if listedPackage.Module != nil && listedPackage.Module.GoVersion != "" {
		configuration.GoVersion = "go" + listedPackage.Module.GoVersion
	}
	// Ignored: the error handler has every error, so Check's own return adds nothing.
	checked.Types, _ = configuration.Check(packagePath, loader.fileSet, checked.Syntax, info)
	parsed := checked.Syntax
	if !loader.roots[listedPackage.ImportPath] || variant {
		// A dependency's or a test variant's syntax and type information is read by no test, so it is not held.
		checked.Files, checked.Syntax = nil, nil
	} else {
		checked.TypesInfo = info
	}
	return checked, parsed
}

// testVariants type-checks each named package's tests as go test builds them, for every named package at once,
// as many at once as slots allows, and returns them in the named packages' order.
func (loader *loader) testVariants(sourcePackages []*listedPackage, slots chan struct{}) []*Package {
	byRoot := make([][]*Package, len(sourcePackages))
	var group sync.WaitGroup
	for index, listedPackage := range sourcePackages {
		if !loader.roots[listedPackage.ImportPath] ||
			len(listedPackage.TestGoFiles)+len(listedPackage.XTestGoFiles) == 0 {
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			byRoot[index] = loader.testVariantsOf(listedPackage)
		}()
	}
	group.Wait()
	var variants []*Package
	for _, each := range byRoot {
		variants = append(variants, each...)
	}
	return variants
}

// testVariantsOf is go test's packages for one package: "p [p.test]", its files and its in-package tests;
// each package the external tests reach that imports p, again against "p [p.test]"; and "p_test [p.test]".
func (loader *loader) testVariantsOf(listedPackage *listedPackage) []*Package {
	path := listedPackage.ImportPath
	testName := " [" + path + ".test]"
	var variants []*Package
	overrides := map[string]*types.Package{}
	if len(listedPackage.TestGoFiles) > 0 {
		inPackage, parsed := loader.check(
			listedPackage,
			path,
			path+testName,
			append(append([]string{}, listedPackage.GoFiles...), listedPackage.TestGoFiles...),
			nil,
		)
		inPackage.Errors = append(
			inPackage.Errors,
			testFunctionErrors(loader.fileSet, filesNamed(loader.fileSet, parsed, listedPackage.TestGoFiles))...,
		)
		variants = append(variants, inPackage)
		overrides[path] = inPackage.Types
	}
	if len(listedPackage.XTestGoFiles) == 0 {
		return variants
	}
	if len(overrides) > 0 {
		// go test builds again every package the external tests reach that imports p, against p's test
		// variant, so the external tests see one p: the order go list printed them in is dependencies first.
		reached := map[string]bool{}
		for _, imported := range listedPackage.XTestImports {
			imported = canonical(listedPackage, imported)
			reached[imported] = true
			if listedImport := loader.listed.byPath[imported]; listedImport != nil {
				for _, dependency := range listedImport.Deps {
					reached[dependency] = true
				}
			}
		}
		for _, between := range loader.listed.packages {
			if between.ImportPath == path ||
				!reached[between.ImportPath] ||
				loader.fromExports[between.ImportPath] ||
				!slices.Contains(between.Deps, path) {
				continue
			}
			rebuilt, _ := loader.check(
				between,
				between.ImportPath,
				between.ImportPath+testName,
				between.GoFiles,
				overrides,
			)
			variants = append(variants, rebuilt)
			overrides[between.ImportPath] = rebuilt.Types
		}
	}
	if overrides[path] == nil {
		overrides = nil
	}
	external, parsed := loader.check(
		listedPackage,
		path+"_test",
		path+"_test"+testName,
		listedPackage.XTestGoFiles,
		overrides,
	)
	external.Errors = append(external.Errors, testFunctionErrors(loader.fileSet, parsed)...)
	return append(variants, external)
}

// parse reads the named files of one package into read, each from the overlay when it names it, with what
// the parser reported, and with what go list reported when withListErrors, as the package's errors.
//
// Mutates: read gains the files, their syntax and their errors, as the package being loaded
func (loader *loader) parse(listedPackage *listedPackage, names []string, withListErrors bool, read *Package) {
	if withListErrors && listedPackage.Error != nil {
		read.Errors = append(read.Errors, errors.New(listedPackage.Error.Err))
	}
	if withListErrors {
		for _, dependencyError := range listedPackage.DepsErrors {
			read.Errors = append(read.Errors, errors.New(dependencyError.Err))
		}
	}
	for _, name := range names {
		path := filepath.Join(listedPackage.Dir, name)
		mode := parser.AllErrors | parser.ParseComments
		var file *ast.File
		var parseFileError error
		// The parser reads the file itself when handed a nil source, and a nil []byte is no nil source: it would
		// parse as an empty file. So the overlay's contents go in only when the overlay names the file.
		if replacement, ok := loader.config.Overlay[path]; ok {
			file, parseFileError = parser.ParseFile(loader.fileSet, path, replacement, mode)
		} else {
			file, parseFileError = parser.ParseFile(loader.fileSet, path, nil, mode)
		}
		if parseFileError != nil {
			read.Errors = append(read.Errors, parseFileError)
		}
		if file != nil {
			read.Files = append(read.Files, path)
			read.Syntax = append(read.Syntax, file)
		}
	}
}

// resolve is one source package's import of path: an override, a source package as checked, or export data.
func (loader *loader) resolve(
	importer *listedPackage,
	path string,
	overrides map[string]*types.Package,
) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	canonicalPath := canonical(importer, path)
	if overridden := overrides[canonicalPath]; overridden != nil {
		return overridden, nil
	}
	if loader.fromExports[canonicalPath] {
		return loader.importExported(canonicalPath)
	}
	loader.checkedLock.Lock()
	checked := loader.checked[canonicalPath]
	loader.checkedLock.Unlock()
	if checked == nil || checked.Types == nil {
		return nil, fmt.Errorf("%s imports %s, which this load did not type-check", importer.ImportPath, canonicalPath)
	}
	return checked.Types, nil
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
// (testfunctions.Problems), each as go test words it, its position first.
func testFunctionErrors(fileSet *token.FileSet, files []*ast.File) []error {
	var found []error
	for _, problem := range testfunctions.Problems(fileSet, files) {
		found = append(found, fmt.Errorf("%s: %s", problem.Position, problem.Message))
	}
	return found
}

type importerFunc func(path string) (*types.Package, error)

func (function importerFunc) Import(path string) (*types.Package, error) { return function(path) }
