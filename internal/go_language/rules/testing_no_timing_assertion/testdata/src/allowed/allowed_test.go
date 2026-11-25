package allowed

import (
	"testing"
	"time"
)

const limit = time.Second

func work() {}

func reportSlow(test *testing.T, elapsed time.Duration) {
	test.Helper()
	test.Fatalf("took %v", elapsed)
}

func TestMarkedWithReason(test *testing.T) {
	start := time.Now()
	work()
	// Timing assertion: the limit is a hundred times the work's slowest run on a loaded pool.
	if time.Since(start) > limit {
		test.Fatal("too slow")
	}
}

func TestMarkerOnALaterLineOfTheGroup(test *testing.T) {
	start := time.Now()
	work()
	// The limit bounds a hang, not the speed of the work.
	// Timing assertion: a hang never returns, and any real run is a thousand times faster.
	if time.Since(start) > limit {
		test.Fatal("hung")
	}
}

func TestBlockMarkerWithReason(test *testing.T) {
	start := time.Now()
	work()
	/* Timing assertion: the work is a constant and the limit leaves a wide margin. */
	if time.Since(start) > limit {
		test.Fatal("too slow")
	}
}

func TestMarkerExcusesEveryComparisonOfItsIf(test *testing.T) {
	start := time.Now()
	work()
	end := time.Now()
	// Timing assertion: both measure a hang, and the limit leaves a wide margin.
	if time.Since(start) > limit || end.Sub(start) > limit {
		test.Fatal("hung")
	}
}

// A check that the work waited long enough fails only a run that returned early, which load cannot cause.
func TestLowerBound(test *testing.T) {
	start := time.Now()
	time.Sleep(time.Millisecond)
	if time.Since(start) < time.Millisecond {
		test.Fatal("returned before the sleep ended")
	}
}

func TestLowerBoundWithTheDurationOnTheRight(test *testing.T) {
	start := time.Now()
	time.Sleep(time.Millisecond)
	if time.Millisecond > time.Since(start) {
		test.Fatal("returned before the sleep ended")
	}
}

func TestNegatedLowerBound(test *testing.T) {
	start := time.Now()
	time.Sleep(time.Millisecond)
	if !(time.Since(start) >= time.Millisecond) {
		test.Fatal("returned before the sleep ended")
	}
}

// An == fails only the one exact duration, which a slower run moves away from.
func TestEquality(test *testing.T) {
	start := time.Now()
	if time.Since(start) == 0 {
		test.Fatal("the clock did not move")
	}
}

// A body that only logs or skips asserts nothing, so a busy machine cannot fail the test.
func TestBodyOnlyLogs(test *testing.T) {
	start := time.Now()
	work()
	if time.Since(start) > limit {
		test.Logf("slow run: %v", time.Since(start))
	}
}

func TestBodySkips(test *testing.T) {
	start := time.Now()
	work()
	if time.Since(start) > limit {
		test.Skip("this machine is too slow to judge")
	}
}

// A helper handed the test may fail it, but it is not the body failing the test directly.
func TestBodyCallsAHelper(test *testing.T) {
	start := time.Now()
	work()
	if elapsed := time.Since(start); elapsed > limit {
		reportSlow(test, elapsed)
	}
}

// A function literal in the body runs whenever it is called, so it is not the body failing the test.
func TestFailureInAFunctionLiteral(test *testing.T) {
	start := time.Now()
	work()
	if time.Since(start) > limit {
		test.Cleanup(func() {
			test.Error("slow run")
		})
	}
}

// Asserting on what the code did is the cure.
func TestAssertsOnTheResult(test *testing.T) {
	done := make(chan struct{})
	go func() {
		work()
		close(done)
	}()
	<-done
}
