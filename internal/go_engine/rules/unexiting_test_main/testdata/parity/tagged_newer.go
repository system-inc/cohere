//go:build go1.15

package parity

import "testing"

type taggedNewer struct{}

// Found by neither: a file tagged for Go 1.15, whose library exits for a TestMain, in a module older than Go 1.21.
func (taggedNewer) TestMain(m *testing.M) {
	m.Run()
}
