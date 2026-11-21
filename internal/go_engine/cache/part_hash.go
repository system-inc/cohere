package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"unsafe"
)

// partHash is a sha256 written one part at a time, each part prefixed with its length, so no two different
// sequences of parts hash alike by running together: "ab" then "c" is not "a" then "bc".
type partHash struct {
	digest hash.Hash

	// prefix is where each part's length, and a count's value, is encoded before it is written. A local array
	// passed to the digest's Write escapes through the hash.Hash interface, one allocation per part; this one lives
	// in the hasher. Each hasher is made by one call and written on that call's goroutine alone, and the digest's
	// Write, the last reader of the bytes, copies them before it returns and keeps nothing (io.Writer's contract),
	// so the next part may write over them.
	prefix [8]byte
}

// newPartHash is an empty partHash.
func newPartHash() *partHash {
	return &partHash{digest: sha256.New()}
}

// length writes a part's length, the prefix every part opens with.
func (partHash *partHash) length(partLength int) {
	binary.BigEndian.PutUint64(partHash.prefix[:], uint64(partLength))
	// Ignored: a hash.Hash's Write never returns an error.
	_, _ = partHash.digest.Write(partHash.prefix[:])
}

// bytes writes one part.
func (partHash *partHash) bytes(contents []byte) {
	partHash.length(len(contents))
	// Ignored: a hash.Hash's Write never returns an error.
	_, _ = partHash.digest.Write(contents)
}

// text writes one part, a string.
func (partHash *partHash) text(value string) {
	partHash.length(len(value))
	// Unsafe: the string's bytes are viewed without the copy []byte(value) makes, and the digest's Write only reads
	// them and keeps nothing (io.Writer's contract), so they never change.
	// Ignored: a hash.Hash's Write never returns an error.
	_, _ = partHash.digest.Write(unsafe.Slice(unsafe.StringData(value), len(value)))
}

// count writes one part, a count, which goes before the parts of a list so lists of different lengths never
// line up: the part's length, eight, then the count in the same eight bytes, which the length's Write has
// already copied.
func (partHash *partHash) count(value int) {
	partHash.length(len(partHash.prefix))
	binary.BigEndian.PutUint64(partHash.prefix[:], uint64(value))
	// Ignored: a hash.Hash's Write never returns an error.
	_, _ = partHash.digest.Write(partHash.prefix[:])
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

// DigestOf is the sha256 of parts, each length-prefixed as a key's parts are, for a digest the findings cache makes
// of something other than a package's inputs.
func DigestOf(parts ...[]byte) Key {
	digest := newPartHash()
	for _, part := range parts {
		digest.bytes(part)
	}
	return digest.sum()
}
