// Package directive is vet's directive pass, a known Go toolchain directive placed or written where the toolchain
// does not take it. Of the directives only `//go:debug` is judged: it belongs in a Go source file, of package main or
// a test, above the package clause. And any `//go:` line whose verb ends in a space other than a plain one, tab or
// newline is reported whatever its verb. It reads every Go file of the package's directory, those its build
// constraints exclude too, and every other file there (assembly, C, headers) as far as their leading comments go.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/directive/directive.go and the
// helper it reads non-Go files through, internal/analysis/analyzerutil/readfile.go, whose logic it keeps and whose
// messages it reports word for word, at the same position. Like the original it reads syntax and text alone, never
// type information, and requires no other analyzer: it walks each file's comments, not its tree, so there is
// nothing for inspect to hand it. The registry runs the port in the original's place, and the original stays the
// oracle: the parity tests beside this file run both over the same packages. The two agree everywhere, so there is
// no differences fixture.
//
// It is not on the engine's syntax dispatcher, and stays a plain analyzer: it reads the pass's OtherFiles and
// IgnoredFiles, which rule.Context does not carry, and it walks no syntax tree, so there is no walk for the
// dispatcher's to replace.
package directive

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "directive",
	Doc: `check Go toolchain directives such as //go:debug

This analyzer checks for problems with known Go toolchain directives
in all Go source files in a package directory, even those excluded by
//go:build constraints, and all non-Go source files too.

For //go:debug (see https://go.dev/doc/godebug), the analyzer checks
that the directives are placed only in Go source files, only above the
package comment, and only in package main or *_test.go files.

Support for other known directives may be added in the future.

This analyzer does not check //go:build, which is handled by the
buildtag analyzer.
`,
	URL: "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/directive",
	Run: run,
}

// run checks the package's Go files, then its other files, then the files its build constraints exclude, in the
// original's order, so the findings come in the original's order too.
func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		checkGoFile(pass, file)
	}
	for _, filename := range pass.OtherFiles {
		if checkError := checkOtherFile(pass, filename); checkError != nil {
			return nil, checkError
		}
	}
	for _, filename := range pass.IgnoredFiles {
		if !strings.HasSuffix(filename, ".go") {
			if checkError := checkOtherFile(pass, filename); checkError != nil {
				return nil, checkError
			}
			continue
		}
		// An excluded Go file is in no package's syntax, so it is parsed here, into the pass's file set, where a
		// finding's position can name it. Like the original, the parse reads the file from disk rather than through
		// pass.ReadFile.
		file, parseError := parser.ParseFile(pass.Fset, filename, nil, parser.ParseComments)
		if parseError != nil {
			// A file that is not valid Go is the compiler's to diagnose, once a build includes it, so it is skipped,
			// as the original skips it.
			continue
		}
		checkGoFile(pass, file)
	}
	return nil, nil
}

// checkGoFile checks each line of each comment of a Go file, the comments above its package clause as its header.
func checkGoFile(pass *analysis.Pass, file *ast.File) {
	// The file's name as it is on disk, which decides whether it is a test, unadjusted by any `//line` directive, as
	// the original's token.File.Name is. A parsed file's package clause always has a position in the file set it was
	// parsed into.
	check := newFileChecker(pass, pass.Fset.PositionFor(file.Package, false).Filename, file)
	for _, group := range file.Comments {
		// A //go:build or a //go:debug comment is ignored after the package declaration, though one adjoining it is
		// in the header, in contrast to a +build comment. Once past the clause, every later group is too, since
		// file.Comments is in source order.
		if group.Pos() >= file.Package {
			check.inHeader = false
		}
		for _, comment := range group.List {
			check.comment(comment.Slash, comment.Text)
		}
	}
}

// checkOtherFile checks a file that is not Go source, read as raw text, since the Go parser cannot read it. An error
// reading it ends the pass, as it ends the original's.
func checkOtherFile(pass *analysis.Pass, filename string) error {
	content, tokenFile, readError := readFile(pass, filename)
	if readError != nil {
		return readError
	}
	newFileChecker(pass, filename, nil).otherFile(token.Pos(tokenFile.Base()), string(content))
	return nil
}

// readFile reads filename through the pass, or from disk when the driver offers no reader, and adds it to the pass's
// file set, so a finding can be reported at a position in it. x/tools' analyzerutil.ReadFile.
func readFile(pass *analysis.Pass, filename string) ([]byte, *token.File, error) {
	reader := pass.ReadFile
	if reader == nil {
		reader = os.ReadFile
	}
	content, readError := reader(filename)
	if readError != nil {
		return nil, nil, readError
	}
	tokenFile := pass.Fset.AddFile(filename, -1, len(content))
	tokenFile.SetLinesForContent(content)
	return content, tokenFile, nil
}
