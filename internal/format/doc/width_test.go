package doc

import "testing"

// TestWidthControlCanFail checks the widths that separate a port from a byte or rune count: a wide character,
// an astral emoji and a combining mark.
func TestWidthControlCanFail(test *testing.T) {
	test.Parallel()
	if StringWidth("中") != 2 || StringWidth("a") != 1 || StringWidth("😀") != 2 || StringWidth("é") != 1 {
		test.Fatalf("basic widths wrong: 中=%d a=%d 😀=%d e+combining=%d",
			StringWidth("中"), StringWidth("a"), StringWidth("😀"), StringWidth("é"))
	}
}
