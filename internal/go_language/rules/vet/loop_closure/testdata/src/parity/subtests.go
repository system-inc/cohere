package parity

import (
	"testing"
)

type testingAlias = testing.T

type embeddingTest struct {
	*testing.T
}

// Parallel subtests, which run after the loop's iteration has moved on.
func subtests(test *testing.T, benchmark *testing.B, wrapped embeddingTest, values []int) {
	for index, value := range values {
		test.Run("parallel", func(subtest *testing.T) {
			subtest.Parallel()
			use(index, value) // want "loop variable index captured by func literal" "loop variable value captured by func literal"
		})
		// Every t.Run in the body is judged, not only the last.
		test.Run("serial", func(subtest *testing.T) {
			use(index, value)
		})
		// Only what follows Parallel runs concurrently.
		test.Run("before", func(subtest *testing.T) {
			use(index)
			subtest.Parallel()
			defer use(value) // want "loop variable value captured by func literal"
		})
		// Parallel on another *testing.T than the subtest's own.
		test.Run("outer", func(subtest *testing.T) {
			test.Parallel()
			use(value)
		})
		// Parallel called through parentheses is not recognized.
		test.Run("parenthesized", func(subtest *testing.T) {
			(subtest.Parallel)()
			use(value)
		})
		test.Run("parenthesized receiver", func(subtest *testing.T) {
			(subtest).Parallel()
			use(value)
		})
		// An unnamed or blank parameter leaves nothing to call Parallel on.
		test.Run("unnamed", func(*testing.T) {
			test.Parallel()
			use(value)
		})
		test.Run("blank", func(_ *testing.T) {
			use(value)
		})
		// A label starts the count again, and a labeled Parallel counts.
		test.Run("labeled", func(subtest *testing.T) {
			subtest.Parallel()
			use(index)
			goto again
		again:
			use(value)
		})
		test.Run("labeled parallel", func(subtest *testing.T) {
			if index > 0 {
				goto parallel
			}
		parallel:
			subtest.Parallel()
			use(value) // want "loop variable value captured by func literal"
		})
		test.Run("nested labels", func(subtest *testing.T) {
			if index > 0 {
				goto first
			}
			goto second
		first:
		second:
			subtest.Parallel()
			use(index) // want "loop variable index captured by func literal"
		})
		// A function value, one argument, a method expression, and a Run that is not a statement of its own.
		function := func(subtest *testing.T) {
			subtest.Parallel()
			use(value)
		}
		test.Run("value", function)
		arguments := func() (string, func(*testing.T)) { return "", nil }
		test.Run(arguments())
		(*testing.T).Run(test, "expression", func(subtest *testing.T) {
			subtest.Parallel()
			use(value)
		})
		_ = test.Run("assigned", func(subtest *testing.T) {
			subtest.Parallel()
			use(value)
		})
		if index > 0 {
			test.Run("in an if", func(subtest *testing.T) {
				subtest.Parallel()
				use(value)
			})
		}
		// Through an alias and an embedded *testing.T.
		test.Run("alias", func(subtest *testingAlias) {
			subtest.Parallel()
			use(value) // want "loop variable value captured by func literal"
		})
		wrapped.Run("embedded", func(subtest *testing.T) {
			subtest.Parallel()
			use(index) // want "loop variable index captured by func literal"
		})
		(test).Run("parenthesized run", func(subtest *testing.T) {
			subtest.Parallel()
			use(value) // want "loop variable value captured by func literal"
		})
		// testing.B's Run is not testing.T's.
		benchmark.Run("benchmark", func(subbenchmark *testing.B) {
			use(value)
		})
	}
}
