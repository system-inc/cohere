package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

// A file handed to the formatter with CRLF is counted once, however many passes it takes, and the note
// names the count and the .gitattributes line. A tree with none prints nothing.
func TestCRLFFilesAreNamedOnceWithTheFix(t *testing.T) {
	t.Parallel()
	var lineEndings crlfFiles
	transform := lineEndings.observing(func(fileName string, text string) (string, error) {
		return strings.ReplaceAll(text, "\r\n", "\n"), nil
	})

	var group sync.WaitGroup
	for _, file := range []struct{ name, text string }{
		{"a.ts", "export const a = 1;\r\n"},
		{"a.ts", "export const a = 1;\n"},
		{"b.ts", "export const b = 2;\r\nexport const c = 3;\r\n"},
		{"lf.ts", "export const lf = 4;\n"},
	} {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := transform(file.name, file.text); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()

	var checked bytes.Buffer
	lineEndings.report(&checked, false)
	if got := checked.String(); !strings.HasPrefix(got, "note: 2 files have CRLF line endings, and the house format is LF, so --no-fix reports them as unformatted.") ||
		!strings.Contains(got, "add `* text=auto eol=lf` to the repository's .gitattributes") {
		t.Fatalf("the note reads %q", got)
	}

	var written bytes.Buffer
	lineEndings.report(&written, true)
	if !strings.Contains(written.String(), "so --fix rewrote them to LF.") {
		t.Fatalf("a writing run's note reads %q", written.String())
	}

	var none crlfFiles
	var quiet bytes.Buffer
	none.report(&quiet, false)
	if quiet.Len() != 0 {
		t.Fatalf("a tree with no CRLF printed %q", quiet.String())
	}
}
