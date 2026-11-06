package flagged

import (
	"strings"
	"testing"
)

func compute() int { return 42 }

func TestEmpty(test *testing.T) {} // want `This test cannot fail on a wrong result`

// Computing a result and dropping it checks nothing.
func TestComputesAndDrops(test *testing.T) { // want `This test cannot fail on a wrong result`
	test.Parallel()
	_ = compute()
}

// Log only writes; it never marks the test failed.
func TestLogsOnly(test *testing.T) { // want `This test cannot fail on a wrong result`
	test.Parallel()
	test.Logf("got %d", compute())
}

// A skip only in short mode still runs, and checks nothing, in a full run.
func TestConditionalSkip(test *testing.T) { // want `This test cannot fail on a wrong result`
	if testing.Short() {
		test.Skip("slow")
	}
	_ = compute()
}

// A skip after the work has already run is no skip before anything runs.
func TestSkipAfterWork(test *testing.T) { // want `This test cannot fail on a wrong result`
	_ = compute()
	test.Skip("done")
}

// A subtest that cannot fail gives its parent no way to fail either.
func TestSubtestCannotFail(test *testing.T) { // want `This test cannot fail on a wrong result`
	test.Parallel()
	test.Run("inner", func(subtest *testing.T) {
		subtest.Parallel()
		subtest.Log(compute())
	})
}

// A closure that only logs is no way to fail, wherever it runs.
func TestCleanupOnlyLogs(test *testing.T) { // want `This test cannot fail on a wrong result`
	test.Parallel()
	test.Cleanup(func() {
		test.Log("cleaned")
	})
}

// The test's name handed to a helper is a string; the test itself goes nowhere.
func TestHandsOnlyTheName(test *testing.T) { // want `This test cannot fail on a wrong result`
	test.Parallel()
	_ = strings.ToUpper(test.Name())
	_ = test.TempDir()
}

// A blank parameter cannot be called, so nothing can fail the test.
func TestBlankParameter(_ *testing.T) { // want `This test cannot fail on a wrong result`
	_ = compute()
}

// checker has an Error method of its own, which fails nothing.
type checker struct{}

func (checker) Error(message string) {}

func TestLookalikeError(test *testing.T) { // want `This test cannot fail on a wrong result`
	test.Parallel()
	checker{}.Error("lookalike")
}

// A writer with no Errorf method is no reporter, so a value written to it is not handed to anything that could fail.
func TestStringBuilder(test *testing.T) { // want `This test cannot fail on a wrong result`
	test.Parallel()
	var builder strings.Builder
	builder.WriteString(test.Name())
}
