// Package staticcheck_wants reads the findings staticcheck's own fixtures expect, so a port of one of staticcheck's
// checks proves itself against the fixture the original is tested on, copied unchanged. Those fixtures write what
// they expect as notes, `//@ diag("text")` or `//@ diag(re"pattern")` on the line of the finding, which staticcheck
// reads with x/tools' go/expect, and the house's analysistest, reading `// want` comments alone, does not. go/expect
// lives on in x/tools only as an internal package, and as a deprecated module cohere does not require.
//
// The notes are read as go/expect v0.1.1 (golang.org/x/tools/go/expect@v0.1.1-deprecated extract.go, the version
// staticcheck v0.8.1 pins) reads them, which comments hold notes and how notes are separated, and matched as
// staticcheck v0.8.1's analysis/lint/testutil/check.go matches them: a string is a substring of the message, a
// re"..." a regular expression it matches. Read knows diag notes alone, each of one argument, and refuses any other
// note, a fact note among them, rather than leaving it unchecked, so a port whose original exports no facts never
// passes over one. ReadWithFacts reads fact notes too, `//@ fact(name, "text")` on the line of the object a fact is
// about, and hands them back beside the wants, for a port whose original exports facts to match through
// CompareFacts as staticcheck's testutil matches them.
package staticcheck_wants

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// Read is every finding the Go files under directory expect, read from their notes, in the order of the files'
// paths and of the notes in each. A note this reader cannot read, or one that is not a diag note, is an error naming
// where it is, never a want dropped.
func Read(directory string) ([]Want, error) {
	wants, _, readError := read(directory, false)
	return wants, readError
}

// ReadWithFacts is every finding and every fact the Go files under directory expect, read from their diag and fact
// notes as Read reads them, the wants in the order Read gives and the facts in the same order. A note that is
// neither, or one this reader cannot read, is an error naming where it is.
func ReadWithFacts(directory string) ([]Want, []FactWant, error) {
	return read(directory, true)
}

// read is the wants and, when readsFacts is set, the facts of the notes of the Go files under directory.
func read(directory string, readsFacts bool) ([]Want, []FactWant, error) {
	root, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		return nil, nil, absoluteError
	}
	fileSet := token.NewFileSet()
	wants := []Want{}
	facts := []FactWant{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, entryError error) error {
		if entryError != nil {
			return entryError
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		file, parseError := parser.ParseFile(fileSet, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseError != nil {
			return fmt.Errorf("parsing %s: %w", path, parseError)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				body, offset, isNote := noteBody(comment.Text)
				if !isNote {
					continue
				}
				noteWants, noteFacts, noteError := newNoteParser(fileSet, comment.Pos()+token.Pos(offset), body,
					readsFacts).notes()
				if noteError != nil {
					return noteError
				}
				wants = append(wants, noteWants...)
				facts = append(facts, noteFacts...)
			}
		}
		return nil
	})
	if walkError != nil {
		return nil, nil, walkError
	}
	return wants, facts, nil
}

// noteBody is the notes text holds, the text of one comment, and the offset in text they start at, or false when
// it holds none. As go/expect reads a comment, its notes follow an @ that opens it, `//@` or `/*@`, or an @ later in
// it that opens a comment written inside it, `// text //@`; an @ elsewhere opens no notes.
func noteBody(text string) (string, int, bool) {
	// The text after the comment's own opening, `//` or `/*`, and before a block's closing.
	offset := 2
	body := text[offset:]
	if strings.HasPrefix(text, "/*") {
		body = strings.TrimSuffix(body, "*/")
	}
	if index := strings.Index(body, "@"); index > 2 {
		if body[index-2:index] != "//" {
			return "", 0, false
		}
		body = body[index:]
		offset += index
	}
	if !strings.HasPrefix(body, "@") {
		return "", 0, false
	}
	return body[1:], offset + 1, true
}
