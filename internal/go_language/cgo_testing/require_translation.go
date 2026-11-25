// Package cgo_testing is for the Go engine's tests that load a project with cgo of its own, whose cgo go list
// -compiled translates, which needs a go that compiles and a toolchain with cgo. Where either is missing such a test
// skips, with the reason, and never fails: a toolchain without cgo, or a runner of prebuilt tests, as Loom's is, whose
// stand-in go refuses every command that compiles. Any other failure of go fails the test, so a broken toolchain
// never reads as a skip.
package cgo_testing

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// probe is what one test binary learned, once, of whether go here translates cgo: a reason to skip, empty when it
// does, or the failure that makes every such test fail.
var probe struct {
	once    sync.Once
	reason  string
	failure error
}

// RequireTranslation skips test, with the reason, where go here cannot translate cgo, and fails it where asking
// went wrong any other way.
func RequireTranslation(test *testing.T) {
	test.Helper()
	probe.once.Do(func() { probe.reason, probe.failure = translationUnavailable() })
	if probe.failure != nil {
		test.Fatal(probe.failure)
	}
	if probe.reason != "" {
		test.Skip(probe.reason)
	}
}

// translationUnavailable is why go here cannot translate cgo, or empty when it can. go reports `CGO_ENABLED` 0
// when it finds no C compiler. A runner's stand-in go answers a read-only query and refuses one that compiles,
// saying so on standard error, and Loom fails a unit over a refused command only when a test failed too, so asking
// for one compiled package is a safe probe: go list -compiled is what the load runs that such a runner refuses.
func translationUnavailable() (string, error) {
	enabled, goEnvironmentError := exec.Command("go", "env", "CGO_ENABLED").Output()
	if goEnvironmentError != nil {
		return "", fmt.Errorf("reading CGO_ENABLED with go env: %w", goEnvironmentError)
	}
	if value := strings.TrimSpace(string(enabled)); value != "1" {
		return fmt.Sprintf("the toolchain here has no cgo (go env CGO_ENABLED is %q), and the cgo tool needs it",
			value), nil
	}
	command := exec.Command("go", "list", "-compiled", "-e", "-json", "unsafe")
	command.Stdout = io.Discard
	var standardError bytes.Buffer
	command.Stderr = &standardError
	if runError := command.Run(); runError != nil {
		said := strings.TrimSpace(standardError.String())
		if strings.Contains(said, "loom-runner:") && strings.Contains(said, "never builds") {
			return "go here is a runner's stand-in that refuses to compile, and the cgo tool needs a go that " +
				"does: " + said, nil
		}
		return "", fmt.Errorf("probing whether go here compiles, with go list -compiled -e -json unsafe: %w: %s",
			runError, said)
	}
	return "", nil
}
