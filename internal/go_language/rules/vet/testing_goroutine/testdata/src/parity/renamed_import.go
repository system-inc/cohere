package parity

import (
	renamed "testing"
)

// A test written through a renamed import is no `*testing.T` by how it is written, so the declaration is not judged.
func TestRenamedImport(t *renamed.T) {
	go t.Fatal("renamed") // clean
}
