// Package parity calls each function with a cutset that is not valid UTF-8 and with one that is, in the shapes
// staticcheck's representation builds a cutset or a call in more than one way.
package parity

import "strings"

type cutset string

const (
	invalid = "\xff"
	valid   = "ab"
	joined  = "a" + "\xfe"
)

const typed cutset = "\xc3"

var trimmed = strings.Trim("x", "\x80")

func eachFunction(text string) {
	strings.IndexAny(text, "\xff")
	strings.IndexAny(text, "é")
	strings.LastIndexAny(text, "\xff")
	strings.LastIndexAny(text, "abc")
	strings.ContainsAny(text, "\xff")
	strings.ContainsAny(text, "")
	strings.Trim(text, "\xff")
	strings.Trim(text, " ")
	strings.TrimLeft(text, "\xff")
	strings.TrimLeft(text, "日本")
	strings.TrimRight(text, "\xff")
	strings.TrimRight(text, "\t\n")
	strings.TrimPrefix(text, "\xff")
	strings.TrimSuffix(text, "\xff")
	strings.Index(text, "\xff")
	strings.Trim("\xff", "x")
}

func cutsetShapes(text string, given string, condition bool) {
	strings.Trim(text, invalid)
	strings.Trim(text, valid)
	strings.Trim(text, joined)
	strings.Trim(text, string(typed))
	strings.Trim(text, ("\xc3\x28"))
	strings.Trim(text, "\xed\xa0\x80")
	strings.Trim(text, "\U0010FFFF")
	strings.Trim(text, string(rune(0xD800)))
	local := "\xff"
	strings.Trim(text, local)
	strings.Trim(text, given)
	strings.Trim(text, given+"\xff")
	differing := "\xff"
	if condition {
		differing = ""
	}
	strings.Trim(text, differing)
}

func callShapes(text string) {
	defer strings.Trim(text, "\xff")
	go strings.TrimLeft(text, "\xff")
	func() {
		strings.TrimRight(text, "\xff")
	}()
	trim := strings.Trim
	trim(text, "\xff")
	strings.Trim(
		text,
		"\xff",
	)
	if strings.ContainsAny(text, "\xff") {
		return
	}
	_ = trimmed
}

func generic[T any](value T, text string) T {
	strings.Trim(text, "\xff")
	return value
}
