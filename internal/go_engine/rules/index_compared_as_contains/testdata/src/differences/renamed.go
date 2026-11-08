package differences

import (
	binary "bytes"
	text "strings"
)

// A renamed import of strings or bytes calls the very functions the check knows, which the original, reading the
// spelling, misses, and the port's fix names the package as the call does.
func renamed(a string, b string, x []byte, y []byte) {
	// text is strings, so this asks whether a holds b.
	_ = text.Index(a, b) != -1 // port finds: should use text.Contains(a, b) instead
	// text is strings, so this asks whether a holds any of b.
	_ = text.IndexAny(a, b) >= 0 // port finds: should use text.ContainsAny(a, b) instead
	// binary is bytes, so this asks whether x lacks the rune.
	_ = binary.IndexRune(x, 'r') < 0 // port finds: should use !binary.ContainsRune(x, 'r') instead
	// binary is bytes, so this asks whether x lacks y.
	_ = binary.Index(x, y) == -1 // port finds: should use !binary.Contains(x, y) instead
	_ = text.LastIndex(a, b) != -1
	_ = binary.IndexByte(x, 'b') >= 0
}
