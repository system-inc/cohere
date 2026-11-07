package main

import "testing"

// A respelling's reflow refills only plain prose, read as line_wrap reads it, so a marker line written straight after
// a paragraph, with no blank line between, ends the paragraph rather than being joined into it: loom's `// Serial:`
// and `// Wall:` guards, an Example's `// Output:` and Go's `// Deprecated:` each stay on a line of their own.
func TestAMarkerLineIsNeverInAParagraph(test *testing.T) {
	test.Parallel()
	cases := []struct {
		text        string
		inParagraph bool
	}{
		{"// The runner holds the machine's only lock for the length of the suite.", true},
		{"// runs.", true},
		{"// Serial: the runner takes the machine-wide lock", false},
		{"// Wall: about four seconds", false},
		{"// Output:", false},
		{"// Deprecated: use Load", false},
		{"// - a list item", false},
		{"// # A heading", false},
		{"//\tindented code", false},
		{"// serial: lowercase is prose", true},
		{"// Ratio: 3:2 is still a marker", false},
		{"// A sentence with a colon: in the middle", true},
	}
	for _, testCase := range cases {
		line := commentLine{text: testCase.text}
		if line.inParagraph() != testCase.inParagraph {
			test.Errorf("%q: inParagraph %t, want %t", testCase.text, line.inParagraph(), testCase.inParagraph)
		}
	}
}
