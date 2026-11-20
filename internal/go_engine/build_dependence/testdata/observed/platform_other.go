//go:build !linux

package observed

// Length is 4 everywhere but linux and 8 on linux.
const Length = 4

// word is an int32 everywhere but linux and a func() on linux, which prints with no verb an int32 prints with.
type word = int32
