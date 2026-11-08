// Package build_tag is vet's buildtag pass, a build constraint written or placed where the go command does not take
// it, or a file's two kinds of constraint saying different things. A `//go:build` line counts above the package
// clause, and a `// +build` line only among a file's leading `//` comments with a blank line after it; one elsewhere
// is misplaced, and a second `//go:build` is one too many. Each constraint must parse, a +build line's terms must be
// letters, digits, underscores and dots under at most one negation, and a tag that looks like a Go version must be a
// valid one. A line that nearly is a constraint is reported as malformed: text holding +build early in a file that is
// not a +build line, and a `//go:build` written with a space after its slashes. In a file with both kinds, the
// //go:build line and the +build lines must say the same. It reads every Go file of the package's directory, those
// its build constraints exclude too, and every other file there (assembly, C, headers) as far as their leading
// comments go.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/buildtag/buildtag.go and the
// helper it reads non-Go files through, internal/analysis/analyzerutil/readfile.go, whose logic it keeps and whose
// messages it reports word for word, at the same position. Like the original it reads syntax and text alone, never
// type information, and requires no other analyzer: it walks each file's comments and text, not its tree, so there is
// nothing for inspect to hand it. The registry runs the port in the original's place, and the original stays the
// oracle: the parity tests beside this file run both over the same packages.
//
// It differs from the original in one place. The original stops checking a package's excluded files at the first
// excluded Go file that does not parse, so every excluded file after it goes unchecked, though its own comment there
// says only that file is to be ignored, and vet's directive pass, which has the same loop, skips just that file. The
// port skips just that file and checks the rest, so it finds what the original would find in them had the broken file
// not been there. The differences fixture holds the case.
package build_tag

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
	Name: "buildtag",
	Doc:  "check //go:build and // +build directives",
	URL:  "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/buildtag",
	Run:  run,
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
		// pass.ReadFile, and resolves no identifiers, since only the comments are read.
		file, parseError := parser.ParseFile(pass.Fset, filename, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseError != nil {
			// A file that is not valid Go is the compiler's to diagnose, once a build includes it, so it is skipped.
			// The original returns here instead, leaving every excluded file after this one unchecked: the one place
			// the port differs.
			continue
		}
		checkGoFile(pass, file)
	}
	return nil, nil
}

// checkGoFile checks each line of each comment of a Go file, and then whether its two kinds of constraint agree.
func checkGoFile(pass *analysis.Pass, file *ast.File) {
	check := newFileChecker(pass)
	for _, group := range file.Comments {
		// A +build comment is ignored after the package clause or adjoining it: a group whose end, the position just
		// past its last character, is one newline short of the clause or beyond it. Since file.Comments is in source
		// order, every later group is past it too.
		if group.End()+1 >= file.Package {
			check.plusBuildAllowed = false
		}
		// A //go:build comment is ignored after the package clause, though one adjoining it counts, unlike a +build
		// comment.
		if group.Pos() >= file.Package {
			check.goBuildAllowed = false
		}
		for _, comment := range group.List {
			// A +build comment is ignored within or after a /* */ comment.
			if !strings.HasPrefix(comment.Text, "//") {
				check.plusBuildAllowed = false
			}
			check.comment(comment.Slash, comment.Text)
		}
	}
	check.finish()
}

// checkOtherFile checks a file that is not Go source, read as raw text, since the Go parser cannot read it, and then
// whether its two kinds of constraint agree. An error reading it ends the pass, as it ends the original's.
func checkOtherFile(pass *analysis.Pass, filename string) error {
	content, tokenFile, readError := readFile(pass, filename)
	if readError != nil {
		return readError
	}
	check := newFileChecker(pass)
	check.otherFile(token.Pos(tokenFile.Base()), string(content))
	check.finish()
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
