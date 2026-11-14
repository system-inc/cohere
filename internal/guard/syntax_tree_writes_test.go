package guard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// No rule writes the TypeScript syntax tree, and this is what makes that structural rather than remembered.
//
// The tree is shared with every checker. While a rule's listener runs on one goroutine, a checker type-checking
// another file on another goroutine reads the same nodes, so a write of any kind is a data race, a temporary one
// put back after the call too. no-dupe-class-members once swapped a class's member list for one without its
// computed members for the length of its listener, on the reasoning that the walk is single-threaded per file,
// and a checker that resolved the class during the swap kept it without its `*[Symbol.iterator]()` for the run:
// false TS2488 in 2 of 60 cold runs of loom (#72kn826). This holds every rule to computing on a copy (#qb5xxj1).
//
// It reads every package under internal/lint from source with its types: the rules, the rule interface, and
// every shelf a rule calls with a node (checking, ecmascript and the rest). A helper is scanned where it is
// written, so a write in a helper a rule calls is named at the helper, whoever calls it. What it reports:
//
//   - an assignment, op-assignment, `++` or `--`, or a range clause assigning, whose target is reached through a
//     field of a struct the ast package declares (`node.Members = ...`, `list.Nodes[i] = ...`, `symbol.Flags |=`),
//     or is an element or target whose type is the tree's (`nodes[i] = ...` over a `[]*ast.Node`, `*list = ...`
//     through a `*ast.NodeList`, an entry of an `ast.SymbolTable`);
//   - the address of such a place, taken explicitly or by calling a pointer method on it, since whoever holds the
//     address can write it: the old swap took `&declaration.Members` and wrote through a `**ast.NodeList`, which
//     reads like a write to any variable, so it is the address that is named. The address of one of the ast
//     package's structs (`&statement.Node`) is the node itself, written only through its fields, and is not;
//   - copy into, append onto, or an in-place sort, reversal, deletion or insertion of such a slice;
//   - a call to one of the ast package's writing methods or functions (syntaxTreeWriters).
//
// A write is the rule's own, and not reported, when its storage is a local variable's: either the variable itself
// with no pointer, slice or map followed on the way (a value copy), or, for a variable that only ever holds what
// the function built (fresh: a composite literal, make, new, append onto a fresh value, slices.Clone, a node
// factory's New, a call to a function of the package that only ever returns what it built), the one allocation it
// holds and nothing reached through that. A parameter is never fresh, since the caller decides what it holds, and
// neither is a variable whose address is taken, since what the address is handed to decides.
//
// Known limits: a call through a function value or an interface is not followed, and is only seen where the
// function it reaches is written in internal/lint; a write inside the TypeScript packages a rule calls (the
// checker's own caches) is the checker's to synchronize and is not looked at; a fresh allocation's field that is
// handed a slice the tree owns, then written through, reads as the rule's own; and freshness is not followed into
// a parameter, so a helper writing a slice every caller built is reported until it is handed a copy.
// TestEveryRuleWalksBesideReadersOfTheSameTreeAtOnce (internal/types/program) runs every rule beside a reader of
// the tree under -race, which catches a write wherever it is written.
func TestRulesNeverWriteTheSyntaxTree(test *testing.T) {
	test.Parallel()

	root, absolutePathError := filepath.Abs("../..")
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	scan := syntaxTreeWriteScan{root: root}
	for _, lintPackage := range lintPackages(test, guardOverlay(test)) {
		if len(lintPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", lintPackage.ImportPath, lintPackage.Errors[0])
		}
		scan.read(lintPackage.FileSet, lintPackage.TypesInfo, lintPackage.Syntax)
	}
	// About 650 files under internal/lint name the tree today. A scan that saw a handful lost the packages, or
	// stopped resolving the ast package's types, and would pass on nothing.
	if scan.treeFiles < 300 {
		test.Fatalf("the scan read %d files, %d of them naming the syntax tree's types, too few to be every rule",
			scan.files, scan.treeFiles)
	}
	for _, site := range scan.writes {
		test.Errorf("%s: the syntax tree is shared with every checker, which reads it on other goroutines while "+
			"the rules run, so compute on a copy and leave the tree as the parser made it (#72kn826)", site)
	}
}

// The scan's positive control: a fixture holding the shapes of tree write it reports, each marked with what it must
// say, among writes to the function's own copies it must not report. Type-checked against the same ast package the
// rules are, so it cannot pass by resolving nothing.
func TestTheSyntaxTreeWriteScanNamesTheFixturesWrites(test *testing.T) {
	test.Parallel()

	root, absolutePathError := filepath.Abs("../..")
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	paths, globError := filepath.Glob(filepath.Join("testdata", "syntax_tree_writes", "*.go"))
	if globError != nil {
		test.Fatal(globError)
	}
	if len(paths) == 0 {
		test.Fatal("no fixture under testdata/syntax_tree_writes, so the scan was checked against nothing")
	}

	var loadedTypes []*types.Package
	for _, lintPackage := range lintPackages(test, guardOverlay(test)) {
		loadedTypes = append(loadedTypes, lintPackage.Types)
	}
	imported := importsOf(loadedTypes)
	fileSet := token.NewFileSet()
	for _, path := range paths {
		source, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		file, parseError := parser.ParseFile(fileSet, path, source, parser.ParseComments)
		if parseError != nil {
			test.Fatal(parseError)
		}
		info := &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Defs:       map[*ast.Ident]types.Object{},
			Uses:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
			Implicits:  map[ast.Node]types.Object{},
		}
		configuration := types.Config{Importer: packageImporter(imported)}
		if _, checkError := configuration.Check("fixture", fileSet, []*ast.File{file}, info); checkError != nil {
			test.Fatalf("type-checking %s: %v", path, checkError)
		}

		wanted := fixtureWants(source)
		if len(wanted) == 0 {
			test.Fatalf("%s marks no write the scan must name, so it proves nothing", path)
		}
		scan := syntaxTreeWriteScan{root: root}
		scan.read(fileSet, info, []*ast.File{file})
		if scan.treeFiles != 1 {
			test.Fatalf("%s did not read as naming the syntax tree's types", path)
		}
		found := map[int]string{}
		for _, site := range scan.writes {
			line, description := siteLine(test, site)
			found[line] = description
		}
		for line, want := range wanted {
			if found[line] != want {
				test.Errorf("%s:%d: the scan said %q, want %q", path, line, found[line], want)
			}
		}
		for line, description := range found {
			if _, marked := wanted[line]; !marked {
				test.Errorf("%s:%d: the scan named %q where the fixture writes only its own copy", path, line,
					description)
			}
		}
	}
}

// fixtureWant is a fixture line's `// want: ...` mark, what the scan must say of the line.
var fixtureWant = regexp.MustCompile(`// want: (.+)$`)

// fixtureWants is every marked line of a fixture, by line number, with what the scan must say of it.
func fixtureWants(source []byte) map[int]string {
	wanted := map[int]string{}
	for index, line := range strings.Split(string(source), "\n") {
		if match := fixtureWant.FindStringSubmatch(line); match != nil {
			wanted[index+1] = match[1]
		}
	}
	return wanted
}

// siteLine splits a reported site, `path:line what`, into its line and what it says.
func siteLine(test *testing.T, site string) (int, string) {
	test.Helper()
	location, description, found := strings.Cut(site, " ")
	if !found {
		test.Fatalf("the site %q names no write", site)
	}
	line, parseError := strconv.Atoi(location[strings.LastIndex(location, ":")+1:])
	if parseError != nil {
		test.Fatalf("the site %q names no line: %v", site, parseError)
	}
	return line, description
}

// importsOf is every package the packages import, transitively, by path.
func importsOf(packages []*types.Package) map[string]*types.Package {
	imported := map[string]*types.Package{}
	var collect func(from *types.Package)
	collect = func(from *types.Package) {
		if from == nil {
			return
		}
		for _, dependency := range from.Imports() {
			if imported[dependency.Path()] == nil {
				imported[dependency.Path()] = dependency
				collect(dependency)
			}
		}
	}
	for _, from := range packages {
		collect(from)
	}
	return imported
}

// packageImporter imports from packages already type-checked, by path.
type packageImporter map[string]*types.Package

func (importer packageImporter) Import(path string) (*types.Package, error) {
	if imported := importer[path]; imported != nil {
		return imported, nil
	}
	return nil, fmt.Errorf("no package %s was loaded with the rule packages", path)
}

// syntaxTreePackages are the import paths of typescript-go's ast package: its own, where every node type and
// every struct the tree holds is declared, and the shim cohere imports it through, which declares only aliases.
var syntaxTreePackages = map[string]bool{
	"github.com/microsoft/TypeScript/tsc/internal/ast": true,
	"github.com/microsoft/TypeScript/tsc/shim/ast":     true,
}

// syntaxTreeWriters are the ast package's methods and functions that write what they are handed, by name, found by
// reading the package for every one that assigns a field of its receiver or argument without a lock of its own
// (#qb5xxj1). The lazy getters that fill a cache under a once or a mutex (GetDeclarationMap, GetNameTable,
// ECMALineMap, GetOrCreateToken and the like) are synchronized by the package itself and not here. AsMutable is the
// door to every MutableNode setter, and a rule has no reason to open it.
var syntaxTreeWriters = map[string]bool{
	"AsMutable":                  true,
	"SetBindDiagnostics":         true,
	"SetCategory":                true,
	"SetContentMapperInfo":       true,
	"SetDiagnostics":             true,
	"SetExpression":              true,
	"SetExternalData":            true,
	"SetExternalModuleIndicator": true,
	"SetFile":                    true,
	"SetHasLazyJSDoc":            true,
	"SetImportsOfSourceFile":     true,
	"SetInitializer":             true,
	"SetJSDiagnostics":           true,
	"SetJSDocCache":              true,
	"SetJSDocDiagnostics":        true,
	"SetLocation":                true,
	"SetMessageChain":            true,
	"SetModifiers":               true,
	"SetParentInChildren":        true,
	"SetParseJSDocForNode":       true,
	"SetRelatedInfo":             true,
	"SetRepopulateInfo":          true,
	"SetSkippedOnNoEmit":         true,
	"SetType":                    true,
}

// inPlaceSliceFunctions are the standard library's functions that write the slice they are handed as their first
// argument, by package path and name.
var inPlaceSliceFunctions = map[string]bool{
	"slices.Compact":        true,
	"slices.CompactFunc":    true,
	"slices.Delete":         true,
	"slices.DeleteFunc":     true,
	"slices.Insert":         true,
	"slices.Replace":        true,
	"slices.Reverse":        true,
	"slices.Sort":           true,
	"slices.SortFunc":       true,
	"slices.SortStableFunc": true,
	"sort.Slice":            true,
	"sort.SliceStable":      true,
}

// freshFunctions are the standard library's functions that return a new slice or map, by package path and name.
var freshFunctions = map[string]bool{"maps.Clone": true, "slices.Clone": true}
