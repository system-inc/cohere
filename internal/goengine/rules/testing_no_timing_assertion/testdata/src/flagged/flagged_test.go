package flagged

import (
	"testing"
	"time"
)

const limit = time.Second

func work() {}

func TestSinceDirectly(test *testing.T) {
	start := time.Now()
	work()
	if time.Since(start) > limit { // want `^This test fails when its work takes longer than a deadline, `
		test.Fatalf("took %v", time.Since(start))
	}
}

func TestThroughLocal(test *testing.T) {
	start := time.Now()
	work()
	elapsed := time.Since(start)
	if elapsed >= 100*time.Millisecond { // want `This test fails when its work takes longer`
		test.Errorf("took %v", elapsed)
	}
}

func TestSub(test *testing.T) {
	start := time.Now()
	work()
	end := time.Now()
	if end.Sub(start) > limit { // want `This test fails when its work takes longer`
		test.Fail()
	}
}

func TestSubThroughVar(test *testing.T) {
	start := time.Now()
	work()
	var elapsed = time.Now().Sub(start)
	if limit < elapsed { // want `This test fails when its work takes longer`
		test.FailNow()
	}
}

func TestDeclaredInTheCondition(test *testing.T) {
	start := time.Now()
	work()
	if elapsed := time.Since(start); elapsed > limit { // want `This test fails when its work takes longer`
		test.Error("too slow")
	}
}

func TestNegatedUpperBound(test *testing.T) {
	start := time.Now()
	work()
	if !(time.Since(start) <= limit) { // want `This test fails when its work takes longer`
		test.Fatal("too slow")
	}
}

func TestNotTheExpectedDuration(test *testing.T) {
	start := time.Now()
	work()
	if time.Since(start) != limit { // want `This test fails when its work takes longer`
		test.Fatal("not a second")
	}
}

func TestNegatedEquality(test *testing.T) {
	start := time.Now()
	work()
	if !(time.Since(start) == limit) { // want `This test fails when its work takes longer`
		test.Fatal("not a second")
	}
}

func TestBothSidesMeasured(test *testing.T) {
	start := time.Now()
	work()
	first := time.Since(start)
	start = time.Now()
	work()
	second := time.Since(start)
	if second > first { // want `This test fails when its work takes longer`
		test.Fatal("the second run was slower")
	}
}

func TestInsideAConjunction(test *testing.T) {
	start := time.Now()
	work()
	if !testing.Short() && time.Since(start) > limit { // want `This test fails when its work takes longer`
		test.Fatal("too slow")
	}
}

func TestEachComparisonOfADisjunction(test *testing.T) {
	start := time.Now()
	work()
	end := time.Now()
	if time.Since(start) > limit || end.Sub(start) > limit { // want `takes longer` `takes longer`
		test.Fatal("too slow")
	}
}

// A failure in a block nested in the body still fails the test because the work ran long.
func TestFailureInANestedBlock(test *testing.T) {
	start := time.Now()
	work()
	if time.Since(start) > limit { // want `This test fails when its work takes longer`
		if !testing.Short() {
			test.Fatal("too slow")
		}
	}
}

func TestInASubtest(test *testing.T) {
	test.Run("timed", func(subtest *testing.T) {
		start := time.Now()
		work()
		if time.Since(start) > limit { // want `This test fails when its work takes longer`
			subtest.Fatal("too slow")
		}
	})
}

func TestLocalOfTheParentReadInASubtest(test *testing.T) {
	start := time.Now()
	work()
	elapsed := time.Since(start)
	test.Run("check", func(subtest *testing.T) {
		if elapsed > limit { // want `This test fails when its work takes longer`
			subtest.Fatal("too slow")
		}
	})
}

func Test_underscored(test *testing.T) {
	start := time.Now()
	if time.Since(start) > limit { // want `This test fails when its work takes longer`
		test.Fatal("too slow")
	}
}

func TestMarkerWithoutReason(test *testing.T) {
	start := time.Now()
	work()
	// Timing assertion:
	if time.Since(start) > limit { // want `^This timing assertion is marked and gives no reason\.`
		test.Fatal("too slow")
	}
}

func TestBlockMarkerWithoutReason(test *testing.T) {
	start := time.Now()
	work()
	/* Timing assertion: */
	if time.Since(start) > limit { // want `This timing assertion is marked and gives no reason`
		test.Fatal("too slow")
	}
}

// A sentence that only mentions the marker excuses nothing.
func TestMarkerMentionedMidSentence(test *testing.T) {
	start := time.Now()
	work()
	// This was a Timing assertion: once, before the pool.
	if time.Since(start) > limit { // want `This test fails when its work takes longer`
		test.Fatal("too slow")
	}
}

// A comment trailing the line above speaks for that line, not for the if below it.
func TestMarkerTrailingTheLineAbove(test *testing.T) {
	start := time.Now()
	work()                         // Timing assertion: the pool runs this test alone.
	if time.Since(start) > limit { // want `This test fails when its work takes longer`
		test.Fatal("too slow")
	}
}

// A comment that code follows on its line speaks for that code.
func TestMarkerLeadingCodeOnTheLineAbove(test *testing.T) {
	start := time.Now()
	/* Timing assertion: the pool runs this test alone. */ work()
	if time.Since(start) > limit { // want `This test fails when its work takes longer`
		test.Fatal("too slow")
	}
}

// A marker above the if's enclosing statement is not above the if.
func TestMarkerAboveTheEnclosingBlock(test *testing.T) {
	start := time.Now()
	work()
	// Timing assertion: the pool runs this test alone.
	for range 1 {
		if time.Since(start) > limit { // want `This test fails when its work takes longer`
			test.Fatal("too slow")
		}
	}
}
