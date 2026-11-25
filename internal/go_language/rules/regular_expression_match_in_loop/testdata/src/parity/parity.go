// Package parity holds loops of each shape around regexp's matching functions, for with and without a condition,
// range over a slice, a map, a channel, an integer and a function, labelled, nested, a goto back, a closure inside a
// loop, blocks before and after a loop, a pattern constant and not, and the shapes staticcheck's representation builds
// the call in more than one way.
package parity

import (
	"io"
	"regexp"
	"strings"
)

const digits = `\d+`

type pattern string

const typed pattern = `a+`

var matched, _ = regexp.MatchString(digits, "1")

func loops(values []string, table map[string]int, channel chan string, reader io.RuneReader, condition bool) {
	regexp.MatchString(digits, "before")
	for index := 0; index < 3; index++ {
		regexp.MatchString(digits, values[index])
	}
	for _, value := range values {
		regexp.MatchString(`a`, value)
		regexp.Match(`a`, []byte(value))
		regexp.MatchReader(`a`, reader)
	}
	for key := range table {
		regexp.MatchString(digits, key)
	}
	for value := range channel {
		regexp.MatchString(digits, value)
	}
	for range 3 {
		regexp.MatchString(digits, "")
	}
	for condition {
		regexp.MatchString(digits, "")
		condition = false
	}
	regexp.MatchString(digits, "after")
}

func shapes(values []string, condition bool) {
outer:
	for _, first := range values {
		for _, second := range values {
			if ok, _ := regexp.MatchString(digits, first+second); ok {
				continue outer
			}
		}
		regexp.MatchString(string(typed), first)
	}
	count := 0
again:
	regexp.MatchString(digits, "goto")
	count++
	if count < 3 {
		goto again
	}
	for _, value := range values {
		func() {
			regexp.MatchString(digits, value)
		}()
		matcher := func() bool {
			found, _ := regexp.MatchString(digits, value)
			return found
		}
		_ = matcher()
	}
	for {
		if condition {
			break
		}
		regexp.MatchString(digits, "before break")
		break
	}
	for _, value := range values {
		if condition {
			return
		}
		_ = value
	}
	regexp.MatchString(digits, "after return")
}

func notConstant(values []string, patterns []string, condition bool) {
	for index, value := range values {
		regexp.MatchString(patterns[index], value)
		regexp.MatchString(value+"a", value)
		differing := "a"
		if condition {
			differing = "b"
		}
		regexp.MatchString(differing, value)
		agreeing := "a"
		if condition {
			agreeing = "a"
		}
		regexp.MatchString(agreeing, value)
		local := `[a-z]`
		regexp.MatchString(local, value)
		compiled := regexp.MustCompile(`a`)
		compiled.MatchString(value)
		regexp.MustCompile(`a`).MatchString(value)
		strings.Contains(value, "a")
	}
}

func callShapes(values []string, reader io.RuneReader) {
	for _, value := range values {
		defer regexp.MatchString(digits, value)
		go regexp.Match(digits, nil)
		match := regexp.MatchString
		match(digits, value)
		regexp.MatchReader(
			digits,
			reader,
		)
		if found, matchError := regexp.MatchString(digits, value); found || matchError != nil {
			continue
		}
	}
	_ = matched
}

func sequence(yield func(string) bool) {
	for _, value := range []string{"a"} {
		if !yield(value) {
			return
		}
	}
}

func rangeOverFunction() {
	for value := range sequence {
		regexp.MatchString(digits, value)
	}
}

func generic[T any](values []T) {
	for range values {
		regexp.MatchString(digits, "")
	}
}

func instantiated() {
	generic([]int{1})
}
