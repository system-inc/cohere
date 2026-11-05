// Package allowed holds integers that already name their base, or are decimal, which the rule must leave alone.
package allowed

import "os"

// Zero is decimal zero, whatever base it might be read in.
const zero = 0

// Octal written with its prefix, with and without separators. An upper-case prefix is not fixtured, since gofmt
// lowers it and the guard holds testdata to gofmt.
const (
	executable = 0o755
	separated  = 0o_600
	grouped    = 0o7_5_5
)

// The other prefixed bases.
const (
	hexadecimal = 0x1F
	binary      = 0b101
)

// Decimal, with and without separators.
const (
	answer   = 42
	thousand = 1_000
)

// A file mode written the way the rule asks.
func writeConfiguration(path string, contents []byte) error {
	return os.WriteFile(path, contents, 0o644)
}
