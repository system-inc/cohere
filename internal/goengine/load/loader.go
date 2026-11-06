package load

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// loader parses and type-checks a graph from source, every package once its imports are, as many at once as
// there are cores, into the packages go/packages would have returned for it: the same IDs, files, imports,
// errors and type information, over one file set.
type loader struct {
	graph   *graph
	fileSet *token.FileSet
	sizes   types.Sizes

	// slots holds one token per core; a parse or a type check holds one while it runs and never waits on another.
	slots chan struct{}

	// parsed is each file parsed once, by path: a package and its test variant hold the same syntax, as under
	// go/packages.
	parsedLock sync.Mutex
	parsed     map[string]*parsedFile

	withheld *withheldErrors
}

// parsedFile is one file's parse, ready once done is closed.
type parsedFile struct {
	done       chan struct{}
	file       *ast.File
	parseError error
}

// connect materializes each package's Imports from the roots down, depth first, as go/packages does: an import
// the listing does not hold, or one that closes a cycle, becomes an import error the type checker reports at the
// import, and the graph stays acyclic. It returns every package the roots reach.
func (loader *loader) connect() []*node {
	const (
		unvisited = iota
		visiting
		visited
	)
	color := map[*node]int{}
	var stack []*node
	var reachable []*node
	var visit func(current *node)
	visit = func(current *node) {
		color[current] = visiting
		stack = append(stack, current)
		current.loaded.Imports = map[string]*packages.Package{}
		// Sorted, so which edge of a cycle is cut does not change from run to run.
		written := make([]string, 0, len(current.imports))
		for path := range current.imports {
			written = append(written, path)
		}
		slices.Sort(written)
		for _, path := range written {
			imported := loader.graph.nodes[current.imports[path]]
			var importError error
			switch {
			case imported == nil:
				importError = fmt.Errorf("missing package: %q", current.imports[path])
			case color[imported] == visiting:
				importError = fmt.Errorf("import cycle: %s", identifiersOf(stack))
			}
			if importError != nil {
				if current.importErrors == nil {
					current.importErrors = map[string]error{}
				}
				current.importErrors[path] = importError
				continue
			}
			if color[imported] == unvisited {
				visit(imported)
			}
			current.loaded.Imports[path] = imported.loaded
		}
		stack = stack[:len(stack)-1]
		color[current] = visited
		reachable = append(reachable, current)
	}
	for _, root := range loader.graph.roots {
		if color[root] == unvisited {
			visit(root)
		}
	}
	return reachable
}

// identifiersOf is the IDs of a stack of packages, as go/packages prints one in an import cycle's error.
func identifiersOf(stack []*node) string {
	identifiers := make([]string, 0, len(stack))
	for _, each := range stack {
		identifiers = append(identifiers, each.loaded.ID)
	}
	return "[" + strings.Join(identifiers, " ") + "]"
}

// load type-checks every reachable package. Each starts parsing at once and type-checks when its imports are
// done; the graph is acyclic, so none waits forever.
func (loader *loader) load(reachable []*node) {
	var group sync.WaitGroup
	for _, current := range reachable {
		group.Add(1)
		go func() {
			defer group.Done()
			defer close(current.done)
			syntax, parseErrors := loader.parseFiles(current.loaded.CompiledGoFiles)
			for _, imported := range current.loaded.Imports {
				<-loader.graph.nodes[imported.ID].done
			}
			loader.slots <- struct{}{}
			loader.check(current, syntax, parseErrors)
			<-loader.slots
		}()
	}
	group.Wait()
}

// parseFiles parses each of files, each file once however many packages hold it, and returns the syntax of those
// that parsed at all, in order, and every error. The parser goes as far as it can, so a file with errors still
// gives its syntax.
func (loader *loader) parseFiles(files []string) ([]*ast.File, []error) {
	parses := make([]*parsedFile, len(files))
	for index, file := range files {
		loader.parsedLock.Lock()
		parse, found := loader.parsed[file]
		if !found {
			parse = &parsedFile{done: make(chan struct{})}
			loader.parsed[file] = parse
		}
		loader.parsedLock.Unlock()
		parses[index] = parse
		if !found {
			go func() {
				defer close(parse.done)
				loader.slots <- struct{}{}
				defer func() { <-loader.slots }()
				parse.file, parse.parseError = loader.parseFile(file)
			}()
		}
	}
	var syntax []*ast.File
	var parseErrors []error
	for _, parse := range parses {
		<-parse.done
		if parse.file != nil {
			syntax = append(syntax, parse.file)
		}
		if parse.parseError != nil {
			parseErrors = append(parseErrors, parse.parseError)
		}
	}
	return syntax, parseErrors
}

// parseFile reads and parses one file in the mode go/packages parses in.
func (loader *loader) parseFile(file string) (*ast.File, error) {
	contents, readError := os.ReadFile(file)
	if readError != nil {
		return nil, readError
	}
	return parser.ParseFile(loader.fileSet, file, contents, parser.AllErrors|parser.ParseComments)
}

// check type-checks one package from syntax, whose imports are checked, as go/packages' loadPackage does, so its
// errors and types are the ones go/packages gave, and sets what go/packages sets. Two things differ: no cgo tool
// ran, so the type checker fakes package "C" (types.Config.FakeImportC) for the cgo files as written, and a type
// error of a dependency's that reaches its cgo files is withheld (see withheld_errors.go). The package is checked at
// its module's go version, and a package in no module, as the standard library's, at the newest.
//
// Mutates: current its package gains its syntax, types and errors
func (loader *loader) check(current *node, syntax []*ast.File, parseErrors []error) {
	loaded := current.loaded
	loaded.Fset = loader.fileSet
	loaded.TypesSizes = loader.sizes
	loaded.TypesInfo = newTypesInfo()
	if loaded.PkgPath == "unsafe" {
		loaded.Types = types.Unsafe
		loaded.Syntax = []*ast.File{}
		return
	}
	loaded.Types = types.NewPackage(loaded.PkgPath, loaded.Name)
	// A package that withholds what reaches its cgo files has its type errors judged once the check is over, since
	// TypesInfo.Uses, what they are judged by, is whole only then.
	var judged []types.Error
	appendError := func(found error) {
		var typeError types.Error
		if current.withholds && errors.As(found, &typeError) {
			judged = append(judged, typeError)
			return
		}
		loader.appendError(current, found)
	}
	for _, parseError := range parseErrors {
		appendError(parseError)
	}
	loaded.Syntax = syntax
	configuration := &types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			if imported := loaded.Imports[path]; imported != nil {
				return imported.Types, nil
			}
			if importError := current.importErrors[path]; importError != nil {
				return nil, importError
			}
			return nil, fmt.Errorf("no metadata for %s", path)
		}),
		Error:       appendError,
		Sizes:       loader.sizes,
		FakeImportC: true,
	}
	if current.listed.Module != nil && current.listed.Module.GoVersion != "" {
		configuration.GoVersion = "go" + current.listed.Module.GoVersion
	}
	checkError := types.NewChecker(configuration, loader.fileSet, loaded.Types, loaded.TypesInfo).Files(syntax)
	if len(judged) > 0 {
		reach := newCgoReach(loader.fileSet, loaded, current.listed)
		for _, typeError := range judged {
			if reach.errorReaches(syntax, typeError.Pos) {
				loader.withheld.add(loaded.PkgPath, typeError.Fset.Position(typeError.Pos), typeError.Msg)
				continue
			}
			loader.appendError(current, typeError)
		}
	}
	// Every types.Error went to appendError already; anything else the checker returned it never reported.
	var typeError types.Error
	if checkError != nil && len(loaded.Errors) == 0 && !errors.As(checkError, &typeError) {
		appendError(checkError)
	}
	if len(current.listed.CgoFiles) > 0 {
		addCgoImports(loaded)
	}
	loaded.IllTyped = len(loaded.Errors) > 0
	for _, imported := range loaded.Imports {
		loaded.IllTyped = loaded.IllTyped || imported.IllTyped
	}
}

// appendError adds one parse or type error to a package's Errors, in go/packages' words.
//
// Mutates: current its package gains the error
func (loader *loader) appendError(current *node, found error) {
	loaded := current.loaded
	var pathError *os.PathError
	var errorList scanner.ErrorList
	var typeError types.Error
	switch {
	case errors.As(found, &typeError):
		position := typeError.Fset.Position(typeError.Pos)
		loaded.TypeErrors = append(loaded.TypeErrors, typeError)
		loaded.Errors = append(loaded.Errors,
			packages.Error{Pos: position.String(), Msg: typeError.Msg, Kind: packages.TypeError})
	case errors.As(found, &errorList):
		for _, each := range errorList {
			loaded.Errors = append(loaded.Errors,
				packages.Error{Pos: each.Pos.String(), Msg: each.Msg, Kind: packages.ParseError})
		}
	case errors.As(found, &pathError):
		loaded.Errors = append(loaded.Errors,
			packages.Error{Pos: pathError.Path + ":1", Msg: pathError.Err.Error(), Kind: packages.ParseError})
	default:
		loaded.Errors = append(loaded.Errors, packages.Error{Pos: "-", Msg: found.Error(), Kind: packages.UnknownError})
	}
}

// addCgoImports makes the types of a package with cgo files import what they would had the cgo tool translated
// it, as go/packages gave them: the imports cgo adds, which its Imports hold already (see newNode), and not the
// package "C" the type checker faked. An analyzer that reads a package's imports then reads the same package from
// either load: vet's cgocall, which runs only on a package whose types import runtime/cgo, and the import graph
// every fact is carried along. On this load such a package is never the project's own, whose cgo sends the whole
// load through go/packages, so what this keeps the same is a dependency, and a package outside the workspace a
// pattern names; cgocall over one finds nothing, since its functions that reach C are withheld without their
// bodies (see withholdCgoBodies).
//
// Mutates: loaded its types' imports
func addCgoImports(loaded *packages.Package) {
	var imported []*types.Package
	for _, each := range loaded.Types.Imports() {
		if each.Path() != "C" {
			imported = append(imported, each)
		}
	}
	for _, path := range cgoImports {
		if dependency := loaded.Imports[path]; dependency != nil && !slices.Contains(imported, dependency.Types) {
			imported = append(imported, dependency.Types)
		}
	}
	loaded.Types.SetImports(imported)
}

// newTypesInfo is a types.Info with every map go/analysis reads.
func newTypesInfo() *types.Info {
	return &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
}

// newLoader is a loader for graph, with one slot per core.
func newLoader(built *graph, sizes types.Sizes, withheld *withheldErrors) *loader {
	return &loader{
		graph:    built,
		fileSet:  token.NewFileSet(),
		sizes:    sizes,
		slots:    make(chan struct{}, runtime.GOMAXPROCS(0)),
		parsed:   map[string]*parsedFile{},
		withheld: withheld,
	}
}
