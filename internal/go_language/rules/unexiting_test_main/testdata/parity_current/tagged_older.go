//go:build go1.14

package parity_current

import "testing"

type taggedOlder struct{}

// Found by neither: a file tagged older than the module, which since Go 1.21 still has at least the module's library.
func (taggedOlder) TestMain(m *testing.M) {
	m.Run()
}
