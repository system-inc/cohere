// Package scanned makes its scanner only in setup, which the test strips.
package scanned

import (
	"bufio"
	"io"
)

var reader io.Reader

var input *bufio.Scanner

func setup() { input = bufio.NewScanner(reader) }
