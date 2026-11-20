// Package differences holds the TestMains where the port and the original could plausibly disagree. Both clear what
// they saw as each TestMain ends, and leave calls inside a call's arguments unseen once the call is judged. They differ
// in one way: the original counts a call to os.Exit at package level for the TestMain after it, here and across
// pair_exit.go and pair_test_main.go, and the port counts only the calls inside a TestMain (#zvwfe26).
package differences

import (
	"os"
	"testing"
)

type first struct{}

// Found by both: the first TestMain, which nothing before it exits for.
func (first) TestMain(m *testing.M) {
	m.Run()
}

// exitAtPackageLevel calls os.Exit at package level, outside any TestMain, which the original counts for the next
// TestMain.
var exitAtPackageLevel = func() int {
	if len(os.Args) > 100 {
		os.Exit(2)
	}
	return 0
}()

type second struct{}

// Found by the port alone: the call to os.Exit above, at package level, is no exit of this TestMain's, though the
// original counts it for this one and misses it.
func (second) TestMain(m *testing.M) { // port finds: TestMain should call os.Exit to set exit code
	m.Run()
}

type third struct{}

// Found by both: the state was cleared when the second TestMain ended.
func (third) TestMain(m *testing.M) {
	m.Run()
}

type insideExit struct{}

// Found by neither: Run inside os.Exit's argument, unseen, and the exit seen.
func (insideExit) TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func wrap(code int) int { return code }

type insideWrap struct{}

// Found by both: Run inside another call's argument, which is walked.
func (insideWrap) TestMain(m *testing.M) {
	_ = wrap(m.Run())
}
