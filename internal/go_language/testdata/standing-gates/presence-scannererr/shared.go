package gate

import (
	"bufio"
	"io"
)

// source is a reader that can fail, of a type no platform declares otherwise.
var source io.Reader

// input is scannererr's candidate, declared in the file both builds compile.
var input = bufio.NewScanner(source)

// Lines names input, so scannererr reads it.
func Lines() int {
	count := 0
	for input.Scan() {
		count++
	}
	return count
}

// Port names no candidate.
func Port() int { return 80 }
