// Package parity calls each FindAll method with a limit of zero and with one that is not, in the shapes
// staticcheck's representation builds a limit or a call in more than one way.
package parity

import "regexp"

const none = 0

type count int

var pattern = regexp.MustCompile(`a`)

var everything = pattern.FindAllString("aaa", 0)

func eachMethod(input []byte, text string) {
	pattern.FindAll(input, 0)
	pattern.FindAll(input, -1)
	pattern.FindAllIndex(input, 0)
	pattern.FindAllIndex(input, -1)
	pattern.FindAllString(text, 0)
	pattern.FindAllString(text, -1)
	pattern.FindAllStringIndex(text, 0)
	pattern.FindAllStringIndex(text, 2)
	pattern.FindAllStringSubmatch(text, 0)
	pattern.FindAllStringSubmatch(text, -1)
	pattern.FindAllStringSubmatchIndex(text, 0)
	pattern.FindAllStringSubmatchIndex(text, -1)
	pattern.FindAllSubmatch(input, 0)
	pattern.FindAllSubmatch(input, 1)
	pattern.FindAllSubmatchIndex(input, 0)
	pattern.FindAllSubmatchIndex(input, -1)
	pattern.FindString(text)
	pattern.ReplaceAllString(text, "")
}

func limitShapes(input []byte, limit int, condition bool) {
	pattern.FindAll(input, none)
	pattern.FindAll(input, int(count(0)))
	pattern.FindAll(input, 1-1)
	pattern.FindAll(input, (0))
	local := 0
	pattern.FindAll(input, local)
	pattern.FindAll(input, limit)
	pattern.FindAll(input, limit*0)
	differing := 0
	if condition {
		differing = -1
	}
	pattern.FindAll(input, differing)
	agreeing := 0
	if condition {
		agreeing = 0
	}
	pattern.FindAll(input, agreeing)
}

func receiverShapes(input []byte, given *regexp.Regexp, value regexp.Regexp) {
	given.FindAll(input, 0)
	value.FindAll(input, 0)
	regexp.MustCompile(`b`).FindAll(input, 0)
	findAll := pattern.FindAll
	findAll(input, 0)
	method := (*regexp.Regexp).FindAll
	method(pattern, input, 0)
}

func callShapes(input []byte) {
	defer pattern.FindAll(input, 0)
	go pattern.FindAll(input, 0)
	func() {
		pattern.FindAll(input, 0)
	}()
	pattern.FindAll(
		input,
		0,
	)
	if found := pattern.FindAll(input, 0); found != nil {
		return
	}
	_ = everything
}

func generic[T any](value T, input []byte) T {
	pattern.FindAll(input, 0)
	return value
}
