package lookalike

import "net/url"

// One capital before a word looks like a run of two and is not an initialism: the second capital opens the word.
var IPv4 = "127.0.0.1"

type TKey struct{}

var AValue = 1

// A screaming snake case name is consistency-no-screaming-snake-case's to report, so this rule leaves it.
const MAX_RETRIES = 3

const HTTP_STATUS_OK = 200

// An embedded field is named by its type, which is url's.
type Link struct {
	*url.URL
}

// A word in capitals inside a comment, a string or a struct tag names nothing: URL, JSON, ID.
type Payload struct {
	Value string `json:"userID" xml:"URL"`
}

var Message = "parse the URL and the JSON"
