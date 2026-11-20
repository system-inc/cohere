//go:build arm64

package architecturesizes

// padSize is 64 on amd64 and 128 on arm64.
const padSize = 128

// word is an int32 on amd64 and a func() on arm64: a func() prints with no verb an int32 prints with, and compares
// with nothing.
type word = func()

// mark is an int32 on amd64 and a string on arm64: each compares, so it can key a map, and they print with different
// verbs.
type mark = string
