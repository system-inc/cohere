// Package needs skips a test, by name, when the machine it runs on lacks what the test checks: a tool on the path,
// or the platform the code under test exists for. A skip is never a pass: go test reports it, and the land gate
// reruns every test Home skipped on the Mac (#acdf5mc), so a test Linux can't run is still checked before a
// landing. A failure here instead would refuse every landing on a box that simply lacks the tool.
package needs

import (
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

// NoRace skips the test in a binary built with -race, saying why the race detector's cost breaks what it checks:
// a test that holds CPU or wall time to a bound, say, since the detector slows instrumented code several times
// over. The land gate runs every package plainly first, so such a test is still checked on every landing.
func NoRace(test testing.TB, why string) {
	test.Helper()
	if raceDetector {
		test.Skipf("this test does not run under -race, since %s", why)
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
