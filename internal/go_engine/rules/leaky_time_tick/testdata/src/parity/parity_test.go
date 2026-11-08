package parity

import (
	"testing"
	"time"
)

// Found by neither: a test may leak a ticker, since it ends with the test binary.
func TestTicks(t *testing.T) {
	<-time.Tick(time.Millisecond)
	func() {
		<-time.Tick(time.Millisecond)
	}()
}

// Found by neither: a helper in a test file.
func waitTick() {
	<-time.Tick(time.Millisecond)
}
