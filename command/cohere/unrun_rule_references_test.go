package main

import (
	"bufio"
	"bytes"
	"testing"
)

// The --verbose note for directives naming rules cohere doesn't run: one line, the total, then each rule
// with its count, most named first and ties by name, and nothing at all when there are none (#v1ah2qq).
func TestUnrunRuleReferencesAreOneNoteMostNamedFirst(test *testing.T) {
	test.Parallel()
	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	writeUnrunRuleReferences(stream, map[string]int{"jsx-a11y/alt-text": 1, "import/no-cycle": 2, "@eslint-react/no-array-index-key": 1})
	flushStream(test, stream)
	want := "  directives: 4 disable comments name rules cohere doesn't run, which silences nothing here: " +
		"import/no-cycle 2, @eslint-react/no-array-index-key 1, jsx-a11y/alt-text 1\n"
	if out.String() != want {
		test.Errorf("the note is\n%q\nwant\n%q", out.String(), want)
	}

	out.Reset()
	writeUnrunRuleReferences(stream, map[string]int{"no-with": 1})
	flushStream(test, stream)
	if want := "  directives: 1 disable comment names a rule cohere doesn't run, which silences nothing here: no-with 1\n"; out.String() != want {
		test.Errorf("the singular note is %q, want %q", out.String(), want)
	}

	out.Reset()
	writeUnrunRuleReferences(stream, nil)
	flushStream(test, stream)
	if out.Len() != 0 {
		test.Errorf("no references printed %q, want nothing", out.String())
	}
}
