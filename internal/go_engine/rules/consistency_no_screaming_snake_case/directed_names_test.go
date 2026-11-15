package consistency_no_screaming_snake_case

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// contextOf is a context holding source parsed as one file, with its comments.
func contextOf(test *testing.T, source string) *rule.Context {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "case.go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing the case: %v", parseError)
	}
	return &rule.Context{FileSet: fileSet, Files: []*ast.File{file}}
}

// directedNames finds each directive's name, and asks a comment's prefix before splitting it, so its allocations do not
// grow with the comments that are no directive: one plain comment and two hundred allocate alike (#k8e189q).
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestDirectedNamesSplitsOnlyDirectives(test *testing.T) {
	directives := "package directed\n\n//export Exported_name\nfunc Exported_name() {}\n\n" +
		"//go:linkname local_name runtime.nanotime\nfunc local_name() int64\n"
	found := directedNames(contextOf(test, directives))
	if !found["Exported_name"] || !found["local_name"] || len(found) != 2 {
		test.Fatalf("want the //export and //go:linkname names, got %v", found)
	}
	plain := func(comments int) *rule.Context {
		return contextOf(test, "package plain\n\n"+strings.Repeat("// A comment that is no directive at all.\n",
			comments)+"var value = 1\n")
	}
	one, many := plain(1), plain(200)
	oneAllocations := testing.AllocsPerRun(10, func() { directedNames(one) })
	manyAllocations := testing.AllocsPerRun(10, func() { directedNames(many) })
	if manyAllocations != oneAllocations {
		test.Errorf("directedNames allocated %.0f times over one plain comment and %.0f over two hundred: it splits "+
			"comments that are no directive", oneAllocations, manyAllocations)
	}
}
