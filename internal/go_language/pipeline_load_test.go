package go_language

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// loadPackagesPath is package load's import path, whose loads only loadPackages may call.
const loadPackagesPath = "github.com/system-inc/cohere/internal/go_language/load"

// Every load of the workspace goes through loadPackages, which marks the go commands started from its start as late and
// gives a cached load the run's pool, account and walks, so a load added anywhere else in cohere-go would start its go
// commands unmarked and take none of them. Read from the package's source, not its tests.
func TestOnlyLoadPackagesLoads(test *testing.T) {
	test.Parallel()
	files, globError := filepath.Glob("*.go")
	if globError != nil {
		test.Fatalf("listing the package's files: %v", globError)
	}
	fileSet := token.NewFileSet()
	calls := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, parseError := parser.ParseFile(fileSet, file, nil, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", file, parseError)
		}
		name := importedAs(parsed, loadPackagesPath)
		if name == "" {
			continue
		}
		for _, declaration := range parsed.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			allowed := isFunction && function.Name.Name == "loadPackages" && function.Recv != nil
			ast.Inspect(declaration, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				selector, isSelector := call.Fun.(*ast.SelectorExpr)
				if !isSelector || !strings.HasPrefix(selector.Sel.Name, "Packages") {
					return true
				}
				if identifier, isIdentifier := selector.X.(*ast.Ident); !isIdentifier || identifier.Name != name {
					return true
				}
				calls++
				if !allowed {
					test.Errorf("%s calls load.%s outside loadPackages, so that load skips what every load of the "+
						"run shares; load through pipeline.loadPackages", fileSet.Position(call.Pos()),
						selector.Sel.Name)
				}
				return true
			})
		}
	}
	if calls == 0 {
		test.Error("found no call of package load's loads at all, so this test reads nothing")
	}
}

// importedAs is the name file refers to path by, empty when it does not import it.
func importedAs(file *ast.File, path string) string {
	for _, imported := range file.Imports {
		unquoted, unquoteError := strconv.Unquote(imported.Path.Value)
		if unquoteError != nil || unquoted != path {
			continue
		}
		if imported.Name != nil {
			return imported.Name.Name
		}
		return filepath.Base(path)
	}
	return ""
}
