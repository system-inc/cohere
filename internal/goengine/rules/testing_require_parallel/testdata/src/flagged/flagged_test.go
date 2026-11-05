package flagged

import "testing"

var other *testing.T

func TestSerial(test *testing.T) { // want `^This test runs alone while its siblings wait, because its first statement is not Parallel on its own \*testing\.T\.`
	test.Log("serial")
}

func TestEmptyBody(test *testing.T) {} // want `This test runs alone`

func TestParallelSecond(test *testing.T) { // want `This test runs alone`
	test.Helper()
	test.Parallel()
}

func TestParallelDeferred(test *testing.T) { // want `This test runs alone`
	defer test.Parallel()
}

func TestParallelOnAnotherT(test *testing.T) { // want `This test runs alone`
	other.Parallel()
}

// A blank or unnamed parameter cannot be called, so the test is serial.
func TestBlankParameter(_ *testing.T) {} // want `This test runs alone`

func TestUnnamedParameter(*testing.T) {} // want `This test runs alone`

func Test_underscored(test *testing.T) {} // want `This test runs alone`

func TestSerialSubtestInAParallelTest(test *testing.T) {
	test.Parallel()
	test.Run("serial", func(subtest *testing.T) { // want `This test runs alone`
		subtest.Log("serial")
	})
}

// The outer test's Parallel inside a subtest marks the outer test a second time, which panics, and leaves the
// subtest serial.
func TestOuterParallelInASubtest(test *testing.T) {
	test.Parallel()
	test.Run("outer", func(subtest *testing.T) { // want `This test runs alone`
		test.Parallel()
	})
}

func TestSubtestParallelSecond(test *testing.T) {
	test.Parallel()
	test.Run("second", func(subtest *testing.T) { // want `This test runs alone`
		subtest.Helper()
		subtest.Parallel()
	})
}

// A subtest that names a function declared here is judged by that function's body.
func TestNamedSerialSubtest(test *testing.T) {
	test.Parallel()
	test.Run("named", serialSubtest) // want `This test runs alone`
}

func serialSubtest(subtest *testing.T) {
	subtest.Log("serial")
}

// A method value names a body the rule can see as well.
func TestMethodValueSubtest(test *testing.T) {
	test.Parallel()
	test.Run("method", suite{}.serial) // want `This test runs alone`
}

type suite struct{}

func (suite) serial(subtest *testing.T) {}

// A reason a paragraph away from the subtest is not that subtest's.
func TestSubtestReasonSeparated(test *testing.T) {
	test.Parallel()
	// Not parallel: this is a paragraph away from the call below.

	test.Run("separated", func(subtest *testing.T) {}) // want `This test runs alone`
}

// Not parallel:
func TestEmptyReason(test *testing.T) {} // want `^This test says it is not parallel and gives no reason\.`

/* Not parallel: */
func TestEmptyBlockReason(test *testing.T) {} // want `This test says it is not parallel and gives no reason`

func TestSubtestEmptyReason(test *testing.T) {
	test.Parallel()
	// Not parallel:
	test.Run("empty", func(subtest *testing.T) {}) // want `This test says it is not parallel and gives no reason`
}

// TestReasonWithoutColon says Not parallel, but with no colon, so it gives no reason.
func TestReasonWithoutColon(test *testing.T) {} // want `This test runs alone`

// Not parallel: this reason is a paragraph away from the test below, so it is not that test's.

func TestReasonSeparated(test *testing.T) { // want `This test runs alone`
	test.Log("serial")
}

// TestMarkerMidSentence was once Not parallel: the marker sits mid-sentence here, so it is no excuse.
func TestMarkerMidSentence(test *testing.T) {} // want `This test runs alone`
