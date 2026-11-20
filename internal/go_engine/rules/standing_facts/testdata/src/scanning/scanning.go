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

// Memory is no candidate: its reader is in memory, and never fails.
var Memory = bufio.NewScanner(strings.NewReader("line"))

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
