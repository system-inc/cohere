//go:build !linux

package partial

// Limit is 4 everywhere but linux.
const Limit = 4
