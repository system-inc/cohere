package needs

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// needsChildVariable makes this test binary the child that asks for a missing tool or another platform.
const needsChildVariable = "COHERE_NEEDS_CHILD"

// A test missing its tool or its platform is reported skipped, naming what it lacks and why, never failed and
// never passed. Shown through a child test binary run with an empty path, since a skip ends the test it is in.
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
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	for _, row := range []struct {
		child string
		want  []string
	}{
		{"tool", []string{"--- SKIP", "zsh is not on the path, and the benchmark is a zsh script"}},
		{"platform", []string{"--- SKIP", "this test runs on", "since the engine ships there alone", runtime.GOOS}},
	} {
		command := exec.Command(executable, "-test.run=^TestAMissingToolOrPlatformIsANamedSkip$", "-test.v")
		command.Env = append(os.Environ(), needsChildVariable+"="+row.child, "PATH=")
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
