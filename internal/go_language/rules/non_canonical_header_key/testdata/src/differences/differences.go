// Package differences holds the shapes where the port's findings or fixes could part from the original's: keys
// spread over lines or beside comments, raw and escaped literals whose fix quotes the canonical text afresh, a
// parenthesized identifier, and a constant shadowed by a variable. The two agree on every one, so no difference is
// recorded.
package differences

import "net/http"

const spaced = "x-spread"

func spread(h http.Header) {
	_ = h[spaced /* after the key */]
	_ = h[ // a comment
	"x-commented"]
	_ = h[`x-raw`]
	_ = h["x-\145scaped"]
	_ = h["x-\x66oo"]
	_ = h[(spaced)]
	const shadow = "shadowed"
	{
		shadow := "variable"
		_ = h[shadow]
	}
	_ = h[shadow]
	_ = h[
	// a comment before the key
	spaced]
}
