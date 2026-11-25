package differences

import (
	"testing"
	"time"
)

// Found by neither: a literal in a test file, judged by its own file.
func TestLiteral(t *testing.T) {
	go func() {
		<-time.Tick(time.Millisecond)
	}()
}
