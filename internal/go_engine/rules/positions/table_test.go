package positions_test

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
)

// writtenFiles are the shapes a line table can get wrong: a `//line` directive, which the table must not follow;
// carriage returns; multibyte text before a column; a file that ends without a newline; and an empty file.
var writtenFiles = map[string]string{
	"directive.go": "package written\n\n//line elsewhere.go:100:7\nfunc Moved() int {\n\treturn 1\n}\n",
	"returns.go":   "package written\r\n\r\n// Carried is two.\r\nfunc Carried() int {\r\n\treturn 2\r\n}\r\n",
	"text.go":      "package written\n\n// Wide is a name after wide letters: \"日本語\".\nvar Wide = \"日本語\" + \"x\"\n",
	"unended.go":   "package written\n\nvar Unended = 3",
	"empty.go":     "package written\n",
}

// parsed is every file of directory and every written file, parsed into one FileSet.
func parsed(test *testing.T, directory string) (*token.FileSet, []*ast.File) {
	test.Helper()
	fileSet := token.NewFileSet()
	var syntax []*ast.File
	names, globError := filepath.Glob(filepath.Join(directory, "*.go"))
	if globError != nil || len(names) == 0 {
		test.Fatalf("finding the Go files of %s: %v, %d found", directory, globError, len(names))
	}
	written := test.TempDir()
	for name, contents := range writtenFiles {
		path := filepath.Join(written, name)
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		names = append(names, path)
	}
	for _, name := range names {
		file, parseError := parser.ParseFile(fileSet, name, nil, parser.ParseComments)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", name, parseError)
		}
		syntax = append(syntax, file)
	}
	return fileSet, syntax
}

// tokenPositions is the position of every token of file, comments included, and of its end.
func tokenPositions(test *testing.T, fileSet *token.FileSet, file *ast.File) []token.Pos {
	test.Helper()
	tokenFile := fileSet.File(file.FileStart)
	contents, readError := os.ReadFile(tokenFile.Name())
	if readError != nil {
		test.Fatal(readError)
	}
	var reading scanner.Scanner
	reading.Init(tokenFile, contents, nil, scanner.ScanComments)
	var found []token.Pos
	for {
		position, kind, _ := reading.Scan()
		if kind == token.EOF {
			break
		}
		found = append(found, position)
	}
	return append(found, token.Pos(tokenFile.Base()+tokenFile.Size()))
}

// Every token of real files and of the written shapes reads the same from the table as from
// FileSet.PositionFor(position, false): the file, offset, line and column, the end of each file included.
func TestEveryTokenReadsAsTheFileSetReadsItUnadjusted(test *testing.T) {
	test.Parallel()

	fileSet, syntax := parsed(test, filepath.Join("..", "..", "check"))
	table := positions.New(fileSet, syntax)
	compared := 0
	for _, file := range syntax {
		for _, position := range tokenPositions(test, fileSet, file) {
			want, got := fileSet.PositionFor(position, false), table.Position(position)
			if got != want {
				test.Fatalf("position %d: want %+v, got %+v", position, want, got)
			}
			if table.Line(position) != want.Line {
				test.Fatalf("position %d: want line %d, got %d", position, want.Line, table.Line(position))
			}
			compared++
		}
	}
	// A comparison that ran over nothing would pass for the wrong reason.
	if compared < 10000 {
		test.Fatalf("compared %d positions, want the check package's tokens, more than 10,000", compared)
	}
	// The comparison above is with the unadjusted FileSet, so it holds the table to the file as written; this says so
	// at the one function a directive moves.
	for _, file := range syntax {
		if filepath.Base(fileSet.File(file.FileStart).Name()) != "directive.go" {
			continue
		}
		moved := file.Decls[0].Pos()
		if adjusted := fileSet.PositionFor(moved, true); filepath.Base(adjusted.Filename) != "elsewhere.go" {
			test.Fatalf("the directive no longer moves the function, so this checks nothing: %+v", adjusted)
		}
		if got := table.Position(moved); filepath.Base(got.Filename) != "directive.go" || got.Line != 4 {
			test.Fatalf("want the function at directive.go:4 as written, got %+v", got)
		}
		return
	}
	test.Fatal("the directive file was not among those parsed")
}

// No position reads as the zero Position, and a position in a file the table was not built from, as an imported
// object's, reads from the FileSet, as every rule's lookup did before the table.
func TestAPositionOutsideTheTableReadsFromTheFileSet(test *testing.T) {
	test.Parallel()

	fileSet, syntax := parsed(test, filepath.Join("..", "..", "check"))
	table := positions.New(fileSet, syntax[:1])
	if got := table.Position(token.NoPos); got != (token.Position{}) {
		test.Fatalf("want the zero Position for no position, got %+v", got)
	}
	outside := syntax[len(syntax)-1].Package
	if want, got := fileSet.PositionFor(outside, false), table.Position(outside); got != want {
		test.Fatalf("want %+v from the FileSet, got %+v", want, got)
	}
}

// The analyzer's Result is the table of the files the pass was given.
func TestTheAnalyzersResultIsTheTableOfThePassesFiles(test *testing.T) {
	test.Parallel()

	fileSet, syntax := parsed(test, filepath.Join("..", "..", "check"))
	result, runError := positions.Analyzer.Run(&analysis.Pass{Fset: fileSet, Files: syntax})
	if runError != nil {
		test.Fatal(runError)
	}
	table, isTable := result.(*positions.Table)
	if !isTable {
		test.Fatalf("want a *positions.Table, got %T", result)
	}
	position := syntax[0].Name.Pos()
	if want, got := fileSet.PositionFor(position, false), table.Position(position); got != want {
		test.Fatalf("want %+v, got %+v", want, got)
	}
}
