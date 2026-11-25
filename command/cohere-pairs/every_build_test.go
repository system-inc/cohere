//go:build unix

// Each stand-in Go child is a /bin/sh script, so the file builds on Unix.

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// standInGoChild writes a cohere at a directory of its own whose Go child answers the probe with answer, a shell
// fragment, only when asked the probe's exact command line at root, and otherwise writes the arguments it was given to
// asked, beside it, and exits 0, as a run of the Go child does.
func standInGoChild(test *testing.T, root string, answer string) string {
	test.Helper()
	directory := test.TempDir()
	binary := filepath.Join(directory, "cohere")
	script := "#!/bin/sh\n" +
		"if [ \"$*\" = \"go-language --contract 4 --root " + root + " --version --host-build-only\" ]; then\n" +
		answer + "\n" +
		"fi\n" +
		"printf '%s\\n' \"$@\" > " + filepath.Join(directory, "asked") + "\n"
	if writeError := executable_testing.WriteFile(binary, []byte(script), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	return binary
}

// Each side's Go child is asked once whether it takes `--host-build-only` (#qqyq61s): one that answers is from after
// every build became the default and is given nothing, one that refuses the switch by name, as a cohere from before
// the flip does, is given `--other-builds`, and one that does anything else refuses the pair, naming the binary.
func TestEachSideIsAskedForEveryBuildInItsOwnWords(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	for _, side := range []struct {
		name   string
		answer string
		want   []string
	}{
		{"a cohere after the flip", "echo '{\"kind\":\"provenance\"}'; exit 0", nil},
		{"a cohere before the flip", "echo '" + hostBuildOnlyUnknown + ".' >&2; exit 2", []string{"--other-builds"}},
	} {
		binary := standInGoChild(test, root, side.answer)
		given, probeError := everyBuildArguments(binary, root, os.Environ())
		if probeError != nil || !slices.Equal(given, side.want) {
			test.Errorf("%s is given %q (%v), want %q", side.name, given, probeError, side.want)
		}
	}
	for name, answer := range map[string]string{
		"refused for another reason": "echo 'No --root was given.' >&2; exit 2",
		"crashed":                    "exit 1",
	} {
		binary := standInGoChild(test, root, answer)
		if given, probeError := everyBuildArguments(binary, root, os.Environ()); probeError == nil ||
			!strings.Contains(probeError.Error(), binary) {
			test.Errorf("a cohere that %s is given %q, want the pair refused naming %s: %v", name, given, binary,
				probeError)
		}
	}
}

// A run of a side's Go child is given what the probe found that side needs to check every build, after the run's own
// arguments, so a base from before the flip checks every build as the branch does (#qqyq61s).
func TestARunGivesTheSideItsEveryBuildArguments(test *testing.T) {
	test.Parallel()
	corpus := test.TempDir()
	binary := standInGoChild(test, corpus, "exit 0")
	planned := plannedRun{corpus: "root", round: 1, side: sideBase, mode: modeNoCache}
	runGoChild(builtBinary{side: sideBase, path: binary, everyBuildArguments: []string{"--other-builds"}}, corpus,
		planned, test.TempDir(), os.Environ(), test.TempDir())
	asked, readError := os.ReadFile(filepath.Join(filepath.Dir(binary), "asked"))
	if readError != nil {
		test.Fatal(readError)
	}
	want := []string{"go-language", "--contract", "4", "--root", corpus, "--no-fix", "--other-builds", "--no-cache"}
	if got := strings.Fields(string(asked)); !slices.Equal(got, want) {
		test.Errorf("the Go child was asked %q, want %q", got, want)
	}
}
