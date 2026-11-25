package lookalike

import (
	"context"
	"testing"
	"time"
)

const limit = time.Second

// processStart is measured from at package level, so a duration held there is no local of a test.
var processStart = time.Now()

var sinceProcessStart = time.Since(processStart)

func work() {}

// Since has time.Since's shape and is this package's own, so what it returns was not measured by the clock.
func Since(start time.Time) time.Duration { return limit }

// stopwatch has a Sub method of time.Time's shape on another type.
type stopwatch struct{}

func (stopwatch) Sub(start time.Time) time.Duration { return limit }

// recorder has testing.T's failure methods on another type, which fail no test.
type recorder struct{}

func (recorder) Fatal(arguments ...any) {}

func (recorder) Errorf(format string, arguments ...any) {}

func TestOwnSince(test *testing.T) {
	if Since(time.Now()) > limit {
		test.Fatal("not the clock")
	}
}

func TestOwnSub(test *testing.T) {
	if (stopwatch{}).Sub(time.Now()) > limit {
		test.Fatal("not the clock")
	}
}

func TestFailureOnAnotherType(test *testing.T) {
	start := time.Now()
	work()
	if time.Since(start) > limit {
		recorder{}.Fatal("not a test")
		recorder{}.Errorf("not a test")
	}
}

// A Sub between two fixed times is date math, whose result a busy machine cannot change.
func TestDateMath(test *testing.T) {
	issued := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
	expiry := issued.Add(24 * time.Hour)
	if expiry.Sub(issued) != 24*time.Hour {
		test.Fatal("a token lives a day")
	}
}

func TestDateMathThroughALocal(test *testing.T) {
	issued := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
	expiry := issued.AddDate(0, 0, 1)
	lifetime := expiry.Sub(issued)
	if lifetime > 24*time.Hour {
		test.Fatal("a token lives a day at most")
	}
}

// A duration that was not measured, a configured timeout, is not the clock's verdict.
func TestConfiguredDuration(test *testing.T) {
	timeout := 2 * time.Second
	if timeout > limit {
		test.Fatal("the timeout is longer than the limit")
	}
}

// A local written again may hold something other than the measurement by the time it is compared.
func TestLocalWrittenAgain(test *testing.T) {
	start := time.Now()
	elapsed := time.Since(start)
	elapsed = elapsed.Truncate(time.Hour)
	if elapsed > limit {
		test.Fatal("over an hour")
	}
}

func TestLocalSteppedAgain(test *testing.T) {
	start := time.Now()
	elapsed := time.Since(start)
	elapsed--
	if elapsed > limit {
		test.Fatal("too slow")
	}
}

func TestPackageLevelMeasurement(test *testing.T) {
	if sinceProcessStart > time.Hour {
		test.Fatal("the process is old")
	}
}

// A measurement converted out of time.Duration is not a duration comparison.
func TestConvertedToSeconds(test *testing.T) {
	start := time.Now()
	work()
	if time.Since(start).Seconds() > 1 {
		test.Fatal("too slow")
	}
}

// A comparison inside a call's argument decides something out of sight.
func TestComparisonPassedToACall(test *testing.T) {
	start := time.Now()
	work()
	if mustHold(time.Since(start) > limit) {
		test.Fatal("held")
	}
}

func mustHold(condition bool) bool { return condition }

// A context deadline is not judged.
func TestContextDeadline(test *testing.T) {
	requestContext, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	work()
	if requestContext.Err() != nil {
		test.Fatal("the deadline passed")
	}
}

// A helper is not a test; only go test's own tests are judged.
func checkFast(test *testing.T, start time.Time) {
	if time.Since(start) > limit {
		test.Fatal("too slow")
	}
}

func TestUsesAHelper(test *testing.T) {
	checkFast(test, time.Now())
}

// Testify is a helper's name, since a lower-case letter follows Test.
func Testify(test *testing.T) {
	start := time.Now()
	if time.Since(start) > limit {
		test.Fatal("too slow")
	}
}

// A benchmark measures time by design, and its failure is on a *testing.B.
func BenchmarkWork(benchmark *testing.B) {
	start := time.Now()
	work()
	if time.Since(start) > limit {
		benchmark.Fatal("too slow")
	}
}
