// Package parity calls both functions with a count of zero and with one that is not, in the shapes staticcheck's
// representation builds a count or a call in more than one way.
package parity

import (
	"bytes"
	"strings"
)

const none = 0

type count int

var replaced = strings.Replace("a", "a", "b", 0)

func eachFunction(text string, data []byte) {
	strings.Replace(text, "a", "b", 0)
	strings.Replace(text, "a", "b", -1)
	bytes.Replace(data, []byte("a"), []byte("b"), 0)
	bytes.Replace(data, []byte("a"), []byte("b"), 2)
	strings.ReplaceAll(text, "a", "b")
	bytes.ReplaceAll(data, []byte("a"), []byte("b"))
	strings.NewReplacer("a", "b").Replace(text)
	strings.SplitN(text, ",", 0)
}

func countShapes(text string, limit int, condition bool) {
	strings.Replace(text, "a", "b", none)
	strings.Replace(text, "a", "b", int(count(0)))
	strings.Replace(text, "a", "b", 1-1)
	strings.Replace(text, "a", "b", (0))
	local := 0
	strings.Replace(text, "a", "b", local)
	strings.Replace(text, "a", "b", limit)
	strings.Replace(text, "a", "b", limit*0)
	differing := 0
	if condition {
		differing = -1
	}
	strings.Replace(text, "a", "b", differing)
	agreeing := 0
	if condition {
		agreeing = 0
	}
	strings.Replace(text, "a", "b", agreeing)
}

func callShapes(text string) {
	defer strings.Replace(text, "a", "b", 0)
	go bytes.Replace(nil, nil, nil, 0)
	func() {
		strings.Replace(text, "a", "b", 0)
	}()
	replace := strings.Replace
	replace(text, "a", "b", 0)
	strings.Replace(
		text,
		"a",
		"b",
		0,
	)
	if result := strings.Replace(text, "a", "b", 0); result != "" {
		return
	}
	_ = replaced
}

func generic[T any](value T, text string) T {
	strings.Replace(text, "a", "b", 0)
	return value
}
