//go:build unix

package dispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A signal to the process building the Swift engine ends the build tool and every compiler it started,
// and the build reports that it was stopped. Before, the signal ended this process alone, and `swift build`
// went on compiling with nobody to read its result (#nqb3mjv).
func TestASignalDuringABuildEndsTheToolAndWhatItStarted(test *testing.T) {
	test.Parallel()
	compilerPID := filepath.Join(test.TempDir(), "compiler.pid")
	// The path is the script's $1, so the shell never parses it; the script's own syntax, a job started in the
	// background and waited on, is the tool being stood in for.
	tool := exec.Command(
		"/bin/sh",
		"-c",
		`sleep 300 & echo $! > "$1.partial" && mv "$1.partial" "$1"; wait`,
		"sh",
		compilerPID,
	)

	signals := make(chan os.Signal, 1)
	result := make(chan error, 1)
	go func() { result <- runBuildToolUntil(tool, signals) }()

	compiler := 0
	for deadline := time.Now().Add(time.Minute); compiler == 0 && time.Now().Before(deadline); {
		if contents, readError := os.ReadFile(compilerPID); readError == nil {
			// The file is written whole under another name and moved into place, so what is read is all of it.
			parsed, parseError := strconv.Atoi(strings.TrimSpace(string(contents)))
			if parseError != nil {
				test.Fatalf("reading the compiler's process id from %q: %v", contents, parseError)
			}
			compiler = parsed
		}
		time.Sleep(20 * time.Millisecond)
	}
	if compiler == 0 {
		test.Fatal("the tool never started its compiler")
	}

	signals <- syscall.SIGTERM
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "stopped by terminated") {
			test.Errorf("a stopped build reported %v", err)
		}
	case <-time.After(buildToolGrace + 30*time.Second):
		test.Fatal("the build did not end after the signal")
	}

	deadline := time.Now().Add(10 * time.Second)
	for syscall.Kill(compiler, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(compiler, 0) == nil {
		if killError := syscall.Kill(compiler, syscall.SIGKILL); killError != nil {
			test.Errorf("ending the compiler %d after the test: %v", compiler, killError)
		}
		test.Error("the compiler the tool started outlived the signal")
	}
}

// A build that ends on its own reports its own result, untouched by the grouping.
func TestABuildToolThatEndsReportsItsOwnExit(test *testing.T) {
	test.Parallel()
	if runBuildToolUntilError := runBuildToolUntil(
		exec.Command("/bin/sh", "-c", "exit 0"),
		make(chan os.Signal),
	); runBuildToolUntilError != nil {
		test.Errorf("a tool that succeeded reported %v", runBuildToolUntilError)
	}
	if runBuildToolUntilError := runBuildToolUntil(
		exec.Command("/bin/sh", "-c", "exit 3"),
		make(chan os.Signal),
	); runBuildToolUntilError == nil {
		test.Error("a tool that failed reported success")
	}
}
