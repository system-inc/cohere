package sentence

import (
	"errors"
	"fmt"
	"testing"
)

func TestOfMakesAClauseASentence(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name     string
		clause   string
		expected string
	}{
		{"a plain clause", "nothing was checked", "Nothing was checked."},
		{"a chain wrapped with colons", "reading go.work: no such file", "Reading go.work: no such file."},
		{"a first word ending a clause", "reading: denied", "Reading: denied."},
		{"already a sentence", "Nothing was checked.", "Nothing was checked."},
		{"a question", "is this a project?", "Is this a project?"},
		{"trailing space is not an ending", "nothing was checked \n", "Nothing was checked."},
		{"a flag keeps its case", "--tsconfig names a TypeScript program", "--tsconfig names a TypeScript program."},
		{"a file keeps its case", "go.work does not use every module", "go.work does not use every module."},
		{"a path keeps its case", "/tmp/a is not a file", "/tmp/a is not a file."},
		{"a code literal keeps its case", "`GOWORK=off` was set", "`GOWORK=off` was set."},
		{"an identifier keeps its case", "go_engine failed", "go_engine failed."},
		{"a capital stays", "Swift is not available", "Swift is not available."},
		{"a list ends with a period", "unlisted:\n  a/go.mod: add it", "Unlisted:\n  a/go.mod: add it."},
		{"a letter beyond ASCII", "école fermée", "École fermée."},
		{"empty text", "", ""},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if actual := Of(testCase.clause); actual != testCase.expected {
				test.Fatalf("Of(%q) = %q, expected %q", testCase.clause, actual, testCase.expected)
			}
		})
	}
}

func TestFromErrorRendersTheWholeChainOnce(test *testing.T) {
	test.Parallel()
	inner := errors.New("no such file")
	chain := fmt.Errorf("checking the workspace: %w", fmt.Errorf("reading go.work: %w", inner))
	expected := "Checking the workspace: reading go.work: no such file."
	if actual := FromError(chain); actual != expected {
		test.Fatalf("FromError = %q, expected %q", actual, expected)
	}
}
