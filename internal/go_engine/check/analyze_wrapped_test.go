package check

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
)

// checkerPath is the import path of the checker whose Analyze this package's tests reach only through analyzeWrapped.
const checkerPath = "github.com/system-inc/cohere/internal/go_engine/check/checker"

// analyzeWrapped is checker.Analyze of analyzers over analyzed, with every analyzer wrapped first, as Run wraps them
// before it analyzes. wrapRuns sets an analyzer's Run in place, under its mutex, the first time any run in the process
// meets it, and Analyze reads the Runs of analyzers and of everything they require with no lock. This package's tests
// run in parallel and share analyzers, so a bare Analyze beside another test's first wrap is a race, seen under -race
// on some runs only (#msx89hq, #9jkrq6h). Once wrapped, an analyzer is never written again. Every test of the package
// analyzes through here, which TestTheTestsAnalyzeOnlyThroughTheWrappingHelper holds.
func analyzeWrapped(
	analyzers []*analysis.Analyzer,
	analyzed []*packages.Package,
	options *checker.Options,
) (*checker.Graph, error) {
	if wrapError := wrapRuns(analyzers); wrapError != nil {
		return nil, wrapError
	}
	return checker.Analyze(analyzers, analyzed, options)
}

// The package's tests reach checker.Analyze through analyzeWrapped alone (#93a1a53): a bare one came back twice
// (ef4c20839, cb6660de5), each time seen only under -race, so a third fails here, at review, named by file and line.
// Each file's own name for the checker is read from its imports, so an alias or a dot import is caught as well, and
// any reach of Analyze counts, a call or a value handed on.
func TestTheTestsAnalyzeOnlyThroughTheWrappingHelper(test *testing.T) {
	test.Parallel()
	entries, readError := os.ReadDir(".")
	if readError != nil {
		test.Fatal(readError)
	}
	fileSet := token.NewFileSet()
	var bare []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, parseError := parser.ParseFile(fileSet, entry.Name(), nil, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		for _, position := range bareAnalyzes(fileSet, parsed) {
			bare = append(bare, position.String())
		}
	}
	if len(bare) > 0 {
		test.Errorf("checker.Analyze is reached outside analyzeWrapped, which wraps first, at %s",
			strings.Join(bare, ", "))
	}
}

// bareAnalyzes is the position of every reach of the checker's Analyze in parsed outside analyzeWrapped's own body,
// by each name parsed imports the checker under, its own or an alias. A dot import is named itself: its bare Analyze
// cannot be told from a local one without the types, and this package's tests have no need of one.
func bareAnalyzes(fileSet *token.FileSet, parsed *ast.File) []token.Position {
	var found []token.Position
	names := map[string]bool{}
	for _, imported := range parsed.Imports {
		path, unquoteError := strconv.Unquote(imported.Path.Value)
		if unquoteError != nil || path != checkerPath {
			continue
		}
		switch {
		case imported.Name == nil:
			names["checker"] = true
		case imported.Name.Name == ".":
			found = append(found, fileSet.Position(imported.Pos()))
		case imported.Name.Name != "_":
			names[imported.Name.Name] = true
		}
	}
	for _, declaration := range parsed.Decls {
		if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Recv == nil &&
			function.Name.Name == "analyzeWrapped" {
			continue
		}
		ast.Inspect(declaration, func(node ast.Node) bool {
			selector, isSelector := node.(*ast.SelectorExpr)
			if !isSelector || selector.Sel.Name != "Analyze" {
				return true
			}
			if qualifier, isIdentifier := selector.X.(*ast.Ident); isIdentifier && names[qualifier.Name] {
				found = append(found, fileSet.Position(selector.Pos()))
			}
			return true
		})
	}
	return found
}
