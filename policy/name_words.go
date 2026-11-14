package policy

import (
	"bytes"
	_ "embed"
)

// NameWords.json is the words a folder's or a file's name spells out and an identifier need not, beside
// Abbreviations.json (#zyq63kk): `docs` is documentation on a path, and inside an identifier it is often the
// plural of Prettier's doc. It has Abbreviations.json's shape, so internal/naming/abbreviation reads it with the
// same loader, and internal/file_name_words judges a name's words by both. The Swift engine compiles in the same
// bytes (SwiftFiles).
//
//go:embed NameWords.json
var nameWordsFile []byte

// NameWordsFile is NameWords.json exactly as embedded. A copy, so a caller that writes into it cannot change what
// the next caller reads.
func NameWordsFile() []byte {
	return bytes.Clone(nameWordsFile)
}
