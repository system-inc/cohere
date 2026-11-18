package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// A binary renamed over the executable's path while the process runs, as `go build` and `go install` write one, never
// becomes the running process's identity: the process hashes the file it opened as it started, through that
// descriptor, so its identity is the bytes it runs.
func TestAnExecutableRenamedOverIsHashedAsItWasOpened(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	path := filepath.Join(directory, "cohere")
	running := []byte("the binary the process runs")
	if writeError := os.WriteFile(path, running, 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	opened := openedExecutableAt(path)
	if opened.openError != nil {
		test.Fatal(opened.openError)
	}
	test.Cleanup(func() {
		// Ignored: the file was only read.
		_ = opened.file.Close()
	})
	rebuilt := filepath.Join(directory, "cohere-rebuilt")
	if writeError := os.WriteFile(rebuilt, []byte("a binary built since"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	if renameError := os.Rename(rebuilt, path); renameError != nil {
		test.Fatal(renameError)
	}
	identity, identityError := opened.digest()
	if identityError != nil {
		test.Fatal(identityError)
	}
	sum := sha256.Sum256(running)
	if want := hex.EncodeToString(sum[:]); identity != want {
		test.Errorf("the identity is %s, the digest of the binary renamed over the path, want %s, the one opened",
			identity, want)
	}
}
