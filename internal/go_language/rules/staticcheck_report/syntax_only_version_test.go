package staticcheck_report_test

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/staticcheck_report"
)

// A syntax-only context, which holds no Files, TypesInfo or Package (rule.NewSyntaxOnlyContext), answers File,
// LanguageVersion and StandardLibraryVersion at a node of the file it is walking exactly as a context holding them all
// does, from the versions it read before dropping them, under a module older than Go 1.21 and one newer, for a file
// tagged older than its module, one tagged newer and one untagged (#y6q1x2j). At a node outside the walked file it
// finds no file, as for a node in none of the package's files.
func TestASyntaxOnlyContextReadsTheVersionsAFullOneReads(test *testing.T) {
	test.Parallel()
	sources := map[string]string{
		"older.go":    "//go:build go1.19\n\npackage tagged\n\nvar Older = 1\n",
		"newer.go":    "//go:build go1.23\n\npackage tagged\n\nvar Newer = 1\n",
		"untagged.go": "package tagged\n\nvar Untagged = 1\n",
	}
	compared := 0
	for _, packageVersion := range []string{"go1.20", "go1.22"} {
		fileSet := token.NewFileSet()
		files := []*ast.File{}
		for _, name := range []string{"older.go", "newer.go", "untagged.go"} {
			file, parseError := parser.ParseFile(fileSet, name, sources[name], parser.ParseComments)
			if parseError != nil {
				test.Fatal(parseError)
			}
			files = append(files, file)
		}
		information := &types.Info{FileVersions: map[*ast.File]string{}}
		// A file tagged past its module's version is the type checker's to judge, which it may refuse; the versions
		// it recorded are what both contexts read either way, so only a check that made no package fails the test.
		checked, checkError := (&types.Config{GoVersion: packageVersion, Error: func(error) {
			// Each error is checkError's too, read below.
		}}).Check("example.com/tagged", fileSet, files, information)
		if checked == nil {
			test.Fatalf("type-checking under %s: %v", packageVersion, checkError)
		}
		pass := &analysis.Pass{Pkg: checked, TypesInfo: information, Fset: fileSet, Files: files,
			ResultOf: map[*analysis.Analyzer]any{}}
		quiet := func(string, analysis.Diagnostic) {
			// Nothing reports: the test reads versions alone.
		}
		judgeNone := func(string) bool { return false }
		full := rule.NewContext(rule.Namespace+"versioned", pass, nil, &flag.FlagSet{}, judgeNone, quiet)
		syntaxOnly := rule.NewSyntaxOnlyContext(rule.Namespace+"versioned", pass, nil, &flag.FlagSet{}, judgeNone,
			quiet)
		for index, file := range files {
			syntaxOnly.EnterFile(file, fileSet.File(file.FileStart))
			node := file.Decls[0]
			if staticcheck_report.File(syntaxOnly, node) != file {
				test.Errorf("%s under %s: the syntax-only context found no file at a node of the walked file",
					fileSet.File(file.FileStart).Name(), packageVersion)
			}
			fullLanguage, onlyLanguage := staticcheck_report.LanguageVersion(full, node),
				staticcheck_report.LanguageVersion(syntaxOnly, node)
			fullLibrary, onlyLibrary := staticcheck_report.StandardLibraryVersion(full, node),
				staticcheck_report.StandardLibraryVersion(syntaxOnly, node)
			if fullLanguage != onlyLanguage || fullLibrary != onlyLibrary || fullLanguage == "" {
				test.Errorf("%s under %s: a full context reads language %q and library %q, the syntax-only one %q "+
					"and %q", fileSet.File(file.FileStart).Name(), packageVersion, fullLanguage, fullLibrary,
					onlyLanguage, onlyLibrary)
			}
			compared++
			other := files[(index+1)%len(files)]
			if staticcheck_report.File(syntaxOnly, other.Decls[0]) != nil {
				test.Errorf("the syntax-only context walking %s found a file at a node of another",
					fileSet.File(file.FileStart).Name())
			}
		}
	}
	if compared != 6 {
		test.Errorf("compared %d files' versions, want three under each of two modules", compared)
	}
}
