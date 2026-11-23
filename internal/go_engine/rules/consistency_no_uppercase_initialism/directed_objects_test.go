package consistency_no_uppercase_initialism

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_types/export_importer"
)

// checkedContextOf is a context holding source parsed and type-checked as one package.
func checkedContextOf(test *testing.T, source string) *rule.Context {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "case.go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing the case: %v", parseError)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	checked, checkError := (&types.Config{Importer: export_importer.Default()}).Check("case", fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("checking the case: %v", checkError)
	}
	return &rule.Context{Package: checked, TypesInfo: info, FileSet: fileSet, Files: []*ast.File{file}}
}

// directedObjects finds a //go:linkname's local declaration and an //export's function, and asks a comment's prefix
// before splitting it, so its allocations do not grow with the comments that are no directive (#k8e189q).
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestDirectedObjectsSplitsOnlyDirectives(test *testing.T) {
	directives := checkedContextOf(test, "package directed\n\nimport _ \"unsafe\"\n\n//export ExportedHTTP\n"+
		"func ExportedHTTP() {}\n\n//go:linkname localURL runtime.nanotime\nfunc localURL() int64\n")
	found := directedObjects(directives)
	names := map[string]bool{}
	for object := range found {
		names[object.Name()] = true
	}
	if !names["ExportedHTTP"] || !names["localURL"] || len(names) != 2 {
		test.Fatalf("want the //export and //go:linkname declarations, got %v", names)
	}
	plain := func(comments int) *rule.Context {
		return checkedContextOf(test, "package plain\n\n"+strings.Repeat("// A comment that is no directive at all.\n",
			comments)+"// Value is one.\nfunc Value() int { return 1 }\n")
	}
	one, many := plain(1), plain(200)
	oneAllocations := testing.AllocsPerRun(10, func() { directedObjects(one) })
	manyAllocations := testing.AllocsPerRun(10, func() { directedObjects(many) })
	if manyAllocations != oneAllocations {
		test.Errorf("directedObjects allocated %.0f times over one plain comment and %.0f over two hundred: it "+
			"splits comments that are no directive", oneAllocations, manyAllocations)
	}
}
