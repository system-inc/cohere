// Package differences holds the TestMains where the port and the original could plausibly disagree, and records no
// difference, since there is none: both keep the same state over the same walk of the package. Whether os.Exit was
// called carries from one TestMain's end to the next, through calls at package level, and calls inside a call's
// arguments go unseen once the call is judged.
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

// exitAtPackageLevel calls os.Exit at package level, outside any TestMain, and the original counts it for the next
// TestMain.
var exitAtPackageLevel = func() int {
	if len(os.Args) > 100 {
		os.Exit(2)
	}
	return 0
}()

type second struct{}

// Found by neither: the call to os.Exit above, at package level, counts for this TestMain, the original's false
// negative.
func (second) TestMain(m *testing.M) {
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
