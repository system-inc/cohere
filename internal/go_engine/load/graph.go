package load

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// cgoImports are the imports the cgo tool adds to a package it translates, when go builds them.
var cgoImports = []string{"unsafe", "syscall", "runtime/cgo"}

// node is one package of the graph as go test builds it: a listed package, or one of its test variants.
type node struct {
	listed *listedPackage

	// loaded is the package as the load returns it, shaped as go/packages shapes it.
	loaded *packages.Package

	// imports are the import path each import resolves to, by the path as written: go list's own for a listed
	// package, and for a test variant the test variant of each import go test builds again.
	imports map[string]string

	// importErrors are the imports the graph could not hold, by the path as written: one missing from the listing,
	// or one that closes a cycle.
	importErrors map[string]error

	// withholds is set for a package outside the workspace with cgo files the cgo tool did not translate, whose type
	// errors and function bodies that reach them are withheld (see cgo_reach.go).
	withholds bool

	// translated is set for a package whose cgo files the cgo tool translated, so its CompiledGoFiles are the cgo
	// tool's outputs and nothing of it is faked.
	translated bool

	// done is closed once the package is type-checked.
	done chan struct{}
}

// graph is every package of the load by ID, and the roots, the packages the patterns matched with each one's test
// variants, in the listing's order.
type graph struct {
	nodes map[string]*node
	roots []*node

	// compiled is go list -compiled's answer for each package with cgo files by import path, on the load of a
	// project with cgo of its own, and nil on the load that translates nothing (see listCompiled).
	compiled map[string]*compiledPackage
}

// newGraph is the listing's packages and the test variants go test would build of each root, without go test's
// synthesized main, which is generated and imports nothing a rule reads. compiled is the cgo tool's translation of the
// packages with cgo files, or nil when nothing is translated.
func newGraph(listed *listing, workspace string, compiled map[string]*compiledPackage) *graph {
	built := &graph{nodes: map[string]*node{}, compiled: compiled}
	for _, listedPackage := range listed.packages {
		added := built.add(newNode(listedPackage, listedPackage.ImportPath, workspace,
			compiled[listedPackage.ImportPath]))
		if listed.roots[listedPackage.ImportPath] {
			built.roots = append(built.roots, added)
		}
	}
	for _, listedPackage := range listed.packages {
		hasTests := len(listedPackage.TestGoFiles)+len(listedPackage.XTestGoFiles) > 0
		if listed.roots[listedPackage.ImportPath] && hasTests {
			built.roots = append(built.roots, built.addTestVariants(listed, listedPackage, workspace)...)
		}
	}
	return built
}

// dropTestVariants leaves out every test variant newGraph built, the roots and the packages built again for a root's
// tests alike, each of which names the root it was built for (ForTest), so the graph is the listing's packages alone.
func (graph *graph) dropTestVariants() {
	graph.roots = slices.DeleteFunc(graph.roots, func(root *node) bool { return root.loaded.ForTest != "" })
	maps.DeleteFunc(graph.nodes, func(_ string, each *node) bool { return each.loaded.ForTest != "" })
}

// add puts one node in the graph, the first of an ID kept.
func (graph *graph) add(added *node) *node {
	if earlier := graph.nodes[added.loaded.ID]; earlier != nil {
		return earlier
	}
	graph.nodes[added.loaded.ID] = added
	return added
}

// newNode is a listed package as id, its files, imports and module as go list gave them, shaped as go/packages
// shapes a package: GoFiles holds the cgo files after the rest, absolute, and CompiledGoFiles, the files
// type-checked, is the same files, or, when compiled holds the cgo tool's translation of them, go list -compiled's
// files, the cgo tool's outputs among them. EmbedFiles are the package's own embedded files: go list resolves a
// test's `//go:embed` patterns only under -test, so "p [p.test]" lists p's and "p_test [p.test]" none, where
// go/packages would add the tests'. No rule reads an embedded file's bytes, and the patterns are in the test files,
// so nothing a verdict depends on is missing from a package's files.
func newNode(listedPackage *listedPackage, id string, workspace string, compiled *compiledPackage) *node {
	directory := listedPackage.Directory
	loaded := &packages.Package{
		ID:      id,
		Name:    listedPackage.Name,
		PkgPath: listedPackage.ImportPath,
		Dir:     directory,
		GoFiles: absolute(directory, listedPackage.GoFiles, listedPackage.CgoFiles),
		OtherFiles: absolute(directory, listedPackage.CFiles, listedPackage.CxxFiles, listedPackage.MFiles,
			listedPackage.HFiles, listedPackage.FFiles, listedPackage.SFiles, listedPackage.SwigFiles,
			listedPackage.SwigCxxFiles, listedPackage.SysoFiles),
		IgnoredFiles: absolute(directory, listedPackage.IgnoredGoFiles, listedPackage.IgnoredOtherFiles),
		EmbedFiles:   absolute(directory, listedPackage.EmbedFiles),
		Module:       listedPackage.Module,
	}
	if listedPackage.Error != nil {
		message := strings.TrimSpace(listedPackage.Error.Message)
		if message == "import cycle not allowed" && len(listedPackage.Error.ImportStack) > 0 {
			message += ": import stack: [" + strings.Join(listedPackage.Error.ImportStack, " ") + "]"
		}
		loaded.Errors = append(loaded.Errors,
			packages.Error{Pos: listedPackage.Error.Position, Msg: message, Kind: packages.ListError})
		addFileFromError(loaded, listedPackage, workspace)
	}
	if compiled != nil && compiled.Error != nil && listedPackage.Error == nil {
		// The cgo tool failed where go list without -compiled saw nothing wrong, so the failure is the package's own
		// error, in go/packages' words, never silent. The package is then checked as written, its package "C"
		// faked as on the load that translates nothing.
		loaded.Errors = append(loaded.Errors, packages.Error{
			Pos: compiled.Error.Position, Msg: strings.TrimSpace(compiled.Error.Message), Kind: packages.ListError,
		})
	}
	translated := compiled.translated(listedPackage)
	switch {
	case translated:
		loaded.CompiledGoFiles = compiled.CompiledGoFiles
	case loaded.PkgPath != "unsafe":
		loaded.CompiledGoFiles = loaded.GoFiles
	}
	imports := resolvedImports(listedPackage, listedPackage.Imports)
	if len(listedPackage.CgoFiles) > 0 {
		// The cgo tool adds these imports to the package it translates, and go list says only that the package
		// depends on them; go/packages, from go list -compiled, gave them as imports.
		for _, path := range cgoImports {
			if path != listedPackage.ImportPath && slices.Contains(listedPackage.Deps, path) {
				imports[path] = path
			}
		}
	}
	return &node{
		listed:  listedPackage,
		loaded:  loaded,
		imports: imports,
		// A translated package fakes nothing, so it withholds nothing.
		withholds: len(listedPackage.CgoFiles) > 0 && !translated &&
			!inside(workspace, directory+string(filepath.Separator)),
		translated: translated,
		done:       make(chan struct{}),
	}
}

// addFileFromError is go/packages' answer to a package go list could not read a file of, as one whose package
// clause does not parse: when the package has no files, the file the error names, when it exists, is the
// package's, so its parse error is reported where it is.
//
// Mutates: loaded gains the file the error names
func addFileFromError(loaded *packages.Package, listedPackage *listedPackage, workspace string) {
	stack := listedPackage.Error.ImportStack
	if len(listedPackage.GoFiles)+len(listedPackage.CgoFiles) > 0 ||
		len(stack) > 0 && stack[len(stack)-1] != listedPackage.ImportPath {
		return
	}
	for _, text := range []string{listedPackage.Error.Position, listedPackage.Error.Message} {
		file := strings.TrimSpace(strings.Split(text, ":")[0])
		if file == "" {
			continue
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(workspace, file)
		}
		if exists(file) {
			loaded.GoFiles = append(loaded.GoFiles, file)
			return
		}
	}
}

// resolvedImports is each of imported, the import paths go list resolved, by the path as written: an identity
// import is its own key, and ImportMap names the rest. A standard package's test imports go list prints resolved
// too, a vendored one as `vendor/X`, while its ImportMap names only the package's own imports, so a vendored import
// ImportMap does not name was written X. Only the standard library, which is in no module, resolves an import to
// `vendor/X`. "C" is no package.
func resolvedImports(listedPackage *listedPackage, imported []string) map[string]string {
	resolved := map[string]string{}
	for _, path := range imported {
		if path != "C" {
			resolved[path] = path
		}
	}
	for written, path := range listedPackage.ImportMap {
		if _, found := resolved[path]; found && written != path {
			delete(resolved, path)
			resolved[written] = path
		}
	}
	if listedPackage.Module == nil {
		for _, path := range imported {
			// Still its own key, so ImportMap did not name it.
			if written, vendored := strings.CutPrefix(path, "vendor/"); vendored && resolved[path] == path {
				delete(resolved, path)
				resolved[written] = path
			}
		}
	}
	return resolved
}

// addTestVariants adds what go test builds of a root with tests, and returns those to analyze. "p [p.test]" is p
// with its in-package tests, built when it has some (or is a command), its tests' files before the cgo tool's
// outputs when p is translated. Every package the tests reach that imports p is built again against it,
// "q [p.test]", so the tests see one p, from q's own compiled files; one the in-package tests import is a cycle,
// which go test refuses. "p_test [p.test]" is the external tests, against "p [p.test]" when there is one, and has
// no cgo, since go refuses import "C" in a test file.
func (graph *graph) addTestVariants(listed *listing, root *listedPackage, workspace string) []*node {
	path := root.ImportPath
	suffix := " [" + path + ".test]"
	variantOf := map[string]string{}
	var analyzed []*node

	var inPackage *node
	if len(root.TestGoFiles) > 0 || root.Name == "main" {
		withTests := *root
		withTests.GoFiles = slices.Concat(root.GoFiles, root.TestGoFiles)
		inPackage = newNode(&withTests, path+suffix, workspace, graph.compiled[path].withTests(root))
		inPackage.loaded.ForTest = path
		for written, resolved := range resolvedImports(root, root.TestImports) {
			inPackage.imports[written] = resolved
		}
		variantOf[path] = inPackage.loaded.ID
		graph.add(inPackage)
		analyzed = append(analyzed, inPackage)
	}

	// What the tests reach is everything their imports are built from; a package among it whose own imports reach
	// p is built again, dependencies first, as the listing's order has them.
	reached := map[string]bool{}
	reach := func(imported []string) {
		for _, each := range imported {
			each = canonical(root, each)
			if each == path || each == "C" {
				continue
			}
			reached[each] = true
			if listedImport := listed.byPath[each]; listedImport != nil {
				for _, dependency := range listedImport.Deps {
					reached[dependency] = true
				}
			}
		}
	}
	if inPackage != nil {
		reach(slices.Concat(root.Imports, root.TestImports))
		reach(root.XTestImports)
		cycle := false
		for written, resolved := range inPackage.imports {
			if written != path && resolved != path && slices.Contains(listedDeps(listed, resolved), path) {
				cycle = true
			}
		}
		if cycle {
			// go test names the cycle on "p [p.test]" in place of any error p has, with no position.
			inPackage.loaded.Errors = []packages.Error{
				{Msg: "import cycle not allowed in test", Kind: packages.ListError},
			}
		}
		for _, between := range listed.packages {
			if between.ImportPath == path || !reached[between.ImportPath] || !slices.Contains(between.Deps, path) {
				continue
			}
			rebuilt := newNode(between, between.ImportPath+suffix, workspace, graph.compiled[between.ImportPath])
			rebuilt.loaded.ForTest = path
			for written, resolved := range rebuilt.imports {
				if variant, found := variantOf[resolved]; found {
					rebuilt.imports[written] = variant
				}
			}
			variantOf[between.ImportPath] = rebuilt.loaded.ID
			graph.add(rebuilt)
		}
		for written, resolved := range inPackage.imports {
			if variant, found := variantOf[resolved]; found && resolved != path {
				inPackage.imports[written] = variant
			}
		}
	}

	if len(root.XTestGoFiles) > 0 {
		external := &listedPackage{
			ImportPath: path + "_test",
			Name:       root.Name + "_test",
			Directory:  root.Directory,
			Module:     root.Module,
			GoFiles:    root.XTestGoFiles,
			ImportMap:  root.ImportMap,
		}
		externalNode := newNode(external, path+"_test"+suffix, workspace, nil)
		externalNode.loaded.ForTest = path
		externalNode.imports = resolvedImports(root, root.XTestImports)
		for written, resolved := range externalNode.imports {
			if variant, found := variantOf[resolved]; found {
				externalNode.imports[written] = variant
			}
		}
		graph.add(externalNode)
		analyzed = append(analyzed, externalNode)
	}
	return analyzed
}

// listedDeps is what go list says path depends on, nothing when it did not list it.
func listedDeps(listed *listing, path string) []string {
	if listedPackage := listed.byPath[path]; listedPackage != nil {
		return listedPackage.Deps
	}
	return nil
}

// absolute is the names in each list, in order, joined to directory.
func absolute(directory string, lists ...[]string) []string {
	var joined []string
	for _, names := range lists {
		for _, name := range names {
			joined = append(joined, filepath.Join(directory, name))
		}
	}
	return joined
}
