package build_dependence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// lineDirectiveCases are the files HoldsLineDirective is held to, each with whether go/scanner, as the parser applies
// it, reads a line directive in it (#mtrjfyn).
var lineDirectiveCases = []struct {
	name   string
	source string
	holds  bool
}{
	{name: "prose and a string", holds: false, source: "// Package prose says a //line directive names another " +
		"file, and /*line x:3*/ is its block spelling.\npackage prose\n\n// spelling is how one opens.\n" +
		"var spelling = []byte(\"//line \")\n\n// block is the other.\nconst block = \"/*line a.go:1*/\"\n"},
	{name: "a directive at the start of its line", holds: true, source: "package real\n\n// One is one.\n" +
		"func One() int { return 1 }\n\n//line other.go:10\n\n// Two is two.\nfunc Two() int { return 2 }\n"},
	{name: "a block directive inside a line", holds: true, source: "package block\n\n// Three is three.\n" +
		"func Three() int { return /*line x:3*/ 3 }\n"},
	{name: "an indented line comment", holds: false, source: "package indented\n\n// Four is four.\n" +
		"func Four() int {\n\t//line indented.go:4\n\treturn 4\n}\n"},
	{name: "no line number", holds: false, source: "package numberless\n\n//line numberless.go\n\n// Five is five.\n" +
		"func Five() int { return 5 }\n"},
	{name: "line zero", holds: false, source: "package zero\n\n//line zero.go:0\n\n// Six is six.\n" +
		"func Six() int { return 6 }\n"},
	{name: "a line and a column", holds: true, source: "package column\n\n//line column.go:7:2\n\n" +
		"// Seven is seven.\nfunc Seven() int { return 7 }\n"},
}

// HoldsLineDirective reads a file as go/scanner, which the parser runs, applies a line directive (#mtrjfyn): only a
// comment counts, `//line ` at the start of its line or `/*line ` anywhere, its text ending in `:N` or `:N:M` with N
// above 0. A file that says `//line` in a comment's prose and in a string literal holds none, as a directive indented
// within its line, one with no line number and one with line zero hold none; one at the start of its line mid-file,
// one in a block comment inside a line and one with a column each hold one. A file that cannot be read holds one,
// failing wide.
func TestHoldsLineDirectiveReadsAsTheScanner(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for _, each := range lineDirectiveCases {
		file := filepath.Join(directory, each.name+".go")
		if writeError := os.WriteFile(file, []byte(each.source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		if holds := HoldsLineDirective(file, os.ReadFile); holds != each.holds {
			test.Errorf("%s: HoldsLineDirective is %t, want %t", each.name, holds, each.holds)
		}
	}
	unreadable := func(string) ([]byte, error) { return nil, errors.New("unreadable") }
	if !HoldsLineDirective(filepath.Join(directory, "missing.go"), unreadable) {
		test.Error("a file that cannot be read holds no line directive, want one, failing wide")
	}
}
