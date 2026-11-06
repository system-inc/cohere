// Package lookalike holds shapes that look like shouting and are not. The rule judges comments only, so capitals in
// code are never read, and inside a comment some capitals are names rather than a voice.
package lookalike

const MAX_RETRIES = 3

var shoutedString = "NEVER DO THIS"

var commentInAString = "// NEVER cache this"

var blockCommentInARawString = `/* this is REALLY important */`

type tagged struct {
	Name string `json:"NAME" yaml:"LOUD"`
}

var tag = tagged{Name: "ALWAYS"}

// HTTPServer and JSONDecoder are names, not a raised voice.

// MAX_RETRIES caps the loop at three.

// UTF8 and SHA256 carry digits, so they are identifiers.

// I said A is the variable here.

// Thing is the one, and WithTLS turns on encryption.

// nolint is not a directive without its colon, and says nothing loud either.
