package flagged

import "testing"

// An empty test passes every run and can never fail, so it is judged like any other function.
func TestNothing(test *testing.T) {} // want "body is empty"

func TestSubtest(test *testing.T) {
	test.Parallel()
	test.Run("empty", func(test *testing.T) {}) // want "body is empty"
}
