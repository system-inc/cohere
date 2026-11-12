//go:build unix

// Every run here goes through stand-in shell scripts for swift, xcrun and the engine, so the file builds on
// Unix, as projects_test.go does (#tejf9bc).

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

const fakeSwiftVersion = "Apple Swift version 9.9 (swiftlang-9.9.9.9.9 clang-1)"

// fakeToolchain is a directory holding a `swift` and an `xcrun` that log every command line to the file it
// returns. `swift --version` answers when answering is true and fails otherwise, and `xcrun --find swift`
// names the stand-in `swift`.
func fakeToolchain(test *testing.T, answering bool) (string, string) {
	test.Helper()
	directory := test.TempDir()
	log := filepath.Join(test.TempDir(), "toolchain")
	version := "exit 1"
	if answering {
		version = "echo '" + fakeSwiftVersion + "'; echo 'Target: arm64-apple-macosx'; exit 0"
	}
	scripts := map[string]string{
		"swift": "echo \"swift $*\" >> '" + log + "'\nif [ \"$1\" = --version ]; then " + version + "; fi\nexit 1",
		"xcrun": "echo \"xcrun $*\" >> '" + log + "'\necho '" + filepath.Join(directory, "swift") + "'",
	}
	for name, body := range scripts {
		script := filepath.Join(directory, name)
		if writeError := executable_testing.WriteFile(
			script,
			[]byte("#!/bin/sh\n"+body+"\n"),
			0o755,
		); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return directory, log
}

// runWithToolchain runs the binary with the stand-in toolchain first on PATH and the extra variables given,
// which win over the same names in this process's environment.
func runWithToolchain(
	test *testing.T,
	binary string,
	directory string,
	toolchain string,
	extra []string,
	arguments ...string,
) (string, int) {
	test.Helper()
	command := exec.Command(binary, verboseArguments(arguments)...)
	command.Dir = directory
	searchPath := "PATH=" + toolchain + string(os.PathListSeparator) + os.Getenv("PATH")
	command.Env = append(os.Environ(), searchPath, "COHERE_VERDICT_FD=")
	command.Env = append(command.Env, extra...)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	runError := command.Run()
	if runError == nil {
		return output.String(), 0
	}
	exited, isExit := runError.(*exec.ExitError)
	if !isExit {
		test.Fatalf("running cohere: %v\n%s", runError, output.String())
	}
	return output.String(), childExitCode(test, exited)
}

func readLogFile(test *testing.T, log string) string {
	test.Helper()
	contents, readError := os.ReadFile(log)
	if readError != nil && !os.IsNotExist(readError) {
		test.Fatal(readError)
	}
	return string(contents)
}

// A repository of several Swift packages asks `swift --version` once, in the run that starts the others, and
// every engine in the tree, coverage queries and project runs alike, is handed that answer and the `swift`
// xcrun named, rather than asking again (#nkk91tv). A value the caller's shell carries is never one of them:
// only a process whose own parent published it may trust it, so a stale toolchain cannot reach a cache key.
func TestAProcessTreeAsksTheSwiftToolchainOnce(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := coveringRepository(test)
	covering, _ := coveringSwiftEngine(
		test,
		coverageAnswerThrough(root, root, filepath.Join(contractDirectory(test), "Clean.jsonl")),
	)
	seen := filepath.Join(test.TempDir(), "seen")
	engine := fakeSwiftEngine(test,
		"printf '%s|%s\\n' \"$(printf '%s' \"$COHERE_SWIFT_VERSION_OUTPUT\" | tr '\\n' ' ')\" "+
			"\"$COHERE_SWIFT_EXECUTABLE\" >> '"+seen+"'\nexec '"+covering+"' \"$@\"")
	toolchain, log := fakeToolchain(test, true)

	output, _ := runWithToolchain(test, binary, root, toolchain, []string{
		"COHERE_SWIFT_ENGINE=" + engine,
		"COHERE_SWIFT_VERSION_OUTPUT=stale swiftlang-1.0.0",
		"COHERE_SWIFT_EXECUTABLE=/stale/swift",
		"COHERE_SWIFT_RESOLVED_BY=1",
	}, "--no-fix")
	if !strings.Contains(output, "projects: 2 checked (2 Swift)") {
		test.Fatalf("the repository did not run as two Swift projects:\n%s", output)
	}
	if asked := strings.Count(readLogFile(test, log), "swift --version"); asked != 1 {
		test.Errorf("swift --version was asked %d times in one run, want once:\n%s", asked, readLogFile(test, log))
	}
	lines := strings.Split(strings.TrimSpace(readLogFile(test, seen)), "\n")
	want := fakeSwiftVersion + " Target: arm64-apple-macosx |" + filepath.Join(toolchain, "swift")
	if len(lines) < 4 {
		test.Fatalf("the engine ran %d times, want the 4 coverage queries and the 2 checks:\n%s", len(lines), output)
	}
	for _, line := range lines {
		if strings.TrimSpace(line) != strings.TrimSpace(want) {
			test.Errorf("an engine was handed %q, want the tree's one answer %q", line, want)
		}
	}
}

// A run trusts an engine and a toolchain it inherits only from its own parent: given them by the process that
// started it, it runs that engine and asks nothing; given the same values by anyone else, it drops them and
// resolves its own, here through a `swift` that does not answer, so the engine it was handed never runs.
func TestARunTrustsAnInheritedSwiftEngineOnlyFromItsParent(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		"Package.swift":         discoveryPackage,
		"Sources/Toy/Toy.swift": "let toy = 1\n",
	})
	for _, trust := range []struct {
		name       string
		resolvedBy string
		trusted    bool
	}{
		{name: "from its parent", resolvedBy: strconv.Itoa(os.Getpid()), trusted: true},
		{name: "from anyone else", resolvedBy: "1"},
	} {
		test.Run(trust.name, func(test *testing.T) {
			test.Parallel()
			engine, log := loggingSwiftEngine(test)
			toolchain, asked := fakeToolchain(test, false)
			output, exitCode := runWithToolchain(test, binary, root, toolchain, []string{
				"COHERE_SWIFT_ENGINE_PATH=" + engine,
				"COHERE_SWIFT_VERSION_OUTPUT=" + fakeSwiftVersion,
				"COHERE_SWIFT_RESOLVED_BY=" + trust.resolvedBy,
			}, "--no-fix")
			ran := readLogFile(test, log) != ""
			if trust.trusted {
				if exitCode != 0 || !ran || readLogFile(test, asked) != "" {
					test.Errorf("a run did not take the engine and toolchain its parent resolved "+
						"(exit %d, engine ran %t, asked %q):\n%s", exitCode, ran, readLogFile(test, asked), output)
				}
				return
			}
			if exitCode == 0 || ran {
				test.Errorf("a run took an engine its parent did not resolve (exit %d, engine ran %t):\n%s",
					exitCode, ran, output)
			}
		})
	}
}
