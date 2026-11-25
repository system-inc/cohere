// Package parity holds the shapes staticcheck's representation builds a cutset or the call in more than one way, and
// cutsets of each kind: runes that repeat side by side or apart, runes of several bytes, bytes that are not UTF-8,
// and the cutsets of one rune or none, whose runes cannot repeat.
package parity

import "strings"

type cutset string

const typed cutset = "xx"

const (
	repeated = "aa"
	distinct = "ab"
	joined   = "a" + "ba"
)

var trimmed = strings.Trim("value", "..")

func init() {
	_ = strings.TrimRight("value", "\n\r\n")
}

func cutsetsOfEachKind(value string) {
	_ = strings.Trim(value, "aba")
	_ = strings.Trim(value, "abcdefa")
	_ = strings.TrimLeft(value, "ab")
	_ = strings.TrimRight(value, "  ")
	_ = strings.Trim(value, "µµ")
	_ = strings.Trim(value, "µu")
	_ = strings.Trim(value, "日本日")
	_ = strings.Trim(value, "日本")
	_ = strings.Trim(value, "\xff\xfe")
	_ = strings.Trim(value, "\xff")
	_ = strings.Trim(value, "\xff�")
	_ = strings.Trim(value, "aA")
	_ = strings.Trim(value, "x")
	_ = strings.Trim(value, "")
	_ = strings.TrimLeft(value, "https://")
	_ = strings.TrimRight(value, ".go")
	_ = strings.TrimRight(value, ".gogo")
}

func constantShapes(value string) {
	_ = strings.Trim(value, string(typed))
	_ = strings.Trim(value, repeated)
	_ = strings.Trim(value, distinct)
	_ = strings.Trim(value, joined)
	_ = strings.Trim(value, distinct+"a")
	_ = strings.Trim(value, ("zz"))
	local := "qq"
	_ = strings.Trim(value, local)
	const declared = "ww"
	_ = strings.TrimLeft(value, declared)
}

func valuesNotConstant(value string, set string, condition bool) {
	_ = strings.Trim(value, set)
	_ = strings.Trim(value, set+"aa")
	_ = strings.Trim(value, strings.Repeat("a", 2))
	differing := "aa"
	if condition {
		differing = "bb" // Two different constants meet in a phi, which is no one constant.
	}
	_ = strings.Trim(value, differing)
	agreeing := "aa"
	if condition {
		agreeing = "aa"
	}
	_ = strings.Trim(value, agreeing)
}

func otherFunctions(value string) {
	_ = strings.TrimPrefix(value, "aa")
	_ = strings.TrimSuffix(value, "aa")
	_ = strings.TrimFunc(value, func(r rune) bool { return r == 'a' })
	_ = strings.TrimSpace(value)
}

func callShapes(value string) {
	defer strings.Trim(value, "aa")
	go strings.TrimLeft(value, "bb")
	func() {
		_ = strings.TrimRight(value, "cc")
	}()
	trim := strings.Trim
	_ = trim(value, "dd")
	_ = strings.Trim(
		value,
		"ee",
	)
	if strings.TrimLeft(value, "ff") == "" {
		return
	}
	_ = strings.Trim(strings.Trim(value, "gg"), "hh")
	_ = trimmed
}

func generic[T any](value T, text string) T {
	_ = strings.Trim(text, "ii")
	return value
}

func instantiated() {
	generic(1, "")
}
