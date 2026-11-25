package differences

import (
	"differences/strings"
)

// A package of another path whose name is strings is no standard library strings, and its functions none of the
// check's.
func fork(a string, b string) {
	// The package declares no Contains, so the original's fix does not compile.
	_ = strings.Index(a, b) != -1 // port drops: should use strings.Contains(a, b) instead
	// The package declares no ContainsAny, so the original's fix does not compile.
	_ = strings.IndexAny(a, b) == -1 // port drops: should use !strings.ContainsAny(a, b) instead
	// The package declares no ContainsRune, so the original's fix does not compile.
	_ = strings.IndexRune(a, 'r') >= 0 // port drops: should use strings.ContainsRune(a, 'r') instead
	_ = strings.LastIndex(a, b) != -1
}
