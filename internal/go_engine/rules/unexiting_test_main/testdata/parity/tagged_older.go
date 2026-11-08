//go:build go1.13

package parity

import "testing"

type taggedOlder struct{}

// Found by both: a file tagged for Go 1.13, whose library does not exit for a TestMain either.
func (taggedOlder) TestMain(m *testing.M) {
	m.Run()
}
