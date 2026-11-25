// Package lacked reads a constant of a package the host records no uses of, beside a command an extra build's load
// leaves out, its test variant standing in for it.
package lacked

import "example.com/lacked/linked"

// Scaled reads linked's Scale, which no build changes.
func Scaled(value int) int {
	return value * linked.Scale
}

// Plain reads nothing of another package.
func Plain(value int) int {
	return -value
}

// Mode is the build's mode, which each build's own file declares.
func Mode() string {
	return mode
}
