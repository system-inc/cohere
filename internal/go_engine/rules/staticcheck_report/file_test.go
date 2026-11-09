//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling
package staticcheck_report_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// position is a bare position, so a test can ask about any one, a file's first and last among them.
type position token.Pos

func (position position) Pos() token.Pos {
	return token.Pos(position)
}

// TestFileMatchesTheFileSetAtEveryEdge holds File to the file set's own lookup at each file's first and last
// positions and on both sides of them, where a bound read one off would answer for a neighbor or for none. The
// package's two files sit between files of the same set that aren't the package's, so a position just outside either
// belongs to another file, and File must say nil there as the lookup does.
func TestFileMatchesTheFileSetAtEveryEdge(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	parse := func(name string, source string) *ast.File {
		file, parseError := parser.ParseFile(fileSet, name, source, parser.ParseComments)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", name, parseError)
		}
		return file
	}
	before := parse("before.go", "package other\n")
	first := parse("first.go", "// The first file starts with its comment.\npackage sample\n\nvar First = 1\n")
	second := parse("second.go", "package sample\n\nfunc Second() {}")
	after := parse("after.go", "package other\n")
	context := &rule.Context{Fset: fileSet, Files: []*ast.File{first, second}}
	// The lookup File replaces: the package's file whose token.File holds the position.
	fileSetAnswer := func(at token.Pos) *ast.File {
		nodeFile := fileSet.File(at)
		if nodeFile == nil {
			return nil
		}
		for _, file := range context.Files {
			if fileSet.File(file.Pos()) == nodeFile {
				return file
			}
		}
		return nil
	}
	positions := []token.Pos{token.NoPos}
	for _, file := range []*ast.File{before, first, second, after} {
		positions = append(positions, file.FileStart-1, file.FileStart, file.FileStart+1, file.Package,
			file.FileEnd-1, file.FileEnd, file.FileEnd+1)
	}
	describe := map[*ast.File]string{nil: "none", first: "first.go", second: "second.go"}
	answered := map[string]int{}
	for _, at := range positions {
		want := fileSetAnswer(at)
		got := staticcheck_report.File(context, position(at))
		if got != want {
			test.Errorf("File at %d (%s): got %s, the file set's lookup %s", at, fileSet.Position(at),
				describe[got], describe[want])
		}
		answered[describe[want]]++
	}
	// Each answer is met, so the comparison could tell a bound read one off from the right one.
	for _, answer := range []string{"none", "first.go", "second.go"} {
		if answered[answer] == 0 {
			test.Errorf("no position the file set places in %s", answer)
		}
	}
	if fileSetAnswer(first.FileEnd) != first || fileSetAnswer(second.FileStart) != second {
		test.Errorf("the file set doesn't place its files' own bounds in them, so the edges above test nothing")
	}
}
