package cache

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Hashing a file from disk reads it through the buffer its worker gives, so a run hashing thousands of files allocates
// no buffer for each, and the digest is the sha256 of every byte, past the buffer's end too.
//
// Not parallel: the bytes allocated are read from the whole process's counters, which another test's allocations
// would move.
func TestAFileIsHashedThroughItsWorkersBuffer(test *testing.T) {
	file := filepath.Join(test.TempDir(), "large.go")
	contents := bytes.Repeat([]byte("package large // a line of a file larger than one buffer\n"), 4096)
	if writeError := os.WriteFile(file, contents, 0o600); writeError != nil {
		test.Fatal(writeError)
	}
	buffer := make([]byte, digestBufferSize)
	digest, digestError := digestOf(file, nil, buffer)
	if digestError != nil || digest != sha256.Sum256(contents) {
		test.Fatalf("hashing a file larger than the buffer gave %x with %v, want the sha256 of its bytes", digest,
			digestError)
	}

	const hashes = 20
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range hashes {
		if _, digestError := digestOf(file, nil, buffer); digestError != nil {
			test.Fatal(digestError)
		}
	}
	runtime.ReadMemStats(&after)
	if perHash := (after.TotalAlloc - before.TotalAlloc) / hashes; perHash >= digestBufferSize/2 {
		test.Errorf("hashing a file allocated %d bytes, want well under one buffer of %d", perHash, digestBufferSize)
	}
}
