package test_main_file

import "testing"

// Found by the port alone: the call to os.Exit in exit.go, at package level, is no exit of this TestMain's, though
// the original counts it for this one and misses it.
func TestMain(m *testing.M) { // port finds: TestMain should call os.Exit to set exit code
	m.Run()
}
