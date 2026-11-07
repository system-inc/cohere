package rename

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// A declaration of another package is described the same whichever way a load read that package: from its source,
// or from cgo's translation of it in the build cache, which names its source file in //line directives. Adamic's
// first house_names pass over its cgo package bridge/tsgo/archive found runtime/cgo's Incomplete keyed by cgo.go before
// the rewrite and by a build-cache file after it, one mismatch that held every rename back.
func TestADeclarationIsDescribedByTheFileItsLineDirectivesName(test *testing.T) {
	test.Parallel()
	declaration := "package cgo\n\ntype Incomplete struct{}\n"
	described := func(filename string, source string) string {
		fileSet := token.NewFileSet()
		file, parseError := parser.ParseFile(fileSet, filename, source, parser.ParseComments)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", filename, parseError)
		}
		checked, checkError := (&types.Config{Importer: importer.Default()}).Check("runtime/cgo", fileSet,
			[]*ast.File{file}, nil)
		if checkError != nil {
			test.Fatalf("checking %s: %v", filename, checkError)
		}
		return describe(fileSet, checked.Scope().Lookup("Incomplete"))
	}
	fromSource := described("runtime/cgo/cgo.go", declaration)
	fromTranslation := described("go-build/c4/c4715bddd9998b26-d", "//line cgo.go:1\n"+declaration)
	if fromSource != fromTranslation {
		test.Errorf("the source reads %q and cgo's translation %q, which must be the same declaration's key",
			fromSource, fromTranslation)
	}
	if want := "type Incomplete @cgo.go"; fromSource != want {
		test.Errorf("described as %q, want %q", fromSource, want)
	}
}
