package needs

import (
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// needsChildVariable makes this test binary the child that asks for a missing tool or another platform.
const needsChildVariable = "COHERE_NEEDS_CHILD"

// A test missing its tool or its platform, or one that can't run under -race in a binary built with it, is
// reported skipped, naming what it lacks and why, never failed and never passed. Shown through a child test binary
// run with an empty path, since a skip ends the test it is in.
func TestAMissingToolOrPlatformIsANamedSkip(test *testing.T) {
	test.Parallel()
	switch os.Getenv(needsChildVariable) {
	case "tool":
		Tool(test, "zsh", "the benchmark is a zsh script")
		test.Fatal("Tool returned with zsh off the path")
	case "platform":
		other := "plan9"
		if runtime.GOOS == other {
			other = "darwin"
		}
		Platform(test, other, "the engine ships there alone")
		test.Fatal("Platform returned on another platform")
	case "opt-in":
		value := OptIn(test, "COHERE_NEEDS_OPT_IN", "it measures a tree of the asker's choosing")
		test.Logf("opted in with %s", value)
		return
	case "race":
		NoRace(test, "it bounds CPU time")
		if raceDetector {
			test.Fatal("NoRace returned under -race")
		}
		return
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	// A plain binary runs the race child to its end; one built with -race skips it, naming why. Whether this one
	// was is read from its build settings, not from raceDetector, so a wrong constant can't agree with itself.
	built, readable := debug.ReadBuildInfo()
	if !readable {
		test.Fatal("this test binary carries no build information")
	}
	raceWant := []string{"--- PASS"}
	if slices.Contains(built.Settings, debug.BuildSetting{Key: "-race", Value: "true"}) {
		raceWant = []string{"--- SKIP", "this test does not run under -race, since it bounds CPU time"}
	}
	for _, row := range []struct {
		child string
		want  []string
		// environment is added to the child's, after the opt-in variable is cleared.
		environment []string
	}{
		{"tool", []string{"--- SKIP", "zsh is not on the path, and the benchmark is a zsh script"}, nil},
		{
			"platform",
			[]string{"--- SKIP", "this test runs on", "since the engine ships there alone", runtime.GOOS},
			nil,
		},
		{"race", raceWant, nil},
		{
			"opt-in",
			[]string{"--- SKIP", OptInPhrase + "COHERE_NEEDS_OPT_IN set: it measures a tree of the asker's choosing"},
			nil,
		},
		{"opt-in", []string{"--- PASS", "opted in with a tree"}, []string{"COHERE_NEEDS_OPT_IN=a tree"}},
	} {
		command := exec.Command(executable, "-test.run=^TestAMissingToolOrPlatformIsANamedSkip$", "-test.v")
		command.Env = append(append(os.Environ(), needsChildVariable+"="+row.child, "PATH=", "COHERE_NEEDS_OPT_IN="),
			row.environment...)
		output, runError := command.CombinedOutput()
		if runError != nil {
			test.Errorf("the %s child failed rather than skipped: %v\n%s", row.child, runError, output)
			continue
		}
		for _, want := range row.want {
			if !strings.Contains(string(output), want) {
				test.Errorf("the %s child's report lacks %q:\n%s", row.child, want, output)
			}
		}
	}
}
