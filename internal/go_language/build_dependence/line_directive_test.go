package build_dependence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// lineDirectiveCases are the files HoldsLineDirective is held to, each with whether it reads a line directive in it
// (#mtrjfyn): wherever go/scanner, as the parser applies it, reads one, and wherever a comment has a directive's
// shape though the scanner rejects its text, the rule erring wide where the two could disagree.
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
	{name: "no line number", holds: true, source: "package numberless\n\n//line numberless.go\n\n// Five is five.\n" +
		"func Five() int { return 5 }\n"},
	{name: "line zero", holds: true, source: "package zero\n\n//line zero.go:0\n\n// Six is six.\n" +
		"func Six() int { return 6 }\n"},
	{name: "a line and a column", holds: true, source: "package column\n\n//line column.go:7:2\n\n" +
		"// Seven is seven.\nfunc Seven() int { return 7 }\n"},
	// The scanner reads the raw text, `x:0\r` its file and 7 its line, where the literal has the carriage return taken
	// out and reads line 0.
	{name: "a carriage return before a column", holds: true, source: "package carriage\n\n//line x:0\r:7\n\n" +
		"// Eight is eight.\nfunc Eight() int { return 8 }\n"},
	// The scanner parses a line at the machine's width and applies this one, past 32 bits.
	{name: "a line past 32 bits", holds: true, source: "package wide\n\n//line wide.go:18446744073709551615\n\n" +
		"// Nine is nine.\nfunc Nine() int { return 9 }\n"},
	{name: "a column past 32 bits", holds: true, source: "package widecolumn\n\n" +
		"//line widecolumn.go:3:18446744073709551615\n\n// Ten is ten.\nfunc Ten() int { return 10 }\n"},
	{name: "carriage return line ends", holds: true, source: "package crlf\r\n\r\n// Eleven is eleven.\r\n" +
		"func Eleven() int { return 11 }\r\n\r\n//line crlf_other.go:20\r\n\r\n// Twelve is twelve.\r\n" +
		"func Twelve() int { return 12 }\r\n"},
	{name: "an unterminated block directive at the end", holds: true, source: "package unterminated\n\n" +
		"// Thirteen is thirteen.\nfunc Thirteen() int { return 13 }\n\n/*line x:3"},
	{name: "a malformed line number", holds: true, source: "package malformed\n\n//line x:abc\n\n" +
		"// Fourteen is fourteen.\nfunc Fourteen() int { return 14 }\n"},
}

// HoldsLineDirective reads a line directive in every comment of a directive's shape (#mtrjfyn): `//line ` at the
// start of its line, or `/*line ` anywhere, whatever its text ends in, so it is never narrower than go/scanner, which
// the parser runs. A file that says `//line` in a comment's prose and in a string literal holds none, nor does one
// whose `//line` is indented within its line. One at the start of its line mid-file, in a block comment inside a
// line, with a column, after `\r\n` line ends, or unterminated at the file's end holds one, and so does each the
// scanner applies past the literal's reading, a carriage return before a column, a line or a column past 32 bits, and
// each it rejects as malformed, with no line number, line zero, or a number that is none. A file that cannot be read
// holds one, failing wide.
func TestHoldsLineDirectiveIsNeverNarrowerThanTheScanner(test *testing.T) {
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
