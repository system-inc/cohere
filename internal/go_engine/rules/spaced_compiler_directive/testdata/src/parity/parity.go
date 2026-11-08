// Package parity holds comments that read as compiler directives with a space or tab after their slashes, and
// comments that do not, every kind gofmt leaves where it is, where the port and staticcheck's SA9009 still agree
// finding for finding. The differential test runs both over each and compares.
package parity

import _ "unsafe"

// Found by both: one space before the directive.

// go:noinline
func oneSpace() {}

// Found by both: three spaces, in a comment that documents nothing, since gofmt rewrites a doc comment's spacing.

//   go:noinline

func threeSpaces() {}

// Found by both: a tab, in a comment that documents nothing.

//	go:nosplit

func tab() {}

// Found by both: a space then a tab, in a comment that documents nothing.

// 	go:norace

func spaceThenTab() {}

// Found by both: a directive with arguments.

// go:linkname linked runtime.nanotime
func linked() int64 { return 0 }

// Found by both: an unknown directive is judged by its shape alone.

// go:whatever we like
func unknown() {}

// Found by both: an embed directive on a variable.

// go:embed parity.go
var embedded string

// Found by neither: the directive is written right.

//go:noinline
func right() {}

// Found by neither: an uppercase letter after go:.

// go:Noinline
func uppercase() {}

// Found by neither: a digit after go:.

// go:1
func digit() {}

// Found by neither: nothing after go:.

// go:
func nothingAfter() {}

// Found by neither: a space after go:, prose about Go.

// go: is a language
func prose() {}

// Found by neither: no go: at all.

// golang:noinline
func otherPrefix() {}

// Found by neither: a block comment is never a directive.

/* go:noinline */
func blockComment() {}

// Found by neither: a comment after code does not start its line.
var trailing = 1 // go:noinline

func insideBody() {
	// Found by neither: an indented comment does not start its line.
	// go:noinline
	_ = trailing
}
