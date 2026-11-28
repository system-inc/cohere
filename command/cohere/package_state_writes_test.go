package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"slices"
	"strings"
	"testing"
)

// packageStateLeftOut is each package-level variable this package's own code writes that packageState leaves out, and
// why a test needs no restore of it.
var packageStateLeftOut = map[string]string{
	"programBuilds":       "resetProgramBuilds empties the record of builds for each test that reads it, and puts it back",
	"profileFile":         "only main's run opens it, for --profile, and exitProcess closes it, and no test calls either",
	"duplicatesRequested": "the --duplicates flag, which only main's run writes, after flag.Parse, and no test calls run",
	"readinessLoaded":     "its once loads the readiness set the binary holds, the same answer for every test",
}

// packageSeams is each package-level variable of this package's own code that only tests write, swapping it on
// purpose, and how each such test restores it. A seam is never run state: no function body outside a test writes it.
var packageSeams = map[string]string{
	"newIgnoreScope":                "discovery's tests swap it with neverDescended, and put both back with test.Cleanup",
	"neverDescended":                "discovery's tests swap it with newIgnoreScope, and put both back with test.Cleanup",
	"parallelFormatWorkers":         "the fix tests set a width of format workers, and put it back by defer",
	"swiftBinaryPlatform":           "the Swift binary tests name a platform, and put it back with test.Cleanup",
	"resolveDevelopmentSwiftBinary": "the program build test swaps the resolver, and puts it back with test.Cleanup",
}

// packageStateFile is the file that declares packageState, whose capture and restore the guard reads.
const packageStateFile = "package_state_test.go"

// failedImporter refuses every import, so the guard type-checks this package's own files and reads no other: the
// checker resolves which name a write reaches by scope, which needs no other package, and an imported name it
// cannot type stays an import, never a package-level variable of this one.
type failedImporter struct{}

// Import refuses path.
func (failedImporter) Import(path string) (*types.Package, error) {
	return nil, errors.New("the guard reads no package but this one: " + path)
}

// parsedPackage is every .go file in the directory the test runs in, this package's own and nothing else: its code
// for every platform, since a file built only for another platform writes state there, and its tests, whose writes
// tell a seam.
func parsedPackage(test *testing.T, fileSet *token.FileSet) []*ast.File {
	test.Helper()
	entries, readError := os.ReadDir(".")
	if readError != nil {
		test.Fatal(readError)
	}
	var files []*ast.File
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file, parseError := parser.ParseFile(fileSet, entry.Name(), nil, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		files = append(files, file)
	}
	return files
}

// isTestFile reports whether position is in one of the package's test files.
func isTestFile(fileSet *token.FileSet, position token.Pos) bool {
	return strings.HasSuffix(fileSet.Position(position).Filename, "_test.go")
}

// checkedPackage type-checks files with no import resolved, keeping what the checker could resolve. Its errors, every
// imported name and every platform's second declaration of a name, are expected, and dropped.
func checkedPackage(fileSet *token.FileSet, files []*ast.File) (*types.Package, *types.Info) {
	information := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	configuration := types.Config{Importer: failedImporter{}, Error: func(error) {
		// Dropped: an Error function keeps the checker going past the first error, and each is one named above.
	}}
	// Ignored: every error is the expected one the comment above names, and the information holds what resolved.
	checked, _ := configuration.Check("main", fileSet, files, information)
	return checked, information
}

// writtenRootOf is the package-level variable of checked, declared in this package's own code rather than its tests,
// that a write to location lands in, through its fields, its elements, what it points to and its address, or nil when
// the location is rooted in anything else: a local, a parameter, a call's result, an import or a test's own variable.
func writtenRootOf(
	fileSet *token.FileSet,
	checked *types.Package,
	information *types.Info,
	location ast.Expr,
) *types.Var {
	current := location
	for {
		switch typed := current.(type) {
		case *ast.Ident:
			variable, isVariable := information.Uses[typed].(*types.Var)
			if isVariable && variable.Parent() == checked.Scope() && !isTestFile(fileSet, variable.Pos()) {
				return variable
			}
			return nil
		case *ast.SelectorExpr:
			current = typed.X
		case *ast.IndexExpr:
			current = typed.X
		case *ast.StarExpr:
			current = typed.X
		case *ast.ParenExpr:
			current = typed.X
		case *ast.UnaryExpr:
			if typed.Op != token.AND {
				return nil
			}
			current = typed.X
		default:
			return nil
		}
	}
}

// variableWrites is where function bodies write one package-level variable, in the package's own code and in its
// tests.
type variableWrites struct {
	fromCode  []string
	fromTests []string
}

// packageVariableWritesOf is each package-level variable of checked's own code that a function body in files writes,
// by name: an assignment to it, an increment or decrement of it, a range assigning it, or its address taken, each
// reaching through its fields and elements too. A package-level initializer writes nothing, but a function literal
// inside one is a function body like any other.
func packageVariableWritesOf(
	fileSet *token.FileSet,
	files []*ast.File,
	checked *types.Package,
	information *types.Info,
) map[string]variableWrites {
	writes := map[string]variableWrites{}
	record := func(location ast.Expr) {
		variable := writtenRootOf(fileSet, checked, information, location)
		if variable == nil {
			return
		}
		written := writes[variable.Name()]
		position := fileSet.Position(location.Pos()).String()
		if isTestFile(fileSet, location.Pos()) {
			written.fromTests = append(written.fromTests, position)
		} else {
			written.fromCode = append(written.fromCode, position)
		}
		writes[variable.Name()] = written
	}
	for _, file := range files {
		inspectBody := func(body *ast.BlockStmt) {
			ast.Inspect(body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.AssignStmt:
					// A short declaration declares its new names and assigns its old ones, which are locals of the
					// same scope, never a package-level variable.
					if typed.Tok != token.DEFINE {
						for _, written := range typed.Lhs {
							record(written)
						}
					}
				case *ast.IncDecStmt:
					record(typed.X)
				case *ast.RangeStmt:
					if typed.Tok == token.ASSIGN {
						for _, written := range []ast.Expr{typed.Key, typed.Value} {
							if written != nil {
								record(written)
							}
						}
					}
				case *ast.UnaryExpr:
					if typed.Op == token.AND {
						record(typed)
					}
				}
				return true
			})
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				if typed.Body != nil {
					inspectBody(typed.Body)
				}
				return false
			case *ast.FuncLit:
				inspectBody(typed.Body)
				return false
			}
			return true
		})
	}
	return writes
}

// stateFunctionNamed is the declaration of the function or method name in packageStateFile, one of files, or nil.
func stateFunctionNamed(fileSet *token.FileSet, files []*ast.File, name string) *ast.FuncDecl {
	for _, file := range files {
		if fileSet.Position(file.Package).Filename != packageStateFile {
			continue
		}
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Name.Name == name {
				return function
			}
		}
	}
	return nil
}

// packageVariablesIn is each package-level variable of checked's own code that node names, or, when assignedOnly, that
// an assignment under node writes, sorted, once each.
func packageVariablesIn(
	fileSet *token.FileSet,
	checked *types.Package,
	information *types.Info,
	node ast.Node,
	assignedOnly bool,
) []string {
	var names []string
	ast.Inspect(node, func(current ast.Node) bool {
		var candidates []ast.Expr
		switch typed := current.(type) {
		case *ast.AssignStmt:
			candidates = typed.Lhs
		case *ast.Ident:
			if !assignedOnly {
				candidates = []ast.Expr{typed}
			}
		}
		for _, candidate := range candidates {
			if variable := writtenRootOf(fileSet, checked, information, candidate); variable != nil {
				names = append(names, variable.Name())
			}
		}
		return true
	})
	slices.Sort(names)
	return slices.Compact(names)
}

// Every package-level variable this package's own code writes from a function body is one packageState captures and
// restores, or one packageStateLeftOut names with why no test restores it (#9b9d4x3), so a new variable a run writes
// cannot leak from one serial test into the parallel ones without the guard going red. Every other package-level
// variable a test writes is a seam packageSeams names, which the package's own code never writes. Each list holds
// only names that still are package-level variables written so, so none goes stale, and packageState's restore
// assigns each variable its capture reads. The guard reads this package's own files and nothing else, so the test
// cache keys on what it read (#0zdyk51).
func TestEveryPackageVariableARunWritesIsRestoredOrLeftOutForAReason(test *testing.T) {
	test.Parallel()

	fileSet := token.NewFileSet()
	files := parsedPackage(test, fileSet)
	checked, information := checkedPackage(fileSet, files)
	writes := packageVariableWritesOf(fileSet, files, checked, information)

	capture := stateFunctionNamed(fileSet, files, "capturePackageState")
	restore := stateFunctionNamed(fileSet, files, "restore")
	if capture == nil || restore == nil {
		test.Fatalf("%s declares no capturePackageState or no restore", packageStateFile)
	}
	captured := packageVariablesIn(fileSet, checked, information, capture.Body, false)
	restored := packageVariablesIn(fileSet, checked, information, restore.Body, true)
	if !slices.Equal(captured, restored) {
		test.Errorf("capturePackageState reads %v, and restore assigns %v", captured, restored)
	}

	for name, written := range writes {
		_, isLeftOut := packageStateLeftOut[name]
		_, isSeam := packageSeams[name]
		isCaptured := slices.Contains(captured, name)
		switch {
		case len(written.fromCode) > 0 && !isCaptured && !isLeftOut:
			test.Errorf("%s is a package-level variable written at %v, which packageState neither captures nor "+
				"packageStateLeftOut names", name, written.fromCode)
		case len(written.fromCode) == 0 && !isCaptured && !isLeftOut && !isSeam:
			test.Errorf("%s is a package-level variable only tests write, at %v, which packageSeams does not name",
				name, written.fromTests)
		}
	}
	for _, name := range captured {
		if _, isLeftOut := packageStateLeftOut[name]; isLeftOut {
			test.Errorf("%s is captured in packageState and named in packageStateLeftOut; keep one", name)
		}
		if len(writes[name].fromCode) == 0 {
			test.Errorf("packageState captures %s, which no function body of this package's code writes", name)
		}
	}
	for name := range packageStateLeftOut {
		if len(writes[name].fromCode) == 0 {
			test.Errorf("packageStateLeftOut names %s, which no function body of this package's code writes", name)
		}
	}
	for name := range packageSeams {
		switch {
		case len(writes[name].fromCode) > 0:
			test.Errorf("packageSeams names %s, which this package's code writes at %v: it is run state",
				name, writes[name].fromCode)
		case len(writes[name].fromTests) == 0:
			test.Errorf("packageSeams names %s, which no test writes as a package-level variable", name)
		}
	}
}
