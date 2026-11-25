package staticcheck_wants

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

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
