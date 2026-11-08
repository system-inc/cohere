// Package parity_current holds a TestMain that never exits in a module of Go 1.22, whose test framework exits for
// it, where neither the port nor staticcheck's SA3000 reports.
package parity_current

import "testing"

// Found by neither: the library exits for it.
func TestMain(m *testing.M) {
	m.Run()
}
