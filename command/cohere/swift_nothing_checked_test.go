//go:build darwin

// The real cohere-swift runs only on macOS, where it ships.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/release/dispatch"
)

// realSwiftBinary is the child built from this checkout's swift_language/, resolved once for the test binary as a
// development run resolves it: reused while its sources are unchanged, built when they moved.
var realSwiftBinary struct {
	once         sync.Once
	path         string
	resolveError error
}

// swiftTestCacheDirectory is where the tests keep the cohere-swift they build: the person's user cache directory,
// read before TestMain moved the home, under `cohere/test-swift`, or when there is none or it can't be made, the user
// cache directory of the home TestMain gave this test binary, which it removes once the tests have run.
//
// Never the module's `.cache/cohere`, where a development run keeps its own (dispatch.DefaultPaths): a `.cache` the
// tests made at the module's root changed the root's listing, which Go's test cache keys every test that reads it
// on, so about 50 packages reran on the next landing (#d49qcjg). Kept outside the module, nothing checks the file
// again, and nothing has to: the binary's name carries the hash of the sources, the toolchain and the platform it was
// built from (dispatch.SwiftBinaryPath), so an edit is a new name and a stale binary is never served.
func swiftTestCacheDirectory() (string, error) {
	if filepath.IsAbs(usersCacheDirectory) {
		directory := filepath.Join(usersCacheDirectory, "cohere", "test-swift")
		// Not returned: a user cache that can't be written is the case the fallback below is for.
		if createDirectoryError := os.MkdirAll(directory, 0o755); createDirectoryError == nil {
			return directory, nil
		}
	}
	ownCache, userCacheDirectoryError := os.UserCacheDir()
	if userCacheDirectoryError != nil {
		return "", fmt.Errorf("finding a cache directory for cohere-swift outside the module: %w",
			userCacheDirectoryError)
	}
	return filepath.Join(ownCache, "cohere", "test-swift"), nil
}

// builtSwiftBinary is the path of the child a development cohere would run, built from this checkout's sources.
func builtSwiftBinary(test *testing.T) string {
	test.Helper()
	realSwiftBinary.once.Do(func() {
		moduleDirectory, findModuleDirectoryError := dispatch.FindModuleDirectory()
		if findModuleDirectoryError != nil {
			realSwiftBinary.resolveError = findModuleDirectoryError
			return
		}
		cacheDirectory, swiftTestCacheDirectoryError := swiftTestCacheDirectory()
		if swiftTestCacheDirectoryError != nil {
			realSwiftBinary.resolveError = swiftTestCacheDirectoryError
			return
		}
		versionOutput, swiftVersionOutputError := dispatch.SwiftVersionOutput()
		if swiftVersionOutputError != nil {
			realSwiftBinary.resolveError = swiftVersionOutputError
			return
		}
		realSwiftBinary.path, _, realSwiftBinary.resolveError = dispatch.ResolveSwiftBinary(
			dispatch.Paths{ModuleDirectory: moduleDirectory, CacheDirectory: cacheDirectory},
			"",
			contractVersion,
			versionOutput,
		)
	})
	if realSwiftBinary.resolveError != nil {
		test.Fatalf("cannot resolve cohere-swift: %v", realSwiftBinary.resolveError)
	}
	return realSwiftBinary.path
}

// toolsWithoutGit is a directory holding the tools a Swift run starts and nothing else, git least of all, so a run
// with it as PATH proves cohere asks no git process which files are tracked (#67vevpz).
func toolsWithoutGit(test *testing.T) string {
	test.Helper()
	tools := test.TempDir()
	for _, tool := range []string{"swift", "xcrun", "xcodebuild", "sh", "env"} {
		found, lookPathError := exec.LookPath(tool)
		if lookPathError != nil {
			test.Fatalf("no %s on PATH for a Swift run: %v", tool, lookPathError)
		}
		if symlinkError := os.Symlink(found, filepath.Join(tools, tool)); symlinkError != nil {
			test.Fatal(symlinkError)
		}
	}
	return tools
}

// swiftNothingCheckedManifest is a one-target package that every package rule passes, so a run's verdict is decided
// by its one source file alone.
const swiftNothingCheckedManifest = `// swift-tools-version:6.2
import PackageDescription

let package = Package(
    name: "App",
    targets: [
        .target(
            name: "App",
            swiftSettings: [
                .enableUpcomingFeature("ExistentialAny"),
                .enableUpcomingFeature("MemberImportVisibility"),
                .strictMemorySafety(),
            ]
        )
    ]
)
`

// swiftPackageInRepository writes the package with ignoreFile as its `.gitignore`, in a repository of its own whose
// index tracks the manifest, the ignore file, and each of forceAdded with `git add -f`, and returns its root.
func swiftPackageInRepository(test *testing.T, ignoreFile string, forceAdded ...string) string {
	test.Helper()
	root := filepath.Join(test.TempDir(), "App")
	for name, contents := range map[string]string{
		"Package.swift":         swiftNothingCheckedManifest,
		".gitignore":            ignoreFile,
		"Sources/App/App.swift": "struct App {}\n",
	} {
		path := filepath.Join(root, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	commands := [][]string{{"init", "--quiet"}, {"add", "Package.swift", ".gitignore"}}
	if len(forceAdded) > 0 {
		commands = append(commands, append([]string{"add", "--force"}, forceAdded...))
	}
	for _, arguments := range commands {
		command := exec.Command("git", arguments...)
		command.Dir = root
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if output, outputError := command.CombinedOutput(); outputError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, outputError, output)
		}
	}
	return root
}

// swiftJsonSummary is the part of `--json`'s last record these runs are judged by.
type swiftJsonSummary struct {
	Kind    string `json:"kind"`
	Verdict string `json:"verdict"`
	Checked int    `json:"checked"`
	Gaps    struct {
		NothingToCheck bool             `json:"nothingToCheck"`
		Excluded       []excludedReason `json:"excluded"`
	} `json:"gaps"`
}

// A Swift package whose every source file git ignores fails through the front door, with the real cohere-swift built
// from this checkout and no git on PATH: exit 1, `--json`'s verdict "fail", and the footer and `gaps.excluded` naming
// the
// `.gitignore` line that ignored the file, which only the child's excluded record carries. The same package with the
// file force-added, which git tracks and so never ignores, passes with the file checked, as does a package with
// nothing ignored (#08cv2mp).
//
// It builds the child when this checkout's Swift sources changed since the last build, which takes minutes, so -short
// leaves it out.
func TestARealSwiftRunThatExcludedEveryFileFails(test *testing.T) {
	test.Parallel()
	if testing.Short() {
		test.Skip("runs the real cohere-swift, built from this checkout when its sources changed")
	}
	binary := buildCohere(test)
	environment := []string{
		dispatch.SwiftBinaryOverrideVariable + "=" + builtSwiftBinary(test),
		"PATH=" + toolsWithoutGit(test),
	}
	// run is a `--no-fix` run at root: its standard output, and with `--json` nothing else, since the line saying
	// which language ran goes to standard error, and its exit code.
	run := func(test *testing.T, root string, arguments ...string) (string, int) {
		test.Helper()
		command := exec.Command(binary, append([]string{"--no-fix"}, arguments...)...)
		command.Dir = root
		command.Env = append(os.Environ(), environment...)
		var errors strings.Builder
		command.Stderr = &errors
		output, outputError := command.Output()
		if outputError == nil {
			return string(output), 0
		}
		exitError, isExit := outputError.(*exec.ExitError)
		if !isExit {
			test.Fatalf("running cohere: %v\n%s%s", outputError, output, errors.String())
		}
		return string(output), childExitCode(test, exitError)
	}
	summary := func(test *testing.T, root string) (swiftJsonSummary, int) {
		test.Helper()
		output, exitCode := run(test, root, "--json")
		lines := strings.Split(strings.TrimSpace(output), "\n")
		var last swiftJsonSummary
		if unmarshalError := json.Unmarshal([]byte(lines[len(lines)-1]), &last); unmarshalError != nil ||
			last.Kind != "summary" {
			test.Fatalf("--json did not end in a summary (%v):\n%s", unmarshalError, output)
		}
		return last, exitCode
	}

	test.Run("every source file ignored by git", func(test *testing.T) {
		test.Parallel()
		root := swiftPackageInRepository(test, ".cache/\nSources/**/*.swift\n")
		footerOutput, footerExit := run(test, root)
		lines := strings.Split(strings.TrimSpace(footerOutput), "\n")
		footerLine := lines[len(lines)-1]
		if footerExit != 1 || !strings.HasPrefix(footerLine, "✗") ||
			!strings.Contains(footerLine, "no files checked: ") ||
			!strings.Contains(footerLine, "ignored by git (.gitignore:2)") {
			test.Errorf("exit %d and the footer %q, want 1 and a failure naming .gitignore:2\n%s",
				footerExit, footerLine, footerOutput)
		}
		got, exitCode := summary(test, root)
		ignored := excludedReason{Reason: "ignored by git", Count: 1, Source: ".gitignore:2"}
		named := false
		for _, excluded := range got.Gaps.Excluded {
			named = named || excluded == ignored
		}
		if exitCode != 1 || got.Verdict != "fail" || !got.Gaps.NothingToCheck || !named || got.Checked != 0 {
			test.Errorf("--json exited %d with %+v, want 1, verdict fail, nothing checked and %+v among the reasons",
				exitCode, got, ignored)
		}
	})

	test.Run("the file force-added", func(test *testing.T) {
		test.Parallel()
		root := swiftPackageInRepository(test, ".cache/\nSources/**/*.swift\n", "Sources/App/App.swift")
		got, exitCode := summary(test, root)
		if exitCode != 0 || got.Verdict != "pass" || got.Checked != 1 || len(got.Gaps.Excluded) != 0 {
			test.Errorf("--json exited %d with %+v, want 0, verdict pass and the tracked file checked", exitCode, got)
		}
	})

	test.Run("one file, nothing ignored", func(test *testing.T) {
		test.Parallel()
		root := swiftPackageInRepository(test, ".cache/\n", "Sources/App/App.swift")
		got, exitCode := summary(test, root)
		if exitCode != 0 || got.Verdict != "pass" || got.Checked != 1 {
			test.Errorf("--json exited %d with %+v, want 0, verdict pass and its one file checked", exitCode, got)
		}
	})
}
