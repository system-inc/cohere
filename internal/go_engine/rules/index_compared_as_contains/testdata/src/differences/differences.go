// Package differences holds where the port parts from the original, which judges strings and bytes by their
// spelling: here variables spelled strings and bytes, which the port drops, beside the trickiest shapes where the two
// agree. Its other files hold a renamed import of each package, which the port finds, an import of bytes under the
// name strings, where the two agree, and a package of another path named strings, which the port drops. Each record
// says what the port does, its reason on the line above.
package differences

import (
	"bytes"
	"strings"
)

type finder struct{}

func (finder) Index(needles ...string) int { return len(needles) }

func (finder) IndexAny(s string, characters string) int { return len(s) - len(characters) }

func (finder) IndexRune(s string, r rune) int { return len(s) - int(r) }

// Arguments across lines and with comments among them, printed into the message and the fix from their positions,
// where the two agree.
func spread(a string, b string, x []byte) {
	_ = strings.Index(
		a,
		b,
	) != -1
	_ = strings.Index(a /* haystack */, b /* needle */) == -1
	_ = bytes.IndexRune(
		x, // the bytes
		'r',
	) >= 0
	_ = strings.IndexAny(func() string {
		return a + b
	}(), "xyz") < 0
}

// A variable spelled strings or bytes is no package, and its methods no call of the standard library's.
func variables(a string, b string, needles []string) {
	strings := finder{}
	// A finder has no Contains, and the original's fix also loses the spread's ellipsis.
	_ = strings.Index(needles...) != -1 // port drops: should use strings.Contains(needles) instead
	// A finder has no Contains, so the original's fix does not compile.
	_ = strings.Index(a, b) == -1 // port drops: should use !strings.Contains(a, b) instead
	// A qualifier in parentheses is no identifier, so both leave it alone.
	_ = (strings).Index(a, b) != -1
	// A finder has no ContainsAny, so the original's fix does not compile.
	_ = strings.IndexAny(a, b) >= 0 // port drops: should use strings.ContainsAny(a, b) instead
	bytes := &finder{}
	// A finder has no ContainsRune, so the original's fix does not compile.
	_ = bytes.IndexRune(a, 'r') < 0 // port drops: should use !bytes.ContainsRune(a, 'r') instead
	// A finder has no Contains, so the original's fix does not compile.
	_ = bytes.Index() > -1 // port drops: should use bytes.Contains() instead
}
