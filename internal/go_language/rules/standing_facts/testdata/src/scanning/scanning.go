// Package scanning is the build's package the scannererr presence test settles: each variable says beside it whether
// the rule's predicate holds of it.
package scanning

import (
	"bufio"
	"os"
	"strings"
)

// Input is scannererr's candidate: a scanner over the process's input, which can fail to read.
var Input = bufio.NewScanner(os.Stdin)

// Assigned is a candidate too, made one by Open's `=` alone.
var Assigned *bufio.Scanner

// Memory is no candidate of the rule's, whose reader is in memory and never fails, but it is a *bufio.Scanner, which
// the row names whatever made it.
var Memory = bufio.NewScanner(strings.NewReader("line"))

// Source is no scanner at all.
var Source = strings.NewReader("line")

// Lines names Input.
func Lines() int {
	count := 0
	for Input.Scan() {
		count++
	}
	return count
}

// Open assigns Assigned a scanner over the process's input.
func Open() { Assigned = bufio.NewScanner(os.Stdin) }

// Count names no variable.
func Count() int { return 1 }

// Remembered names Memory.
func Remembered() bool { return Memory.Scan() }

// Sourced names Source.
func Sourced() int { return Source.Len() }

// lineReader is an interface a scanner implements, which In is read through.
type lineReader interface {
	Scan() bool
	Err() error
}

// In is a candidate only the walk names: a package-level specification makes it, and its type is no *bufio.Scanner.
var In lineReader = bufio.NewScanner(os.Stdin)

// ReadsIn names In.
func ReadsIn() bool { return In.Scan() }
