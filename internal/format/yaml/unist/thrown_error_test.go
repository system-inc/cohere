package unist

import (
	"errors"
	"testing"
)

// TestParseReturnsWhatUpstreamThrows drives Parse to throwError and throwTypeError, the port's form of
// upstream's throws, and holds Parse to returning each as the *ThrownError upstream's caller catches, so
// a parse that stops recovering them panics out of Parse and fails here. Each expected name and message
// is what the fork's yaml-unist-parser throws for the same text.
func TestParseReturnsWhatUpstreamThrows(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name     string
		text     string
		expected ThrownError
	}{
		{
			// A directive after the last document is left in tokensBeforeBody, which transformDocuments
			// refuses once every token is read.
			name:     "throwError",
			text:     "...\n%YAML 1.2\n",
			expected: ThrownError{Name: "Error", Message: "Unexpected 'directive' token at 2:1"},
		},
		{
			// A flow map item that is only a tag has no key, no : and no value, and upstream reads the
			// offset of the value it does not have.
			name:     "throwTypeError on a flow map item that is only a tag",
			text:     "{!}",
			expected: ThrownError{Name: "TypeError", Message: "Cannot read properties of undefined (reading 'offset')"},
		},
		{
			// !!pairs makes the plain item a into a pair with no source item, and upstream reads the start
			// of the source item it does not have.
			name:     "throwTypeError on a !!pairs item that is not a pair",
			text:     "!!pairs\n- a\n",
			expected: ThrownError{Name: "TypeError", Message: "Cannot read properties of undefined (reading 'start')"},
		},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			root, parseError := Parse(each.text, nil)
			var thrown *ThrownError
			if !errors.As(parseError, &thrown) {
				test.Fatalf("Parse(%q) returned %v and error %v, expected the thrown %s", each.text, root, parseError,
					each.expected.Error())
			}
			if *thrown != each.expected {
				test.Errorf("Parse(%q) returned %q, expected %q", each.text, thrown.Error(), each.expected.Error())
			}
		})
	}
}
