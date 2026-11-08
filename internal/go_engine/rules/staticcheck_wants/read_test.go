package staticcheck_wants

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// staticcheckDiagnosticNotes is how many diag notes the Go files of staticcheck v0.8.1's check fixtures hold outside
// sa5012's, counted apart from this reader: every diag name after a comma, a space or the opening of a line's first
// //@ or /*@. A count is the one way a note the reader silently passes over shows, so it moves only with the pin.
const staticcheckDiagnosticNotes = 1204

// Read finds a want in each shape of note staticcheck's fixtures write, in a folder below too, at the line of the
// note's name, and none in a comment that only mentions an @ or a diag: a string is a text the message contains, and
// re and a string a pattern it matches.
func TestReadReadsEveryShapeOfNote(test *testing.T) {
	test.Parallel()
	wants, readError := Read(filepath.Join("testdata", "read"))
	if readError != nil {
		test.Fatal(readError)
	}
	directory, absoluteError := filepath.Abs(filepath.Join("testdata", "read"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	readFile := filepath.Join(directory, "read.go")
	nestedFile := filepath.Join(directory, "nested", "nested.go")
	// Read walks the files in lexical order of their paths, so the folder nested comes before read.go.
	expected := []string{
		nestedFile + `:4: diag("nested")`,
		readFile + `:5: diag("plain text")`,
		readFile + `:6: diag(re"pattern \\d+")`,
		readFile + `:7: diag("first")`,
		readFile + `:7: diag("second")`,
		readFile + `:8: diag("inside a comment")`,
		readFile + `:9: diag("no space after the at")`,
		readFile + `:10: diag("in a block")`,
		readFile + `:11: diag("over two lines")`,
	}
	actual := []string{}
	for _, want := range wants {
		actual = append(actual, want.String())
	}
	if !slices.Equal(actual, expected) {
		test.Errorf("Read found\n%s\nwhere the fixture holds\n%s", strings.Join(actual, "\n"),
			strings.Join(expected, "\n"))
	}
}

// A note Read cannot read is an error naming its file, its line and what is wrong, never a want dropped: an unclosed
// note, a pattern that does not compile, two notes with no separator, an argument that is no string, and a fact note,
// which staticcheck checks and this reader does not.
func TestReadRefusesANoteItCannotRead(test *testing.T) {
	test.Parallel()
	cases := []struct {
		folder string
		says   string
	}{
		{"unclosed", "where its one argument closes with )"},
		{"pattern", "invalid regular expression ("},
		{"separator", "where a comma or a new line was expected"},
		{"number", "where a string or re and a string was expected"},
		{"fact", "a fact note, where this reader reads diag notes alone"},
	}
	for _, testCase := range cases {
		wants, readError := Read(filepath.Join("testdata", "refused", testCase.folder))
		if readError == nil {
			test.Errorf("the %s note read as %v, where an error was expected", testCase.folder, wants)
			continue
		}
		place := testCase.folder + ".go:3:"
		if !strings.Contains(readError.Error(), place) || !strings.Contains(readError.Error(), testCase.says) {
			test.Errorf("the %s note failed with %q, which does not say %q at %s", testCase.folder, readError,
				testCase.says, place)
		}
	}
}

// Every note in the fixtures of staticcheck v0.8.1's checks, the ones the house ports, reads, but sa5012's, whose fact
// notes are refused, and the reader finds every diag note there is.
func TestReadReadsStaticchecksFixtures(test *testing.T) {
	test.Parallel()
	fixtures := []string{}
	for _, checks := range []string{"staticcheck", "simple", "stylecheck", "quickfix"} {
		found, globError := filepath.Glob(filepath.Join("..", "..", "..", "..", "third_party", "honnef.co", "go",
			"tools", checks, "*", "testdata"))
		if globError != nil {
			test.Fatal(globError)
		}
		fixtures = append(fixtures, found...)
	}
	if len(fixtures) == 0 {
		test.Fatal("found no fixture of staticcheck's, so the test proves nothing")
	}
	read := 0
	refused := []string{}
	for _, fixture := range fixtures {
		wants, readError := Read(fixture)
		if readError != nil {
			if strings.Contains(readError.Error(), "a fact note, where this reader reads diag notes alone") {
				refused = append(refused, fixture)
				continue
			}
			test.Errorf("reading %s: %v", fixture, readError)
			continue
		}
		read += len(wants)
	}
	test.Logf("read %d wants from %d fixtures, and refused %d for their fact notes: %v", read,
		len(fixtures)-len(refused), len(refused), refused)
	if read != staticcheckDiagnosticNotes {
		test.Errorf("read %d wants from staticcheck's fixtures, where they hold %d diag notes outside sa5012", read,
			staticcheckDiagnosticNotes)
	}
	if len(refused) != 1 || filepath.Base(filepath.Dir(refused[0])) != "sa5012" {
		test.Errorf("refused %v for their fact notes, where only sa5012's fixture holds any", refused)
	}
}

// Compare matches each finding to one want on its file and line, by text or pattern, and returns what is left on
// each side: a finding no want expects, one more finding than a line's wants, and a want on another line or whose
// text the message lacks.
func TestCompareMatchesAsStaticchecksTestutil(test *testing.T) {
	test.Parallel()
	text := Want{File: "a.go", Line: 1, Text: "missing closing )"}
	pattern := Want{File: "a.go", Line: 2, Pattern: regexp.MustCompile(`^error parsing regexp: .+ \x60\(\x60$`)}
	elsewhere := Want{File: "a.go", Line: 3, Text: "on line three"}
	lacking := Want{File: "b.go", Line: 1, Text: "not in the message"}
	findings := []Finding{
		{File: "a.go", Line: 1, Message: "error parsing regexp: missing closing ): `(`"},
		{File: "a.go", Line: 1, Message: "error parsing regexp: missing closing ): `[(`"},
		{File: "a.go", Line: 2, Message: "error parsing regexp: missing closing ): `(`"},
		{File: "a.go", Line: 4, Message: "on line three"},
		{File: "b.go", Line: 1, Message: "something else"},
	}
	unexpected, untaken := Compare([]Want{text, pattern, elsewhere, lacking}, findings)
	expectedUnexpected := []Finding{findings[1], findings[3], findings[4]}
	if !slices.Equal(unexpected, expectedUnexpected) {
		test.Errorf("Compare found unexpected %v, where %v was", unexpected, expectedUnexpected)
	}
	if expectedUntaken := []Want{elsewhere, lacking}; !slices.Equal(untaken, expectedUntaken) {
		test.Errorf("Compare left untaken %v, where %v was", untaken, expectedUntaken)
	}
	unexpected, untaken = Compare([]Want{text}, findings[:1])
	if len(unexpected) != 0 || len(untaken) != 0 {
		test.Errorf("a finding its want matches left %v unexpected and %v untaken", unexpected, untaken)
	}
}
