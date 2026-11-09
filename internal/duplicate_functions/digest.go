package duplicate_functions

import (
	"bytes"
	"encoding/hex"
	"fmt"
)

// Digest is a function's canonical hash, or one value's within it: the first sixteen bytes of a SHA-256. Two
// functions with equal digests compute the same thing, as the hash spec on #6rj7jw7 defines the same.
type Digest [16]byte

// Less orders digests by their bytes, which is how a commutative operator's operands are put in one order.
func (digest Digest) Less(other Digest) bool {
	return bytes.Compare(digest[:], other[:]) < 0
}

// String is the digest in hex.
func (digest Digest) String() string {
	return hex.EncodeToString(digest[:])
}

// MarshalText writes the digest as hex, so a record carries a string rather than sixteen numbers.
func (digest Digest) MarshalText() ([]byte, error) {
	return []byte(digest.String()), nil
}

// UnmarshalText reads the hex MarshalText writes, and refuses anything else.
func (digest *Digest) UnmarshalText(text []byte) error {
	decoded, decodeError := hex.DecodeString(string(text))
	if decodeError != nil {
		return fmt.Errorf("reading a digest: %w", decodeError)
	}
	if len(decoded) != len(digest) {
		return fmt.Errorf("reading a digest: it is %d bytes, not %d", len(decoded), len(digest))
	}
	copy(digest[:], decoded)
	return nil
}
