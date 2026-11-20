//go:build linux

package partial

// Limit is declared an int from a string on linux, which does not type-check.
const Limit int = "eight"
