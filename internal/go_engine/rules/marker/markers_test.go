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
	"unicode"

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

// A rule that reads a marker itself, cutting or testing a comment's prefix with the strings package as the Mutates,
// Reads and WaitDelay rules do, reads one of this package's constants too. A prefix that is a string ending in a colon,
// written in place or as a constant the rule's own package declares, is a marker living outside All, which the reflow
// would not see. A prefix built from one of this package's constants, as Reads' "// " + marker.Reads is, is held.
func TestEveryRuleReadingAPrefixReadsTheMarkerPackagesConstants(test *testing.T) {
	test.Parallel()
	directories, globError := filepath.Glob(filepath.Join("..", "*"))
	if globError != nil {
		test.Fatalf("listing the rule packages: %v", globError)
	}
	prefixReaders := map[string]bool{"CutPrefix": true, "HasPrefix": true, "TrimPrefix": true}
	held := 0
	for _, directory := range directories {
		if filepath.Base(directory) == "marker" {
			continue
		}
		files := parseRuleSources(test, directory)
		// A colon-ending string constant anywhere in the rule's package, by name, is a marker it declared itself.
		ownMarkers := map[string]bool{}
		for _, file := range files {
			for name := range colonConstants(file) {
				ownMarkers[name] = true
			}
		}
		for path, file := range files {
			stringsName, markerName := importNameOf(file, "strings"), markerImportName(file)
			if stringsName == "" {
				continue
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall || len(call.Args) != 2 {
					return true
				}
				selector, isSelector := call.Fun.(*ast.SelectorExpr)
				if !isSelector || !prefixReaders[selector.Sel.Name] {
					return true
				}
				if receiver, isIdentifier := selector.X.(*ast.Ident); !isIdentifier || receiver.Name != stringsName {
					return true
				}
				prefix := call.Args[1]
				if namesMarkerConstant(prefix, markerName) {
					held++
					return true
				}
				switch typed := prefix.(type) {
				case *ast.BasicLit:
					if text, unquoteError := strconv.Unquote(typed.Value); unquoteError == nil && markerShaped(text) {
						test.Errorf("%s: strings.%s reads the marker %q, which is not one of the marker package's "+
							"constants", path, selector.Sel.Name, text)
					}
				case *ast.Ident:
					if ownMarkers[typed.Name] {
						test.Errorf("%s: strings.%s reads the marker %s, a constant of the rule's own package and not "+
							"one of the marker package's", path, selector.Sel.Name, typed.Name)
					}
				}
				return true
			})
		}
	}
	// The Mutates, Reads and WaitDelay rules read their markers this way today, so finding none means the walk missed
	// them.
	if held == 0 {
		test.Errorf("found no rule reading a marker package constant through strings' prefix functions")
	}
}

// parseRuleSources parses one rule package's non-test files, by path.
func parseRuleSources(test *testing.T, directory string) map[string]*ast.File {
	test.Helper()
	paths, globError := filepath.Glob(filepath.Join(directory, "*.go"))
	if globError != nil {
		test.Fatalf("listing %s: %v", directory, globError)
	}
	files := map[string]*ast.File{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, parseError := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", path, parseError)
		}
		files[path] = file
	}
	return files
}

// colonConstants are the names of file's string constants, at any depth, whose value is shaped as a marker.
func colonConstants(file *ast.File) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		declaration, isDeclaration := node.(*ast.GenDecl)
		if !isDeclaration || declaration.Tok != token.CONST {
			return true
		}
		for _, spec := range declaration.Specs {
			value, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for index, name := range value.Names {
				if index >= len(value.Values) {
					continue
				}
				literal, isLiteral := value.Values[index].(*ast.BasicLit)
				if !isLiteral || literal.Kind != token.STRING {
					continue
				}
				if text, unquoteError := strconv.Unquote(literal.Value); unquoteError == nil && markerShaped(text) {
					names[name.Name] = true
				}
			}
		}
		return true
	})
	return names
}

// markerShaped is whether text reads as every marker in All does: after an optional "// ", a phrase of letters and
// spaces opening with a capital letter, then the colon that ends it. A directive's prefix ("//go:") and a key with
// punctuation in it are not.
func markerShaped(text string) bool {
	phrase, endsInColon := strings.CutSuffix(strings.TrimPrefix(text, "// "), ":")
	if !endsInColon || phrase == "" || !unicode.IsUpper(rune(phrase[0])) {
		return false
	}
	for _, character := range phrase {
		if !unicode.IsLetter(character) && character != ' ' {
			return false
		}
	}
	return true
}

// namesMarkerConstant is whether an expression is one of the marker package's constants, or a concatenation holding
// one, as "// " + marker.Reads is.
func namesMarkerConstant(expression ast.Expr, markerName string) bool {
	if markerName == "" {
		return false
	}
	switch typed := expression.(type) {
	case *ast.SelectorExpr:
		packageName, isIdentifier := typed.X.(*ast.Ident)
		return isIdentifier && packageName.Name == markerName
	case *ast.BinaryExpr:
		return typed.Op == token.ADD &&
			(namesMarkerConstant(typed.X, markerName) || namesMarkerConstant(typed.Y, markerName))
	case *ast.ParenExpr:
		return namesMarkerConstant(typed.X, markerName)
	}
	return false
}

// importNameOf is the name file calls the package at path by, or "" when it does not import it.
func importNameOf(file *ast.File, path string) string {
	for _, imported := range file.Imports {
		if importPath, unquoteError := strconv.Unquote(imported.Path.Value); unquoteError != nil || importPath != path {
			continue
		}
		if imported.Name != nil {
			return imported.Name.Name
		}
		return filepath.Base(path)
	}
	return ""
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
