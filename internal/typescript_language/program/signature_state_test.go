package program

import (
	"path/filepath"
	"testing"
)

// state is one build's signatures and both fingerprints, keyed by base name.
type signatureState struct {
	signatures map[string]SignatureEntry
	computed   int
	emits      int
	bySignature,
	byContent map[string]string
}

func (state signatureState) signature(test *testing.T, name string) string {
	test.Helper()
	for fileName, entry := range state.signatures {
		if filepath.Base(fileName) == name {
			return entry.Signature
		}
	}
	test.Fatalf("no signature for %s", name)
	return ""
}
