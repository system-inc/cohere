// Package allowed holds what the rule must leave alone: cohere's own suppression, the directives as text in a string,
// and a mention of them in prose.
package allowed

//cohere:ignore cohere-go/U1000 kept for the caller the next change adds
func kept() {}

// The rule refuses staticcheck's //lint:ignore and //lint:file-ignore and golangci-lint's //nolint, and this
// sentence only names them.

// A comment that opens with a space before the slashes' word is prose, as this one's next line is.
// //lint:file-ignore U1000 covers the whole file, a line a directive covers being quoted here.
var quoted = 1

// nolint is a word in this sentence, after the space that makes it prose.
var prose = 2

/* //lint:ignore U1000 in a block comment, which staticcheck does not read */
var blocked = 3

/*nolint*/
var blockedBare = 4

// interpreted holds a directive in an interpreted string, which is code and not a comment.
var interpreted = "//nolint:nilerr"
