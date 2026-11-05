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
		{"several lines close nothing", "unlisted:\n  a/go.mod: add it", "Unlisted:\n  a/go.mod: add it"},
		{"a tool's output is left as it is", "building: exit 1\nmain.go:3: oops", "Building: exit 1\nmain.go:3: oops"},
		{"a path at the end stays open", "reading /repo/go.work", "Reading /repo/go.work"},
		{"a file at the end stays open", "nothing uses tsconfig.json", "Nothing uses tsconfig.json"},
		{"a quoted value at the end stays open", `unknown rule "printf"`, `Unknown rule "printf"`},
		{"an assignment at the end stays open", "the load ran with GOWORK=off", "The load ran with GOWORK=off"},
		{"a parenthesis is not a literal", "the engine stopped (exit 2)", "The engine stopped (exit 2)."},
		{"a colon opens rather than closes", "these were untested:", "These were untested:"},
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
