package std_version

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// uniqueSource stands in for the standard library's unique, whose Handle and Make the table gives go1.23, so the test
// type-checks without reading the toolchain's sources.
const uniqueSource = "package unique\n\n" +
	"type Handle[T comparable] struct{ value *T }\n\n" +
	"func Make[T comparable](value T) Handle[T] { return Handle[T]{value: &value} }\n"

// usesSource uses unique's Make, too new before go1.23.
const usesSource = "package uses\n\nimport \"unique\"\n\nvar handle = unique.Make(1)\n"

// packageImporter imports each package it holds by its path.
type packageImporter map[string]*types.Package

// Import is the package at path, an error for one the importer does not hold.
func (importer packageImporter) Import(path string) (*types.Package, error) {
	if imported, isHeld := importer[path]; isHeld {
		return imported, nil
	}
	return nil, fmt.Errorf("no package %s", path)
}

// checkedAt type-checks source as the module of goVersion, beside the stand-in for unique, and runs the listener over
// it as the syntax dispatcher would, each file and then its identifiers in preorder. It returns what the rule kept and
// the messages it reported.
func checkedAt(test *testing.T, goVersion string) (*packageState, []string) {
	test.Helper()
	fileSet := token.NewFileSet()
	uniqueFile, uniqueParseError := parser.ParseFile(fileSet, "unique.go", uniqueSource, parser.ParseComments)
	if uniqueParseError != nil {
		test.Fatalf("parsing unique: %v", uniqueParseError)
	}
	unique, uniqueCheckError := (&types.Config{}).Check("unique", fileSet, []*ast.File{uniqueFile}, nil)
	if uniqueCheckError != nil {
		test.Fatalf("type-checking unique: %v", uniqueCheckError)
	}
	file, parseError := parser.ParseFile(fileSet, "uses.go", usesSource, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing the package: %v", parseError)
	}
	information := &types.Info{
		Uses:         map[*ast.Ident]types.Object{},
		FileVersions: map[*ast.File]string{},
	}
	configuration := &types.Config{GoVersion: goVersion, Importer: packageImporter{"unique": unique}}
	checked, checkError := configuration.Check("example.com/uses", fileSet, []*ast.File{file}, information)
	if checkError != nil {
		test.Fatalf("type-checking the package: %v", checkError)
	}
	pass := &analysis.Pass{Fset: fileSet, Files: []*ast.File{file}, Pkg: checked, TypesInfo: information}
	reported := []string{}
	context := rule.NewContext(passName, pass, nil, nil, nil, func(_ string, diagnostic analysis.Diagnostic) {
		reported = append(reported, diagnostic.Message)
	})
	ast.Inspect(file, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.File, *ast.Ident:
			checkNode(context, node, nil)
		}
		return true
	})
	return rule.State[packageState](context), reported
}

// A file at the newest version the table holds can use nothing too new, so its identifiers are left before the rule
// works out any package's too-new symbols, and it reports nothing. The same file at go1.22 works them out and reports
// its use of Make, so the test sees the path it skips.
func TestAFileAtTheTablesNewestVersionWorksOutNoTooNewSymbols(test *testing.T) {
	test.Parallel()
	newest, reportedAtNewest := checkedAt(test, newestTableVersion)
	if len(newest.tooNew) != 0 || len(reportedAtNewest) != 0 {
		test.Errorf("at %s the rule worked out %d packages' too-new symbols and reported %q, want none of either",
			newestTableVersion, len(newest.tooNew), reportedAtNewest)
	}
	older, reportedOlder := checkedAt(test, "go1.22")
	wanted := []string{"unique.Make requires go1.23 or later (module is go1.22)"}
	if len(older.tooNew) == 0 || !slices.Equal(reportedOlder, wanted) {
		test.Errorf("at go1.22 the rule worked out %d packages' too-new symbols and reported %q, want some and %q",
			len(older.tooNew), reportedOlder, wanted)
	}
}
