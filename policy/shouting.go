package policy

import (
	"bytes"
	_ "embed"
)

// Shouting.json is the uppercase vocabulary the shouting rules judge with: nexus's consistency-no-shouting in Go,
// and cohere-swift's of the same name in Swift. The Go rule reads and checks it
// (internal/lint/rules/nexus/shouting_vocabulary.go), refusing an unknown key and panicking at init on a file it
// refuses, since which words are acronyms is that rule's to interpret. The Swift engine compiles in the same bytes
// (SwiftFiles), so a released engine carries the words with nothing handed to it at run time.
//
//go:embed Shouting.json
var shoutingFile []byte

// ShoutingFile is Shouting.json exactly as embedded. A copy, so a caller that writes into it cannot change what
// the next caller reads.
func ShoutingFile() []byte {
	return bytes.Clone(shoutingFile)
}
