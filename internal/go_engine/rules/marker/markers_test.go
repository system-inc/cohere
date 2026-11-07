package marker_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
)

// markerImportPath is this package's import path, which a rule's file names it by.
const markerImportPath = "github.com/system-inc/cohere/internal/go_engine/rules/marker"

// Every marker a rule hands this package is one of its constants, so a new rule's marker cannot live in the rule's
// own package where All, and the reflow that leaves All's lines alone, would never see it. The functions that take a
// marker are read from this package's own source, a parameter named marker, so one added later is held the same way.
func TestEveryRuleHandsTheMarkerPackageOneOfItsConstants(test *testing.T) {
	test.Parallel()
	constants, takers := readMarkerPackage(test)
	ruleFiles, globError := filepath.Glob(filepath.Join("..", "*", "*.go"))
	if globError != nil {
		test.Fatalf("listing the rule packages' files: %v", globError)
	}
	calls := 0
	for _, path := range ruleFiles {
		// The marker package's own functions hand a caller's marker on, which is no marker of their own.
		if strings.HasSuffix(path, "_test.go") || filepath.Base(filepath.Dir(path)) == "marker" {
			continue
		}
		file, parseError := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", path, parseError)
		}
		importName := markerImportName(file)
		ast.Inspect(file, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			taker, takesMarker := takers[selector.Sel.Name]
			if !takesMarker {
				return true
			}
			// A function of the package is called through its import name, and a method through any receiver.
			if !taker.isMethod {
				if receiver, isIdentifier := selector.X.(*ast.Ident); !isIdentifier || receiver.Name != importName {
					return true
				}
			}
			calls++
			if taker.markerIndex >= len(call.Args) {
				test.Errorf("%s: %s is called with no marker", path, selector.Sel.Name)
				return true
			}
			argument, isConstant := call.Args[taker.markerIndex].(*ast.SelectorExpr)
			if isConstant {
				packageName, isIdentifier := argument.X.(*ast.Ident)
				isConstant = isIdentifier && importName != "" && packageName.Name == importName &&
					constants[argument.Sel.Name] != ""
			}
			if !isConstant {
				test.Errorf("%s: %s is handed a marker that is not one of the marker package's constants",
					path, selector.Sel.Name)
			}
			return true
		})
	}
	// The rules read seven markers through Read and ExcuseFor today, so finding none means the walk missed them.
	if calls == 0 {
		test.Errorf("found no call of %v in %d rule files", slices.Sorted(maps.Keys(takers)), len(ruleFiles))
	}
}

// All lists every marker constant, each once, so a constant added for a rule is one the reflow refuses as well.
func TestAllListsEveryMarkerConstant(test *testing.T) {
	test.Parallel()
	constants, _ := readMarkerPackage(test)
	listed := map[string]int{}
	for _, text := range marker.All {
		listed[text]++
	}
	for name, text := range constants {
		if listed[text] != 1 {
			test.Errorf("marker.%s, %q, is listed in All %d times, expected once", name, text, listed[text])
		}
	}
	if len(marker.All) != len(constants) {
		test.Errorf("All lists %d markers, and the package declares %d marker constants", len(marker.All),
			len(constants))
	}
}

// markerTaker is a function or method of the marker package that takes a marker, and which of its parameters it is.
type markerTaker struct {
	isMethod    bool
	markerIndex int
}

// readMarkerPackage parses this package's own source for its exported string constants, by name, and its functions
// and methods that take a marker, by name.
func readMarkerPackage(test *testing.T) (map[string]string, map[string]markerTaker) {
	test.Helper()
	sources, globError := filepath.Glob("*.go")
	if globError != nil {
		test.Fatalf("listing the marker package's files: %v", globError)
	}
	constants := map[string]string{}
	takers := map[string]markerTaker{}
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, parseError := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", path, parseError)
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.GenDecl:
				readConstants(test, declaration, constants)
			case *ast.FuncDecl:
				index := 0
				for _, field := range declaration.Type.Params.List {
					for _, name := range field.Names {
						if name.Name == "marker" {
							takers[declaration.Name.Name] = markerTaker{
								isMethod:    declaration.Recv != nil,
								markerIndex: index,
							}
						}
						index++
					}
				}
			}
		}
	}
	return constants, takers
}

// readConstants adds declaration's exported string constants to constants, by name. The Excuse values are typed
// integers, so a string literal is what marks a marker.
//
// Mutates: constants is the map readMarkerPackage collects into.
func readConstants(test *testing.T, declaration *ast.GenDecl, constants map[string]string) {
	test.Helper()
	if declaration.Tok != token.CONST {
		return
	}
	for _, spec := range declaration.Specs {
		value, isValue := spec.(*ast.ValueSpec)
		if !isValue {
			continue
		}
		for index, name := range value.Names {
			if !name.IsExported() || index >= len(value.Values) {
				continue
			}
			literal, isLiteral := value.Values[index].(*ast.BasicLit)
			if !isLiteral || literal.Kind != token.STRING {
				continue
			}
			text, unquoteError := strconv.Unquote(literal.Value)
			if unquoteError != nil {
				test.Fatalf("reading marker.%s: %v", name.Name, unquoteError)
			}
			constants[name.Name] = text
		}
	}
}

// markerImportName is the name file calls the marker package by, or "" when it does not import it.
func markerImportName(file *ast.File) string {
	for _, imported := range file.Imports {
		if path, unquoteError := strconv.Unquote(imported.Path.Value); unquoteError != nil || path != markerImportPath {
			continue
		}
		if imported.Name != nil {
			return imported.Name.Name
		}
		return "marker"
	}
	return ""
}
