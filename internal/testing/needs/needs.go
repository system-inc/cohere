// Package needs skips a test, by name, when the machine it runs on lacks what the test checks: a tool on the path,
// or the platform the code under test exists for. A skip is never a pass: go test reports it, and the land gate
// reruns every test Home skipped on the Mac (#acdf5mc), so a test Linux can't run is still checked before a
// landing. A failure here instead would refuse every landing on a box that simply lacks the tool.
package needs

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// Tool returns the path of a tool, or skips the test, naming the tool and why the test needs it.
func Tool(test testing.TB, tool string, why string) string {
	test.Helper()
	found, lookPathError := exec.LookPath(tool)
	if lookPathError != nil {
		test.Skipf("%s is not on the path, and %s: %v", tool, why, lookPathError)
	}
	return found
}

// OptInPhrase opens the skip of a test no gate runs, a measurement or a census over a tree of the asker's choosing,
// which runs only when its variable is set. The land gate reads it in a box's skips: a test that is opt-in is no
// gate's, so it is named as opt-in on the land line and never run again on the Mac, where it would only skip again
// (#xc29pnd).
const OptInPhrase = "opt-in, run with "

// OptIn returns the value of variable, or skips the test when it is unset, naming it opt-in by OptInPhrase and saying
// what the run is for. It is for tests no gate runs; a test a gate runs, whose input is merely missing, is Tool's.
func OptIn(test testing.TB, variable string, why string) string {
	test.Helper()
	value := os.Getenv(variable)
	if value == "" {
		test.Skipf("%s%s set: %s", OptInPhrase, variable, why)
	}
	return value
}

// NoRace skips the test in a binary built with -race, saying why the race detector's cost breaks what it checks:
// a test that holds CPU or wall time to a bound, say, since the detector slows instrumented code several times
// over. The land gate runs every package plainly first, so such a test is still checked on every landing.
func NoRace(test testing.TB, why string) {
	test.Helper()
	if raceDetector {
		test.Skipf("this test does not run under -race, since %s", why)
	}
}

// SwiftUnlaidVariable was set to SwiftUnlaidSetting by `cohere-dev integrate-job` for the tests it ran on Shackler,
// while cohere-swift couldn't be built there: its build fetched packages from github.com, wrote Swift's caches and
// started SwiftPM's own sandbox, none of which an integrate job's sandbox allows (#5wj23dd). Machines laid Swift there
// and the judge builds from the laid set now (#je1r32a), so it sets the variable no more. The skip stays one pull
// request longer, because the judge that runs is main's: the pull request that stopped setting it was judged by the
// judge before it, which still set it, and its tests skipped by it there. The one after it deletes the skip.
// SwiftUnlaid is what the skip says.
const (
	SwiftUnlaidVariable = "COHERE_DEV_LAND_JOB_SWIFT"
	SwiftUnlaidSetting  = "unlaid"
	SwiftUnlaid         = "Swift checked nowhere: cohere-swift isn't laid on Shackler (#je1r32a)"
)

// SwiftLaid skips a test that builds cohere-swift in a run where it can't be built, SwiftUnlaidVariable set to
// SwiftUnlaidSetting, saying SwiftUnlaid. Anywhere else the test runs.
func SwiftLaid(test testing.TB) {
	test.Helper()
	if os.Getenv(SwiftUnlaidVariable) == SwiftUnlaidSetting {
		test.Skip(SwiftUnlaid)
	}
}

// Platform skips the test unless it runs on the operating system named, as Go names it, saying why the test needs it.
func Platform(test testing.TB, operatingSystem string, why string) {
	test.Helper()
	if runtime.GOOS != operatingSystem {
		test.Skipf("this test runs on %s only, since %s; this is %s/%s", operatingSystem, why, runtime.GOOS,
			runtime.GOARCH)
	}
}
