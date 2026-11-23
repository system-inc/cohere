package rule

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
)

// syntaxOnlyPass is a pass over a package of one file tagged for go1.23, type-checked under go1.22, whose results hold
// marker's and planted's, with every field a syntax-only context drops set.
func syntaxOnlyPass(test *testing.T) *analysis.Pass {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "planted.go", "//go:build go1.23\n\npackage planted\n",
		parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	information := &types.Info{FileVersions: map[*ast.File]string{}}
	checked, checkError := (&types.Config{GoVersion: "go1.22"}).Check("example.com/planted", fileSet,
		[]*ast.File{file}, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	return &analysis.Pass{
		Pkg:        checked,
		TypesInfo:  information,
		TypesSizes: types.SizesFor("gc", "amd64"),
		Fset:       fileSet,
		Files:      []*ast.File{file},
		ReadFile:   os.ReadFile,
		ResultOf: map[*analysis.Analyzer]any{
			marker.Analyzer: "marker's result",
			plantedAnalyzer: "planted's result",
		},
	}
}

// A syntax-only context holds nothing that reads past the file it is called on (#y6q1x2j): Package, TypesInfo,
// TypesSizes, ReadFile and Files are nil, State, LookupIn and Reads panic, ResultOf answers marker and panics on
// planted though the listener requires both, and the versions a ported check's gates read are carried, read from the
// pass before it dropped them. A context of any other kind keeps them all.
func TestASyntaxOnlyContextHoldsNothingPastItsFile(test *testing.T) {
	test.Parallel()
	pass := syntaxOnlyPass(test)
	requires := []*analysis.Analyzer{marker.Analyzer, plantedAnalyzer}
	context := NewSyntaxOnlyContext(Namespace+"planted", pass, requires, &flag.FlagSet{}, generatedNone,
		func(string, analysis.Diagnostic) {
			test.Error("a context that reports nothing handed the sink a finding")
		})
	if !context.IsSyntaxOnly() {
		test.Error("a syntax-only context says it is not one")
	}
	if context.Package != nil || context.TypesInfo != nil || context.TypesSizes != nil || context.ReadFile != nil ||
		context.Files != nil || context.IsGenerated != nil {
		test.Errorf("a syntax-only context holds Package %v, TypesInfo %v, TypesSizes %v, ReadFile %t, Files %d, "+
			"IsGenerated %t, want none", context.Package, context.TypesInfo, context.TypesSizes,
			context.ReadFile != nil, len(context.Files), context.IsGenerated != nil)
	}
	if context.FileSet != pass.Fset {
		test.Error("a syntax-only context dropped the file set, which positions read")
	}
	tokenFile := pass.Fset.File(pass.Files[0].FileStart)
	if version := context.FileGoVersions[tokenFile]; version != "go1.23" || context.PackageGoVersion != "go1.22" {
		test.Errorf("the file's version is %q and the package's %q, want go1.23 and go1.22", version,
			context.PackageGoVersion)
	}
	if result := context.ResultOf(marker.Analyzer); result != "marker's result" {
		test.Errorf("ResultOf read %v for marker, which a syntax-only context answers", result)
	}
	panics := map[string]func(){
		"reads the result of analyzer planted in a syntax-only run": func() { context.ResultOf(plantedAnalyzer) },
		"reads State in a file-local run":                           func() { State[int](context) },
		"looks up Name in a syntax-only run":                        func() { context.LookupIn(nil, "Name") },
		"reads the package's reads in a syntax-only run":            func() { context.Reads() },
	}
	for saying, call := range panics {
		if recovered := panicOf(call); !strings.Contains(recovered, saying) {
			test.Errorf("want a panic saying %q, got %q", saying, recovered)
		}
	}

	ordinary := NewFileLocalContext(Namespace+"planted", pass, requires, &flag.FlagSet{}, generatedNone,
		func(string, analysis.Diagnostic) {
			test.Error("a context that reports nothing handed the sink a finding")
		})
	if ordinary.IsSyntaxOnly() || ordinary.TypesInfo == nil || ordinary.Package == nil ||
		ordinary.IsGenerated == nil || ordinary.ResultOf(plantedAnalyzer) != "planted's result" {
		test.Error("a file-local context that is not syntax-only dropped what a syntax-only one drops")
	}
}
