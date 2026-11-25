package parity_test

import (
	"testing"
	"time"
)

// Found by neither: a function of an external test package, in a test file.
func TestExternal(t *testing.T) {
	<-time.Tick(time.Millisecond)
}
