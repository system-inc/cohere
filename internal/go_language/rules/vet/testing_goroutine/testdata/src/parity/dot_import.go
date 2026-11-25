package parity

import (
	"testing"
	. "testing"
)

// A test written through a dot import is no `*testing.T` by how it is written, so the declaration is not judged.
func TestDotImport(t *T) {
	go t.Fatal("dot") // clean
}

// A declaration that also takes a test as written is judged, and a dot import's method is the testing package's.
func dotImportHelper(t *T, written *testing.T) {
	go t.Fatal("dot") // reported
}
