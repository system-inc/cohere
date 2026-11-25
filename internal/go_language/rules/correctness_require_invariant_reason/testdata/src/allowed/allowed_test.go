package allowed

import (
	"regexp"
	"testing"
)

// A test's panic and Must calls fail one test, not a running program, so a _test.go file is not judged.
func TestPanicsAndMustCalls(test *testing.T) {
	test.Parallel()
	_ = regexp.MustCompile(`\d+`)
	defer func() {
		_ = recover()
	}()
	panic("in a test")
}
