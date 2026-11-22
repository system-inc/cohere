package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// lengthPrefixedSum is the sha256 a partHash must make of parts, written out by hand with a fresh array for each
// prefix, the encoding every key in the store was named with: each part's length as eight big-endian bytes, then
// the part.
func lengthPrefixedSum(parts ...[]byte) Key {
	digest := sha256.New()
	for _, part := range parts {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		// Ignored: a hash.Hash's Write never returns an error.
		_, _ = digest.Write(length[:])
		// Ignored: a hash.Hash's Write never returns an error.
		_, _ = digest.Write(part)
	}
	return Key(digest.Sum(nil))
}

// encodedCount is a count's part as a partHash writes it, eight big-endian bytes.
func encodedCount(count int) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(count))
	return encoded[:]
}

// A partHash writes each part's length through one prefix it keeps and a string without copying it, and the sum is
// still the encoding the store's keys were named with: bytes, a string, a count whose value goes through the same
// prefix as its length, and an empty part.
func TestPartHashKeepsTheLengthPrefixedEncoding(test *testing.T) {
	test.Parallel()
	for _, example := range []struct {
		name     string
		contents []byte
		text     string
		count    int
	}{
		{name: "empty"},
		{name: "binary", contents: []byte{0, 255, 128}, text: "\x00\xffé", count: -1},
		{name: "longer than a block", contents: bytes.Repeat([]byte{42}, 130), text: strings.Repeat("日", 93),
			count: 1024},
	} {
		test.Run(example.name, func(test *testing.T) {
			test.Parallel()
			parts := newPartHash()
			parts.bytes(example.contents)
			parts.text(example.text)
			parts.count(example.count)
			parts.bytes(nil)
			expected := lengthPrefixedSum(example.contents, []byte(example.text), encodedCount(example.count), nil)
			if sum := parts.sum(); sum != expected {
				test.Errorf("the part hash is %s, want %s", sum, expected)
			}
		})
	}
}

// Keys are hashed on many goroutines at once, so the prefix a partHash reuses must be its own: hashers side by side,
// each writing its own lengths and counts through its prefix while the others write theirs, each sum the encoding of
// its own parts. A prefix shared between hashers would have one's length written over by another's before its
// digest read it.
func TestPartHashesSideBySideKeepTheirOwnPrefixes(test *testing.T) {
	test.Parallel()
	const hashers = 8
	const rounds = 2000
	var group sync.WaitGroup
	mismatches := make([]string, hashers)
	for hasher := range hashers {
		group.Go(func() {
			for round := range rounds {
				contents := bytes.Repeat([]byte{byte(hasher)}, hasher+round%7)
				text := strings.Repeat("x", hasher*3+round%5)
				parts := newPartHash()
				parts.count(hasher*rounds + round)
				parts.bytes(contents)
				parts.text(text)
				expected := lengthPrefixedSum(encodedCount(hasher*rounds+round), contents, []byte(text))
				if sum := parts.sum(); sum != expected {
					mismatches[hasher] = fmt.Sprintf("hasher %d, round %d: the part hash is %s, want %s", hasher,
						round, sum, expected)
					return
				}
			}
		})
	}
	group.Wait()
	for _, mismatch := range mismatches {
		if mismatch != "" {
			test.Error(mismatch)
		}
	}
}
