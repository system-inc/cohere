package load

import (
	"bytes"
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
	"sync/atomic"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// loader parses and type-checks a graph from source, every package once its imports are, as many at once as
// there are cores, into the packages go/packages would have returned for it: the same IDs, files, imports,
// errors and type information, over one file set.
type loader struct {
	graph   *graph
	fileSet *token.FileSet
	sizes   types.Sizes

	// slots holds one slot per core; a read, a parse, a type check or a read of export data holds one while it runs
	// and never waits on anything else, so nothing waits on a slot forever. They are granted longest path first, by
	// priorities.
	slots *prioritySlots

	// priorities is each reachable package's longest path to the end of the graph (see prioritize), which every slot
	// taken for its work carries. It is written before any work starts and only read after.
	priorities map[*node]int

	// observeGrant, nil outside tests, is told of every slot granted, while it is held: the ID of the package the
	// work is for, and the kind of work.
	observeGrant func(identifier string, work slotWork)

	// parsed is each file parsed once, by path: a package and its test variant hold the same syntax, as under
	// go/packages. Its lock is also held while a package reserves its files' ranges in fileSet, so no other package's
	// reservations fall between them.
	parsedLock sync.Mutex
	parsed     map[string]*parsedFile

	withheld *withheldErrors

	// overlay is the bytes a file is parsed from in place of what is on disk, by its absolute path, and held the bytes
	// the run holds of what is on disk, read in place of opening a file it holds again.
	overlay map[string][]byte
	held    Held

	// cgoOutputs are the cgo tool's outputs the load parses, by path, with the package each translates, whose
	// `//line` directives readFile points at the package's directory (see rewriteCgoLines), and rewritten the bytes
	// of each it rewrote, which every reader of the load's files is given (see Rewritten).
	cgoOutputs map[string]*listedPackage
	rewritten  *Rewritten

	// restored are the packages the findings cache holds, whose types come from their export data (see Restorer).
	restored map[*packages.Package]*Restored

	// analyzed are the packages to analyze (analyzedOfGraph), those Caching.Analyzes turns down included, and
	// testFunctions each such test variant's refused test functions, judged as it is checked, while its syntax is
	// surely there: an analysis beside the load lets an analyzed package's syntax go once it has finished with it,
	// which may be before the load ends.
	analyzed          map[*packages.Package]bool
	testFunctionsLock sync.Mutex
	testFunctions     map[*packages.Package][]Diagnostic

	// typesOnly are the dependencies whose types alone the caller reads (see PackagesWithRootSyntax): each is checked
	// with no type maps, but one that withholds, which judges its errors and withholds its declarations by them.
	typesOnly map[*packages.Package]bool

	// filesParsed counts the files parsed, and fromExportData the packages whose types were read from export data.
	filesParsed    atomic.Int64
	fromExportData atomic.Int64

	// timing is the run's collector under `--timing`, which each file's read and parse, each type check and each read
	// of export data is added to, measured on the goroutine that does it. Nil measures nothing.
	timing *timing.Collector
}

// parsedFile is one file's parse, ready once done is closed. reserved is closed once the package that claimed the file
// has reserved its range in the file set, or found it has none to reserve, since it did not read.
type parsedFile struct {
	reserved   chan struct{}
	done       chan struct{}
	file       *ast.File
	parseError error
}

// claim is a package's files as it took them: each one's parse, in the package's order, and which of them it claimed
// and so reads, reserves and parses, the rest being another package's.
type claim struct {
	files   []string
	parses  []*parsedFile
	claimed []bool
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

// load type-checks every reachable package the findings cache does not hold. Each starts parsing at once and
// type-checks when its imports are done; the graph is acyclic, so none waits forever. A package the cache holds is
// never parsed: its types are read from its export data once its imports are done, when a package checked here
// imports it, directly or through other packages the cache holds, and are never read otherwise (see needed).
func (loader *loader) load(reachable []*node) {
	needed := loader.needed(reachable)
	loader.priorities = loader.prioritize(reachable)
	// Every package checked from source claims its files before any parse starts, in reachable's order, which connect
	// fixes, so which package parses a file two packages share, as p and "p [p.test]" do, is the same every run.
	claims := map[*node]*claim{}
	for _, current := range reachable {
		if _, held := loader.restored[current.loaded]; !held {
			claims[current] = loader.claim(current.loaded.CompiledGoFiles)
		}
	}
	var group sync.WaitGroup
	for _, current := range reachable {
		if restored, held := loader.restored[current.loaded]; held {
			loader.withheld.restore(current.loaded, restored.Withheld.Errors)
			current.loaded.Fset = loader.fileSet
			current.loaded.TypesSizes = loader.sizes
			if !needed[current] {
				close(current.done)
				continue
			}
			group.Add(1)
			go func() {
				defer group.Done()
				defer close(current.done)
				for _, imported := range current.loaded.Imports {
					<-loader.graph.nodes[imported.ID].done
				}
				loader.acquire(current, slotWorkExportData)
				readError := loader.readExportData(current, restored)
				loader.slots.release()
				if readError == nil {
					return
				}
				// Export data that does not read is a package checked from source after all, its imports done. It
				// claims its files only now, after every package checked from source, so it waits only on claims
				// made before its own.
				syntax, parseErrors := loader.parseClaimed(loader.claim(current.loaded.CompiledGoFiles), current)
				loader.acquire(current, slotWorkCheck)
				loader.check(current, syntax, parseErrors)
				loader.slots.release()
			}()
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			defer close(current.done)
			syntax, parseErrors := loader.parseClaimed(claims[current], current)
			for _, imported := range current.loaded.Imports {
				<-loader.graph.nodes[imported.ID].done
			}
			loader.acquire(current, slotWorkCheck)
			loader.check(current, syntax, parseErrors)
			loader.slots.release()
		}()
	}
	group.Wait()
}

// needed is the packages whose types the load sets: each it checks from source, and each the cache holds that one
// of those imports, directly or through other packages the cache holds.
func (loader *loader) needed(reachable []*node) map[*node]bool {
	needed := map[*node]bool{}
	var need func(current *node)
	need = func(current *node) {
		if needed[current] {
			return
		}
		needed[current] = true
		for _, imported := range current.loaded.Imports {
			need(loader.graph.nodes[imported.ID])
		}
	}
	for _, current := range reachable {
		if _, held := loader.restored[current.loaded]; !held {
			need(current)
		}
	}
	return needed
}

// prioritize is each reachable package's priority: its cost plus the largest priority of the packages that import it,
// so a slot goes first to the work the longest chain of what is still to load waits behind. reachable is connect's
// postorder, every importer after what it imports, so walked backwards each package's importers are done before it.
// A package's cost is the files it parses when checked from source, and 1 when its types are read from export data,
// which stands for how long its work holds slots without a stat of every file, which would sit on the very path this
// shortens. A restored package whose export data does not read is checked at its restored priority.
func (loader *loader) prioritize(reachable []*node) map[*node]int {
	priorities := make(map[*node]int, len(reachable))
	// longestAfter is the largest priority among each package's importers seen so far.
	longestAfter := make(map[*node]int, len(reachable))
	for index := len(reachable) - 1; index >= 0; index-- {
		current := reachable[index]
		cost := len(current.loaded.CompiledGoFiles)
		if _, held := loader.restored[current.loaded]; held {
			cost = 1
		}
		priorities[current] = cost + longestAfter[current]
		for _, imported := range current.loaded.Imports {
			importedNode := loader.graph.nodes[imported.ID]
			longestAfter[importedNode] = max(longestAfter[importedNode], priorities[current])
		}
	}
	return priorities
}

// acquire takes a slot for work on current's behalf, at its priority, and tells observeGrant once it holds it.
func (loader *loader) acquire(current *node, work slotWork) {
	loader.slots.acquire(loader.priorities[current])
	if loader.observeGrant != nil {
		loader.observeGrant(current.loaded.ID, work)
	}
}

// readExportData sets a package's types from the export data the findings cache holds, against the types its
// imports already have: every package its export data names is among its imports, transitively, and is put in the
// importer's map first, so each object is the one that package declares, and every package of the run has exactly
// one *types.Package. The package's own imports are set to the ones its type check had, in its order, since export
// data names every package it reaches.
//
// Mutates: current its package gains its types
func (loader *loader) readExportData(current *node, restored *Restored) error {
	loaded := current.loaded
	if loaded.PkgPath == "unsafe" {
		loaded.Types = types.Unsafe
		return nil
	}
	imports := map[string]*types.Package{}
	var collect func(importer *packages.Package)
	collect = func(importer *packages.Package) {
		for _, imported := range importer.Imports {
			if _, collected := imports[imported.PkgPath]; collected || imported.Types == nil {
				continue
			}
			imports[imported.PkgPath] = imported.Types
			collect(imported)
		}
	}
	collect(loaded)
	own := types.NewPackage(loaded.PkgPath, loaded.Name)
	imports[loaded.PkgPath] = own
	var read *types.Package
	var readError error
	exportDataCpu := loader.timing.Measure(func() {
		exportData, exportError := restored.ReadExportData()
		if exportError != nil {
			readError = exportError
			return
		}
		read, readError = gcexportdata.Read(bytes.NewReader(exportData), loader.fileSet, imports, loaded.PkgPath)
	})
	if readError != nil {
		return fmt.Errorf("reading the export data of %s: %w", loaded.ID, readError)
	}
	ownImports := make([]*types.Package, 0, len(restored.Imports))
	for _, path := range restored.Imports {
		imported, found := imports[path]
		if !found {
			return fmt.Errorf("reading the export data of %s: it imports %s, which none of its imports is", loaded.ID,
				path)
		}
		ownImports = append(ownImports, imported)
	}
	read.SetImports(ownImports)
	loaded.Types = read
	loader.fromExportData.Add(1)
	// Added only once the types are set, so a package whose export data did not read is counted with those checked
	// from source, and its read's CPU is no row's.
	loader.timing.ExportData(exportDataCpu)
	return nil
}

// claim takes each of files no package has taken yet, under one hold of the lock, so a package's claim never
// interleaves with another's: every file of an earlier claim is that claim's or an even earlier one's, which is why a
// claim waiting on the reservations of files others claimed (see parseClaimed) never waits on itself.
func (loader *loader) claim(files []string) *claim {
	taken := &claim{files: files, parses: make([]*parsedFile, len(files)), claimed: make([]bool, len(files))}
	loader.parsedLock.Lock()
	defer loader.parsedLock.Unlock()
	for index, file := range files {
		parse, found := loader.parsed[file]
		if !found {
			parse = &parsedFile{reserved: make(chan struct{}), done: make(chan struct{})}
			loader.parsed[file] = parse
			taken.claimed[index] = true
		}
		taken.parses[index] = parse
	}
	return taken
}

// parseClaimed parses the files a claim took for current, each file once however many packages hold it, and returns
// the syntax of the claim's files that parsed at all, in order, and every error. Each read and parse takes its slot at
// current's priority. The parser goes as far as it can, so a file with errors still gives its syntax.
//
// A file's base in the file set is fixed when its range is added, and a check that compares positions across a
// package's files, or orders findings by position, reads which file sorts first from the bases. So the claimed files
// are read in parallel, then their ranges reserved in the package's order once every file of the claim another
// package took has its own, and only then parsed, in parallel, each into the range reserved for it. Their bases then
// follow the claim's order, and the files others took come first, whatever order the reads and parses finish in.
func (loader *loader) parseClaimed(taken *claim, current *node) ([]*ast.File, []error) {
	contents := make([][]byte, len(taken.files))
	readErrors := make([]error, len(taken.files))
	var reads sync.WaitGroup
	for index, file := range taken.files {
		if !taken.claimed[index] {
			continue
		}
		loader.filesParsed.Add(1)
		reads.Go(func() {
			loader.acquire(current, slotWorkRead)
			defer loader.slots.release()
			// The file is counted here, once, as filesParsed counts it, and its parse below adds CPU alone.
			loader.timing.Parse(loader.timing.Measure(func() {
				contents[index], readErrors[index] = loader.readFile(file)
			}), 1)
		})
	}
	reads.Wait()
	for index, parse := range taken.parses {
		if !taken.claimed[index] {
			<-parse.reserved
		}
	}
	placeholders := make([]*token.File, len(taken.files))
	loader.parsedLock.Lock()
	for index, file := range taken.files {
		if taken.claimed[index] && readErrors[index] == nil {
			placeholders[index] = loader.fileSet.AddFile(file, -1, len(contents[index]))
		}
	}
	loader.parsedLock.Unlock()
	for index, parse := range taken.parses {
		if !taken.claimed[index] {
			continue
		}
		close(parse.reserved)
		if readErrors[index] != nil {
			parse.parseError = readErrors[index]
			close(parse.done)
			continue
		}
		go func() {
			defer close(parse.done)
			loader.acquire(current, slotWorkParse)
			defer loader.slots.release()
			loader.timing.Parse(loader.timing.Measure(func() {
				parse.file, parse.parseError = loader.parseInto(
					taken.files[index],
					contents[index],
					placeholders[index],
				)
			}), 0)
		}()
	}
	var syntax []*ast.File
	var parseErrors []error
	for _, parse := range taken.parses {
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

// readFile is one file's bytes: the overlay's when it holds the file, else the ones the run holds of it, else the
// disk's. A cgo output's `//line` directives that name a copy of the package elsewhere are pointed at the package's
// own directory first, in a copy, never in the build cache (see rewriteCgoLines), and the copy is kept in rewritten,
// so a rule that reads the file reads the bytes its syntax was parsed from.
func (loader *loader) readFile(file string) ([]byte, error) {
	if contents, overlaid := loader.overlay[file]; overlaid {
		return contents, nil
	}
	contents, readError := loader.held.read(file)
	if listedPackage := loader.cgoOutputs[file]; listedPackage != nil && readError == nil {
		if parsed, isRewritten := rewriteCgoLines(contents, listedPackage); isRewritten {
			loader.rewritten.add(file, parsed)
			return parsed, nil
		}
	}
	return contents, readError
}

// parseInto parses one file's contents in the mode go/packages parses in, into the range placeholder reserved for it
// in the file set. The parse runs in a file set of its own, whose next base is the reservation's: go/token gives the
// first file of a new set base 1 and the file after one of size n the base after it plus n plus 1, so a first file of
// size base-2 leaves the next at exactly base, and a base of 1 needs none. The parsed file then takes the
// placeholder's place.
//
// Between the removal and the add the range belongs to no file, which no reader can see: no position in it exists
// until the parse that returns here is published, by the file's done, and every other file the set adds, as export
// data's, takes a base past every reservation, since removing a file never lowers the set's next base.
func (loader *loader) parseInto(file string, contents []byte, placeholder *token.File) (*ast.File, error) {
	own := token.NewFileSet()
	if placeholder.Base() >= 2 {
		own.AddFile("", -1, placeholder.Base()-2)
	}
	syntax, parseError := parser.ParseFile(own, file, contents, parser.AllErrors|parser.ParseComments)
	parsed := own.File(token.Pos(placeholder.Base()))
	if parsed == nil || parsed.Base() != placeholder.Base() || parsed.Size() != placeholder.Size() {
		return nil, fmt.Errorf("parsing %s: its file did not take the range reserved for it, base %d", file,
			placeholder.Base())
	}
	loader.fileSet.RemoveFile(placeholder)
	loader.fileSet.AddExistingFiles(parsed)
	return syntax, parseError
}

// check type-checks one package from syntax, whose imports are checked, as go/packages' loadPackage does, so its
// errors and types are the ones go/packages gave, and sets what go/packages sets. A translated package is exactly
// that: its syntax is the cgo tool's outputs. One the cgo tool did not translate differs in two things: the type
// checker fakes package "C" (types.Config.FakeImportC) for the cgo files as written, and a type error of a
// dependency's that reaches its cgo files is withheld (see withheld_errors.go). The package is checked at its
// module's go version, and a package in no module, as the standard library's, at the newest.
//
// Mutates: current its package gains its syntax, types and errors
func (loader *loader) check(current *node, syntax []*ast.File, parseErrors []error) {
	loaded := current.loaded
	loaded.Fset = loader.fileSet
	loaded.TypesSizes = loader.sizes
	if loader.typesOnly[loaded] && !current.withholds {
		loaded.TypesInfo = &types.Info{}
	} else {
		loaded.TypesInfo = newTypesInfo()
	}
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
		Importer: importerFunction(func(path string) (*types.Package, error) {
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
	var checkError error
	loader.timing.TypeCheck(loader.timing.Measure(func() {
		checkError = types.NewChecker(configuration, loader.fileSet, loaded.Types, loaded.TypesInfo).Files(syntax)
	}))
	if len(judged) > 0 {
		reach := newCgoReach(loader.fileSet, loaded, current.listed)
		for _, typeError := range judged {
			if reach.errorReaches(syntax, typeError.Pos) {
				loader.withheld.add(loaded, typeError.Fset.Position(typeError.Pos), typeError.Msg)
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
	// A translated package's types import what the cgo tool's outputs import already.
	if len(current.listed.CgoFiles) > 0 && !current.translated {
		addCgoImports(loaded)
	}
	loaded.IllTyped = len(loaded.Errors) > 0
	for _, imported := range loaded.Imports {
		loaded.IllTyped = loaded.IllTyped || imported.IllTyped
	}
	if loaded.ForTest != "" && loader.analyzed[loaded] {
		refused := testFunctionDiagnostics(loaded)
		loader.testFunctionsLock.Lock()
		loader.testFunctions[loaded] = refused
		loader.testFunctionsLock.Unlock()
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

// addCgoImports makes the types of a package with cgo files the cgo tool did not translate import what they would
// had it translated them, as go/packages gave them: the imports cgo adds, which its Imports hold already (see
// newNode), and not the package "C" the type checker faked. An analyzer that reads a package's imports then reads
// the same package from either load: vet's cgocall, which runs only on a package whose types import runtime/cgo,
// and the import graph every fact is carried along. Such a package is the project's own only when the cgo tool
// failed on it, a diagnostic already, since the project's own cgo has every package with cgo files translated, so
// what this keeps the same is a dependency on the load that translates nothing, and a package outside the workspace
// a pattern names; cgocall over one finds nothing, since its functions that reach C are withheld without their
// bodies (see withholdCgoDeclarations).
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

// newLoader is a loader for graph, with one slot per core, parsing each file in overlay from its bytes there and each
// other file held holds from its held bytes, and adding what it measures to collector.
func newLoader(
	built *graph,
	sizes types.Sizes,
	withheld *withheldErrors,
	overlay map[string][]byte,
	held Held,
	collector *timing.Collector,
) *loader {
	return &loader{
		graph:         built,
		fileSet:       token.NewFileSet(),
		sizes:         sizes,
		slots:         newPrioritySlots(runtime.GOMAXPROCS(0)),
		parsed:        map[string]*parsedFile{},
		withheld:      withheld,
		overlay:       overlay,
		held:          held,
		cgoOutputs:    cgoOutputsOf(built),
		rewritten:     newRewritten(),
		analyzed:      map[*packages.Package]bool{},
		testFunctions: map[*packages.Package][]Diagnostic{},
		typesOnly:     map[*packages.Package]bool{},
		timing:        collector,
	}
}
