package flagged

import "testing"

// A label in a test's subtest literal is judged as any other, a test file included.
func TestDrain(test *testing.T) {
	test.Parallel()
	test.Run("empty", func(test *testing.T) {
		test.Parallel()
		values := []int{1, 2}
	values: // want "`values` is a label"
		for range values {
			break values
		}
	})
}
