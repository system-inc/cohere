// Package gate drops the error of a digest's Write, which correctness-require-handled-error exempts only where hash
// is in the package's import closure: dep imports hash on the host alone, so the host exempts Target and windows
// reports it.
package gate

import _ "example.test/gate/dep"

// digest has hash.Hash's methods by shape.
type digest struct{}

// Write takes data whole.
func (*digest) Write(data []byte) (int, error) { return len(data), nil }

// Sum appends nothing to prefix.
func (*digest) Sum(prefix []byte) []byte { return prefix }

// Reset does nothing.
func (*digest) Reset() {}

// Size is the sum's length.
func (*digest) Size() int { return 0 }

// BlockSize is the block the digest takes.
func (*digest) BlockSize() int { return 1 }

// Target writes data into a digest and drops the error.
func Target(data []byte) {
	written := &digest{}
	written.Write(data)
}
