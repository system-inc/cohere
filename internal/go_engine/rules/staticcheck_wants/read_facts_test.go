package staticcheck_wants

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// sa5012DiagnosticNotes is how many diag notes sa5012's fixture holds, counted apart from this reader.
const sa5012DiagnosticNotes = 16

// ReadWithFacts finds a fact in each shape of fact note staticcheck's fixtures write, at the line of the note's name,
// each with the object it names, and the diag notes beside them as Read finds them.
func TestReadWithFactsReadsEveryShapeOfFactNote(test *testing.T) {
	test.Parallel()
	wants, facts, readError := ReadWithFacts(filepath.Join("testdata", "facts"))
	if readError != nil {
		test.Fatal(readError)
	}
	directory, absoluteError := filepath.Abs(filepath.Join("testdata", "facts"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	file := filepath.Join(directory, "facts.go")
	expectedFacts := []string{
		file + `:4: fact(values, "needs even elements")`,
		file + `:6: fact(values, re"needs \\w+ elements")`,
		file + `:8: fact(first, "one")`,
		file + `:8: fact(second, "two")`,
		file + `:10: fact(values, "beside")`,
		file + `:11: fact(values, "over lines")`,
	}
	actualFacts := []string{}
	for _, fact := range facts {
		actualFacts = append(actualFacts, fact.String())
	}
	if !slices.Equal(actualFacts, expectedFacts) {
		test.Errorf("ReadWithFacts found the facts\n%s\nwhere the fixture holds\n%s", strings.Join(actualFacts, "\n"),
			strings.Join(expectedFacts, "\n"))
	}
	actualWants := []string{}
	for _, want := range wants {
		actualWants = append(actualWants, want.String())
	}
	if expectedWants := []string{file + `:10: diag("a diag")`}; !slices.Equal(actualWants, expectedWants) {
		test.Errorf("ReadWithFacts found the wants %v, where the fixture holds %v", actualWants, expectedWants)
	}
}

// A note ReadWithFacts cannot read is an error naming its file, its line and what is wrong: a note neither diag nor
// fact, a fact note whose object is a string rather than a name, one missing the comma after the name, and one with
// a third argument.
func TestReadWithFactsRefusesANoteItCannotRead(test *testing.T) {
	test.Parallel()
	cases := []struct {
		folder string
		says   string
	}{
		{"unknown", "a note note, where this reader reads diag and fact notes alone"},
		{"unnamed", "where the name of the object it is about was expected"},
		{"comma", "where a comma after the object's name was expected"},
		{"unclosed", "where its two arguments close with )"},
	}
	for _, testCase := range cases {
		wants, facts, readError := ReadWithFacts(filepath.Join("testdata", "refused_with_facts", testCase.folder))
		if readError == nil {
			test.Errorf("the %s note read as %v and %v, where an error was expected", testCase.folder, wants, facts)
			continue
		}
		place := testCase.folder + ".go:3:"
		if !strings.Contains(readError.Error(), place) || !strings.Contains(readError.Error(), testCase.says) {
			test.Errorf("the %s note failed with %q, which does not say %q at %s", testCase.folder, readError,
				testCase.says, place)
		}
	}
}

// Over every fixture of staticcheck v0.8.1's checks, ReadWithFacts reads every diag note Read reads and sa5012's
// besides, refusing none, and the fact notes, which sa5012's fixture alone holds: its four, one on each function that
// needs its slice of even length.
func TestReadWithFactsReadsStaticchecksFixtures(test *testing.T) {
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
	read := 0
	facts := []string{}
	for _, fixture := range fixtures {
		wants, fixtureFacts, readError := ReadWithFacts(fixture)
		if readError != nil {
			test.Errorf("reading %s: %v", fixture, readError)
			continue
		}
		read += len(wants)
		for _, fact := range fixtureFacts {
			// The fact as its note writes it, after the base name of its file and its line.
			written := strings.SplitN(fact.String(), ":", 3)[2]
			facts = append(facts, fmt.Sprintf("%s:%d:%s", filepath.Base(fact.File), fact.Line, written))
		}
	}
	test.Logf("read %d wants and %d facts from %d fixtures", read, len(facts), len(fixtures))
	if expected := staticcheckDiagnosticNotes + sa5012DiagnosticNotes; read != expected {
		test.Errorf("read %d wants from staticcheck's fixtures, where they hold %d diag notes", read, expected)
	}
	expectedFacts := []string{
		`CheckEvenSliceLength.go:5: fact(args, "needs even elements")`,
		`CheckEvenSliceLength.go:11: fact(args, "needs even elements")`,
		`CheckEvenSliceLength.go:17: fact(args, "needs even elements")`,
		`CheckEvenSliceLength.go:21: fact(bleh, "needs even elements")`,
	}
	if !slices.Equal(facts, expectedFacts) {
		test.Errorf("read the facts\n%s\nwhere sa5012's fixture holds\n%s", strings.Join(facts, "\n"),
			strings.Join(expectedFacts, "\n"))
	}
}

// CompareFacts matches each fact to one want on its object's file and line that names its object, by text or
// pattern, and returns what is left on each side: a fact about another object on the want's line, a fact whose text
// the want lacks, one more fact than a line's wants, and a want no fact reaches.
func TestCompareFactsMatchesAsStaticchecksTestutil(test *testing.T) {
	test.Parallel()
	text := FactWant{File: "a.go", Line: 1, Object: "values", Text: "needs even"}
	pattern := FactWant{File: "a.go", Line: 2, Object: "args", Pattern: regexp.MustCompile(`^needs \w+ elements$`)}
	unreached := FactWant{File: "b.go", Line: 1, Object: "values", Text: "needs even"}
	facts := []FoundFact{
		{File: "a.go", Line: 1, Object: "values", Fact: "needs even elements"},
		{File: "a.go", Line: 1, Object: "values", Fact: "needs even elements"},
		{File: "a.go", Line: 2, Object: "other", Fact: "needs even elements"},
		{File: "a.go", Line: 2, Object: "args", Fact: "needs even elements"},
		{File: "a.go", Line: 3, Object: "values", Fact: "needs even elements"},
		{File: "b.go", Line: 1, Object: "values", Fact: "something else"},
	}
	unexpected, untaken := CompareFacts([]FactWant{text, pattern, unreached}, facts)
	expectedUnexpected := []FoundFact{facts[1], facts[2], facts[4], facts[5]}
	if !slices.Equal(unexpected, expectedUnexpected) {
		test.Errorf("CompareFacts found unexpected %v, where %v was", unexpected, expectedUnexpected)
	}
	if expectedUntaken := []FactWant{unreached}; !slices.Equal(untaken, expectedUntaken) {
		test.Errorf("CompareFacts left untaken %v, where %v was", untaken, expectedUntaken)
	}
	unexpected, untaken = CompareFacts([]FactWant{text}, facts[:1])
	if len(unexpected) != 0 || len(untaken) != 0 {
		test.Errorf("a fact its want matches left %v unexpected and %v untaken", unexpected, untaken)
	}
}
