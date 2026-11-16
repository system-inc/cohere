package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// typeFreeSource is a package holding what each pass typeFree names reports: a build line with a malformed Go version,
// a go:debug directive outside package main, a dot import, a package imported twice, a directive indented out of
// effect, a string holding a control character, and a statement after a return.
const typeFreeSource = "//go:build go1.x\n//go:debug panicnil=1\n\npackage typefree\n\nimport (\n\t. \"fmt\"\n" +
	"\t\"strings\"\n\tagain \"strings\"\n)\n\n// go:linkname unreached unreached\n\nvar Bell = \"a\x07b\"\n\n" +
	"func Unreached() string {\n\treturn Sprint(strings.ToUpper(again.ToLower(Bell)))\n\treturn \"\"\n}\n"

// Each pass typeFree names reads no type information (#9k8ps1y): run over a package whose pass holds of the type
// checker only the Go version it records for the file, which a port's version gate reads, no sizes, and a package that
// is its path and name alone, it neither crashes nor errors, and, but for framepointer, which judges only assembly on
// the architectures it knows, it reports what the package holds for it, so the run reached the code that reports. A
// pass that read a type, a use or a definition would find none, and report nothing or crash.
func TestEveryTypeFreePassRunsWithoutTypes(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	filename := filepath.Join(directory, "typefree.go")
	if writeError := os.WriteFile(filename, []byte(typeFreeSource), 0o600); writeError != nil {
		test.Fatal(writeError)
	}
	registered := All()
	for upstreamName := range typeFree {
		at := slices.IndexFunc(registered, func(each rule.Rule) bool {
			return each.Name == rule.Namespace+upstreamName
		})
		if at < 0 {
			test.Errorf("the type-free table names %q, which is not a registered rule", upstreamName)
			continue
		}
		analyzer := registered[at].Analyzer
		// A dispatched rule runs on a dispatcher of its own, which runs its listener alone, as the shared one would
		// with every rule's beside it.
		if registered[at].Listener != nil {
			analyzer = dispatch.NewSyntax().RuleAnalyzer(registered[at].Name, upstreamName, analyzer.Doc,
				registered[at].Listener)
		}
		reported, runError := runWithoutTypes(test, analyzer, filename)
		if runError != nil {
			test.Errorf("%s run without types: %v", upstreamName, runError)
			continue
		}
		if upstreamName != "framepointer" && len(reported) == 0 {
			test.Errorf("%s reported nothing run without types, so the run never reached what it reports", upstreamName)
		}
	}
}

// runWithoutTypes is what analyzer reports over the one file at filename, and its error, run after every analyzer it
// requires, each on a pass holding of the type checker only the file's Go version, no sizes, and a package that is
// its path and name alone.
func runWithoutTypes(test *testing.T, analyzer *analysis.Analyzer, filename string) ([]analysis.Diagnostic, error) {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, filename, nil, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	results := map[*analysis.Analyzer]any{}
	var reported []analysis.Diagnostic
	var runRequired func(each *analysis.Analyzer) error
	runRequired = func(each *analysis.Analyzer) error {
		resultOf := map[*analysis.Analyzer]any{}
		for _, required := range each.Requires {
			if _, ran := results[required]; !ran {
				if requiredError := runRequired(required); requiredError != nil {
					return requiredError
				}
			}
			resultOf[required] = results[required]
		}
		pass := &analysis.Pass{Analyzer: each, Fset: fileSet, Files: []*ast.File{file},
			Pkg:       types.NewPackage("example.com/typefree", "typefree"),
			TypesInfo: &types.Info{FileVersions: map[*ast.File]string{file: "go1.24"}}, ResultOf: resultOf,
			ReadFile: os.ReadFile,
			Report: func(diagnostic analysis.Diagnostic) {
				if each == analyzer {
					reported = append(reported, diagnostic)
				}
			}}
		result, runError := each.Run(pass)
		if runError != nil {
			return runError
		}
		results[each] = result
		return nil
	}
	if runError := runRequired(analyzer); runError != nil {
		return nil, runError
	}
	return reported, nil
}
