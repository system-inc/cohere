package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Started, the executable's digest is hashed before anything asks for it, so a run that starts it as it begins finds
// it ready when its first key asks rather than hashing it there, between its listing and its type check: hashing
// begins with no get, and the get that follows returns the file's digest.
func TestAStartedDigestIsHashedBeforeTheFirstAsk(test *testing.T) {
	test.Parallel()

	path := filepath.Join(test.TempDir(), "cohere")
	contents := []byte("the binary the process runs")
	if writeError := os.WriteFile(path, contents, 0o755); writeError != nil {
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
	hashing := make(chan struct{})
	digest := newExecutableDigest(opened, func() { close(hashing) })
	digest.start()
	select {
	case <-hashing:
	case <-time.After(10 * time.Second):
		test.Fatal("ten seconds after the start, nothing had begun hashing the executable, which nothing had asked for")
	}
	identity, identityError := digest.get()
	if identityError != nil {
		test.Fatal(identityError)
	}
	sum := sha256.Sum256(contents)
	if want := hex.EncodeToString(sum[:]); identity != want {
		test.Errorf("the started digest is %s, want the file's %s", identity, want)
	}
}
