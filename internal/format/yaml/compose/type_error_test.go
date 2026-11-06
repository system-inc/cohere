package compose

import (
	"slices"
	"testing"
	"unicode/utf16"

	"github.com/system-inc/cohere/internal/format/yaml/cst"
)

// TestComposeReportsTheFlowCommentTypeError drives Compose to the typeError resolveFlowCollection raises
// on a comment that belongs to a previous item never composed, and holds Compose to reporting it as the
// RESOURCE_EXHAUSTION error upstream's catch makes, so a composer that stops recovering it panics out of
// Compose and fails here.
//
// cst.Parser never builds such a collection: an item holding only spaces, commas and comments takes in
// every later one of those, so it is always the last item. Compose takes any tokens, though, and the
// fork's Composer, given these same tokens, reports these same errors at these same positions.
func TestComposeReportsTheFlowCommentTypeError(test *testing.T) {
	test.Parallel()
	// sourceToken is a token with source, as cst.Parser makes one at indent 0.
	sourceToken := func(tokenType string, offset int, source string) *cst.Token {
		return &cst.Token{Type: tokenType, Offset: offset, Source: utf16.Encode([]rune(source))}
	}
	// commentedStart is an item's start that opens with a comma and holds a comment, offsets 2 to 7.
	commentedStart := func() []*cst.Token {
		return []*cst.Token{
			sourceToken("comma", 2, ","),
			sourceToken("space", 3, " "),
			sourceToken("comment", 4, "# c"),
			sourceToken("newline", 7, "\n"),
		}
	}
	cases := []struct {
		name string
		// text is what the tokens spell, whose length Compose takes as the end offset.
		text       string
		collection *cst.Token
		expected   []YamlError
	}{
		{
			name: "flow sequence",
			text: "[ , # c\na]",
			collection: &cst.Token{
				Type:      "flow-collection",
				FlowStart: sourceToken("flow-seq-start", 0, "["),
				Items: []*cst.CollectionItem{
					{Start: []*cst.Token{sourceToken("space", 1, " ")}},
					{Start: commentedStart(), Value: sourceToken("scalar", 8, "a")},
				},
				End: []*cst.Token{sourceToken("flow-seq-end", 9, "]")},
			},
			expected: []YamlError{
				{
					Name:     "YAMLParseError",
					Code:     "UNEXPECTED_TOKEN",
					Message:  "Unexpected empty item in flow sequence",
					Position: [2]int{2, 3},
				},
				{
					Name:     "YAMLParseError",
					Code:     "RESOURCE_EXHAUSTION",
					Message:  "Cannot read properties of undefined (reading 'comment')",
					Position: [2]int{0, 1},
				},
			},
		},
		{
			name: "flow map",
			text: "{ , # c\na: b}",
			collection: &cst.Token{
				Type:      "flow-collection",
				FlowStart: sourceToken("flow-map-start", 0, "{"),
				Items: []*cst.CollectionItem{
					{Start: []*cst.Token{sourceToken("space", 1, " ")}},
					{
						Start: commentedStart(),
						Key:   sourceToken("scalar", 8, "a"),
						Sep:   []*cst.Token{sourceToken("map-value-ind", 9, ":"), sourceToken("space", 10, " ")},
						Value: sourceToken("scalar", 11, "b"),
					},
				},
				End: []*cst.Token{sourceToken("flow-map-end", 12, "}")},
			},
			expected: []YamlError{
				{
					Name:     "YAMLParseError",
					Code:     "UNEXPECTED_TOKEN",
					Message:  "Unexpected empty item in flow map",
					Position: [2]int{2, 3},
				},
				{
					Name:     "YAMLParseError",
					Code:     "RESOURCE_EXHAUSTION",
					Message:  "Cannot read properties of undefined (reading 'comment')",
					Position: [2]int{0, 1},
				},
			},
		},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			document := &cst.Token{Type: "document", Start: []*cst.Token{}, Value: each.collection}
			composer := NewComposer(UnistParserOptions())
			documents := slices.Collect(composer.Compose([]*cst.Token{document}, true, len(each.text)))
			if len(documents) != 1 {
				test.Fatalf("Compose of %q made %d documents, expected 1", each.text, len(documents))
			}
			actual := make([]YamlError, 0, len(documents[0].Errors))
			for _, yamlError := range documents[0].Errors {
				actual = append(actual, *yamlError)
			}
			if !slices.Equal(actual, each.expected) {
				test.Errorf("Compose of %q reported %+v, expected %+v", each.text, actual, each.expected)
			}
		})
	}
}
