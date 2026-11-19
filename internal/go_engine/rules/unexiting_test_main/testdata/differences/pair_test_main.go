package differences

import "testing"

type pairInAnotherFile struct{}

// Found by the port alone: the call to os.Exit in pair_exit.go, at package level in another file, is no exit of this
// TestMain's, though the original counts it for this one and misses it.
func (pairInAnotherFile) TestMain(m *testing.M) { // port finds: TestMain should call os.Exit to set exit code
	m.Run()
}
