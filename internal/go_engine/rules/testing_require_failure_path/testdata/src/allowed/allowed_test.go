package allowed

import (
	"testing"

	"reporting"
)

func compute() int { return 42 }

func TestError(test *testing.T) {
	test.Parallel()
	if compute() != 42 {
		test.Error("wrong")
	}
}

func TestErrorf(test *testing.T) {
	if got := compute(); got != 42 {
		test.Errorf("got %d", got)
	}
}

func TestFatal(test *testing.T) {
	if compute() != 42 {
		test.Fatal("wrong")
	}
}

func TestFatalf(test *testing.T) {
	if got := compute(); got != 42 {
		test.Fatalf("got %d", got)
	}
}

func TestFail(test *testing.T) {
	if compute() != 42 {
		test.Fail()
	}
}

func TestFailNow(test *testing.T) {
	if compute() != 42 {
		test.FailNow()
	}
}

// A subtest that can fail fails its parent with it.
func TestSubtestFails(test *testing.T) {
	test.Parallel()
	test.Run("inner", func(subtest *testing.T) {
		subtest.Parallel()
		if compute() != 42 {
			subtest.Error("wrong")
		}
	})
}

// A closure that can fail counts wherever it runs: deferred, in a goroutine, or at cleanup.
func TestCleanupFails(test *testing.T) {
	test.Cleanup(func() {
		if compute() != 42 {
			test.Error("wrong")
		}
	})
}

func subtestNamed(test *testing.T) {
	if compute() != 42 {
		test.Error("wrong")
	}
}

// A subtest named rather than spelled runs a body the walk cannot reach, so it is trusted to fail.
func TestRunNamedSubtest(test *testing.T) {
	test.Run("named", subtestNamed)
}

func check(test *testing.T, got int) {
	test.Helper()
	if got != 42 {
		test.Errorf("got %d", got)
	}
}

// A helper handed the test can fail it.
func TestHelper(test *testing.T) {
	test.Parallel()
	check(test, compute())
}

func checkThrough(reporter testing.TB, got int) {
	if got != 42 {
		reporter.Errorf("got %d", got)
	}
}

func TestHelperTakingTB(test *testing.T) {
	checkThrough(test, compute())
}

// errorsOnly is a reporter of its own, the shape analysistest.Run takes.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Errorf(format, arguments...)
}

// Another package handed a reporter that wraps the test can fail it.
func TestReporterToAnotherPackage(test *testing.T) {
	test.Parallel()
	reporting.Run(errorsOnly{test}, "flagged", "allowed")
}

// suite holds the test, so its methods can fail it.
type suite struct {
	test *testing.T
}

func (current suite) check() {
	if compute() != 42 {
		current.test.Error("wrong")
	}
}

func TestStoredInAStruct(test *testing.T) {
	suite{test: test}.check()
}

// embedded promotes testing.T's methods, so its Error is testing's own.
type embedded struct {
	*testing.T
}

func TestEmbeddedError(test *testing.T) {
	wrapped := embedded{test}
	if compute() != 42 {
		wrapped.Error("wrong")
	}
}

// A method value of a failing method is a way to call it.
func TestMethodValue(test *testing.T) {
	fail := test.Errorf
	if got := compute(); got != 42 {
		fail("got %d", got)
	}
}

// A method expression of a failing method is a way to call it too.
func TestMethodExpression(test *testing.T) {
	if compute() != 42 {
		(*testing.T).Error(test, "wrong")
	}
}

// A panic fails the test it runs in.
func TestPanics(test *testing.T) {
	test.Parallel()
	if compute() != 42 {
		panic("wrong")
	}
}

// A test that skips before anything runs never runs, so it is not judged.
func TestSkippedFirst(test *testing.T) {
	test.Skip("not yet")
	_ = compute()
}

func TestSkippedAfterParallel(test *testing.T) {
	test.Parallel()
	test.Skipf("not yet: %d", compute())
}

func TestSkipNow(test *testing.T) {
	test.SkipNow()
}

// Parentheses change nothing about the call.
func TestParenthesized(test *testing.T) {
	if compute() != 42 {
		(test).Error("wrong")
	}
}
