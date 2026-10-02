package release

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryReleaseTargetCompiles builds cohere for every platform a release ships, so a target that
// stops compiling fails `go test` instead of the next release.
//
// `go test` otherwise builds only for the host. cohere stopped compiling for windows at `11a25134`
// and nothing in the ordinary gate noticed, because the break was in a Unix-only process-group call
// and every machine that ran the tests was a Mac. The first place it would have shown up was a
// release, the most expensive place there is to find it.
//
// It ranges over Targets itself rather than a list of its own, because a second list is how a
// dropped or renamed platform stops being checked without anything failing. It compiles through
// goBuildCommand, the same command the release runs, for the same reason.
//
// Measured on an M-series Mac: about a minute per target the first time a toolchain sees it, then
// zero to six seconds warm, because the build cache is kept per GOOS and GOARCH. The gate runs plain
// `go test ./...`, so it always runs this; `-short` skips it for a quick local loop and says so.
func TestEveryReleaseTargetCompiles(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skipf("NOT MEASURED: -short skips cross-compiling cohere for the %d release targets, so a platform that no longer builds would pass here", len(Targets))
	}

	moduleDirectory := filepath.Join("..", "..", "..")

	for _, target := range Targets {
		t.Run(target.String(), func(t *testing.T) {
			t.Parallel()

			command := goBuildCommand(moduleDirectory, target, strings.Join(StripFlags, " "), filepath.Join(t.TempDir(), target.BinaryFileName()))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("cohere does not compile for %s, so a release would fail there: %v\n%s", target, err, output)
			}
		})
	}
}

// TestCrossCompileCatchesAPlatformOnlyBreak is the known-dirty control for the test above. A check
// that has never failed has not been shown to check anything, and the shape here is the shape of the
// windows break that motivated it: code that calls something only one platform defines.
func TestCrossCompileCatchesAPlatformOnlyBreak(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "go.mod"), "module crosscompile\n\ngo 1.21\n")
	writeFile(t, filepath.Join(directory, "command", "cohere", "main.go"), "package main\n\nfunc main() { onlyOnDarwin() }\n")
	writeFile(t, filepath.Join(directory, "command", "cohere", "only_darwin.go"), "package main\n\nfunc onlyOnDarwin() {}\n")

	darwin := Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"}
	if output, err := goBuildCommand(directory, darwin, "", filepath.Join(t.TempDir(), "cohere")).CombinedOutput(); err != nil {
		t.Fatalf("the control does not compile even for darwin, so it cannot show anything: %v\n%s", err, output)
	}

	windows := Target{GoOperatingSystem: "windows", GoArchitecture: "amd64"}
	output, err := goBuildCommand(directory, windows, "", filepath.Join(t.TempDir(), "cohere.exe")).CombinedOutput()
	if err == nil {
		t.Fatal("a call to a darwin-only function compiled for windows, so the cross-compile check cannot catch a platform-only break")
	}
	if !strings.Contains(string(output), "onlyOnDarwin") {
		t.Fatalf("the windows build failed, but not on the darwin-only call: %s", output)
	}
}
