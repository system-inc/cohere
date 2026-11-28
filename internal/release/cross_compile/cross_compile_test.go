package cross_compile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	release "github.com/system-inc/cohere/internal/release/packaging"
)

// crossCompileVariable chooses how TestEveryReleaseTargetCompiles proves the targets. Unset, it
// type-checks cohere for each one from source, here and in release.yml. Set to "build", it builds each one
// as the release does, for whoever wants that proof from a test.
//
// Measured on private caches (#nkbfkvn), for all six targets: building costs 17.1 GB of Go cache cold,
// and 5.5 GB more after one real line in internal/lint/rule, because every package that imports it
// recompiles six times. Type-checking from source costs 12 MB cold and nothing after the same edit, in
// about five seconds, since nothing is compiled. `go vet` per target was no better than building, at
// 2.7 GB cold for one target: it compiles every dependency for its export data. Building on every
// local `go test ./...` was most of the 25 GB an hour that filled the disk on 2026-10-03.
//
// Type-checking catches what the Windows break was, a call to something one platform does not define,
// and any type error in a file only one platform compiles. What only a build catches, a link-time
// failure, is left to release.yml's staging, which builds all six right after its tests and fails on
// any that does not build. Asking the test to build them there as well cost a three-core runner 600
// seconds and a timeout (dry run 37238149770), for a proof staging gives anyway.
const crossCompileVariable = "COHERE_CROSS_COMPILE"

// TestEveryReleaseTargetCompiles builds cohere for every platform a release ships, so a target that
// stops compiling fails `go test` instead of the next release.
//
// `go test` otherwise builds only for the host. cohere stopped compiling for windows at `11a25134`
// and nothing in the ordinary gate noticed, because the break was in a Unix-only process-group call
// and every machine that ran the tests was a Mac. The first place it would have shown up was a
// release, the most expensive place there is to find it.
//
// It ranges over release.Targets itself rather than a list of its own, because a second list is how a
// dropped or renamed platform stops being checked without anything failing. It compiles through
// release.GoBuildCommand, the same command the release runs, for the same reason.
//
// It type-checks rather than builds, locally and in release.yml; crossCompileVariable says why, with the
// measurements. `-short` skips it for a quick local loop and says so.
func TestEveryReleaseTargetCompiles(test *testing.T) {
	test.Parallel()

	if testing.Short() {
		test.Skipf(
			"NOT MEASURED: -short skips checking cohere for the %d release targets, so a platform that no longer builds would pass here",
			len(release.Targets),
		)
	}
	mode := os.Getenv(crossCompileVariable)
	if mode != "" && mode != "build" {
		test.Fatalf(
			"%s is %q; it is unset to type-check each target, or \"build\" to build each one",
			crossCompileVariable,
			mode,
		)
	}

	moduleDirectory := filepath.Join("..", "..", "..")

	for _, target := range release.Targets {
		// Six type-checks at once hold about 5.4 GB, 900 MB each. One after another they were most of
		// packaging's wall on every edit, about 6s against about 2s in parallel (#nxgt2ca).
		test.Run(target.String(), func(test *testing.T) {
			test.Parallel()
			if mode != "build" {
				if problems := typeCheckTarget(moduleDirectory, target); len(problems) > 0 {
					test.Fatalf(
						"cohere does not type-check for %s, so a release would fail there:\n%s",
						target,
						strings.Join(problems, "\n"),
					)
				}
				return
			}
			profile, readProfileError := release.ReadProfile(moduleDirectory)
			if readProfileError != nil {
				test.Fatal(readProfileError)
			}
			command := release.GoBuildCommand(
				moduleDirectory,
				target,
				strings.Join(release.StripFlags, " "),
				profile,
				filepath.Join(test.TempDir(), target.BinaryFileName()),
			)
			if output, outputError := command.CombinedOutput(); outputError != nil {
				test.Fatalf(
					"cohere does not compile for %s, so a release would fail there: %v\n%s",
					target,
					outputError,
					output,
				)
			}
		})
	}
}

// typeCheckTarget type-checks the package the release builds, and everything it imports, from source as
// target would compile it, and returns every error, or none. Nothing is compiled, so nothing is written
// to the build cache.
func typeCheckTarget(moduleDirectory string, target release.Target) []string {
	return TypeCheck(moduleDirectory, target, false, "./command/cohere")
}

// TestTypeCheckCatchesAPlatformOnlyBreak is the known-dirty control for the local path: the darwin-only
// call the build control below uses, and a type error only a windows file holds, both fail the
// windows type-check and neither fails darwin's.
func TestTypeCheckCatchesAPlatformOnlyBreak(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeFile(test, filepath.Join(directory, "go.mod"), "module cross_compile\n\ngo 1.21\n")
	writeFile(
		test,
		filepath.Join(directory, "command", "cohere", "main.go"),
		"package main\n\nfunc main() { onlyOnDarwin() }\n",
	)
	writeFile(
		test,
		filepath.Join(directory, "command", "cohere", "only_darwin.go"),
		"package main\n\nfunc onlyOnDarwin() {}\n",
	)
	writeFile(
		test,
		filepath.Join(directory, "command", "cohere", "broken_windows.go"),
		"package main\n\nvar broken int = \"text\"\n",
	)

	if problems := typeCheckTarget(
		directory,
		release.Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"},
	); len(problems) > 0 {
		test.Fatalf("the control does not type-check even for darwin, so it cannot show anything: %v", problems)
	}
	problems := strings.Join(
		typeCheckTarget(directory, release.Target{GoOperatingSystem: "windows", GoArchitecture: "amd64"}),
		"\n",
	)
	for _, want := range []string{"onlyOnDarwin", "broken_windows.go"} {
		if !strings.Contains(problems, want) {
			test.Errorf("the windows type-check did not report %s: %q", want, problems)
		}
	}
}

// TestTypeCheckOfTestsCatchesAUnixOnlyTestHelper is the known-dirty control for windows_tests'
// TestEveryPackagesTestsTypeCheckForWindows: a test that calls a helper only Unix defines type-checks for
// darwin and fails for windows, and only when tests are checked. It reads only its fixture, so it stays
// here beside TypeCheck, whose tests mode it proves, and stays cached when the module changes.
func TestTypeCheckOfTestsCatchesAUnixOnlyTestHelper(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeFile(test, filepath.Join(directory, "go.mod"), "module cross_compile\n\ngo 1.21\n")
	writeFile(test, filepath.Join(directory, "tool", "tool.go"), "package tool\n")
	writeFile(
		test,
		filepath.Join(directory, "tool", "helper_unix_test.go"),
		"//go:build unix\n\npackage tool\n\nfunc stand() string { return \"/bin/sh\" }\n",
	)
	writeFile(
		test,
		filepath.Join(directory, "tool", "tool_test.go"),
		"package tool\n\nimport \"testing\"\n\nfunc TestTool(t *testing.T) { _ = stand() }\n",
	)

	darwin := release.Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"}
	windows := release.Target{GoOperatingSystem: "windows", GoArchitecture: "amd64"}
	if problems := TypeCheck(directory, darwin, true, "./..."); len(problems) > 0 {
		test.Fatalf("the control does not type-check even for darwin, so it cannot show anything: %v", problems)
	}
	if problems := TypeCheck(directory, windows, false, "./..."); len(problems) > 0 {
		test.Fatalf("the control's code alone failed for windows, so it does not isolate the tests: %v", problems)
	}
	if problems := strings.Join(
		TypeCheck(directory, windows, true, "./..."),
		"\n",
	); !strings.Contains(problems, "stand") {
		test.Errorf("the windows type-check of the tests did not report the Unix-only helper: %q", problems)
	}
}

// TestCrossCompileCatchesAPlatformOnlyBreak is the known-dirty control for the test above. A check
// that has never failed has not been shown to check anything, and the shape here is the shape of the
// windows break that motivated it: code that calls something only one platform defines.
func TestCrossCompileCatchesAPlatformOnlyBreak(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeFile(test, filepath.Join(directory, "go.mod"), "module cross_compile\n\ngo 1.21\n")
	writeFile(
		test,
		filepath.Join(directory, "command", "cohere", "main.go"),
		"package main\n\nfunc main() { onlyOnDarwin() }\n",
	)
	writeFile(
		test,
		filepath.Join(directory, "command", "cohere", "only_darwin.go"),
		"package main\n\nfunc onlyOnDarwin() {}\n",
	)

	darwin := release.Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"}
	if output, outputError := release.GoBuildCommand(
		directory,
		darwin,
		"",
		release.Profile{},
		filepath.Join(test.TempDir(), "cohere"),
	).CombinedOutput(); outputError != nil {
		test.Fatalf(
			"the control does not compile even for darwin, so it cannot show anything: %v\n%s",
			outputError,
			output,
		)
	}

	windows := release.Target{GoOperatingSystem: "windows", GoArchitecture: "amd64"}
	output, outputError := release.GoBuildCommand(
		directory,
		windows,
		"",
		release.Profile{},
		filepath.Join(test.TempDir(), "cohere.exe"),
	).CombinedOutput()
	if outputError == nil {
		test.Fatal(
			"a call to a darwin-only function compiled for windows, so the cross-compile check cannot catch a platform-only break",
		)
	}
	if !strings.Contains(string(output), "onlyOnDarwin") {
		test.Fatalf("the windows build failed, but not on the darwin-only call: %s", output)
	}
}

func writeFile(test *testing.T, path string, contents string) {
	test.Helper()
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}
