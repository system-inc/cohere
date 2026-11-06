package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
)

// partHash is a sha256 written one part at a time, each part prefixed with its length, so no two different
// sequences of parts hash alike by running together: "ab" then "c" is not "a" then "bc".
type partHash struct {
	digest hash.Hash
}

// newPartHash is an empty partHash.
func newPartHash() *partHash {
	return &partHash{digest: sha256.New()}
}

// bytes writes one part.
func (partHash *partHash) bytes(contents []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(contents)))
	// Ignored: a hash.Hash's Write never returns an error.
	_, _ = partHash.digest.Write(length[:])
	// Ignored: a hash.Hash's Write never returns an error.
	_, _ = partHash.digest.Write(contents)
}

// text writes one part, a string.
func (partHash *partHash) text(value string) {
	partHash.bytes([]byte(value))
}

// count writes one part, a count, which goes before the parts of a list so lists of different lengths never
// line up.
func (partHash *partHash) count(value int) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	partHash.bytes(encoded[:])
}

// texts writes a list of strings: its count, then each.
func (partHash *partHash) texts(values []string) {
	partHash.count(len(values))
	for _, value := range values {
		partHash.text(value)
	}
}

// sum is the sha256 of every part written.
func (partHash *partHash) sum() Key {
	var key Key
	copy(key[:], partHash.digest.Sum(nil))
	return key
}
