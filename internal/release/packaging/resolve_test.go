package release

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// The defect every test here guards is one shape: a resolution that produces something runnable
// when it should have produced an error. That is the failure that cost days: a resolver found no
// binary, returned a bare command name, and the failed spawn read as a clean tree. So these assert
// on the error as hard as they assert on the success.

// Not parallel: clearOverride empties COHERE_BINARY with test.Setenv, which a parallel test may not call.
func TestResolveFailsLoudlyWhenNothingIsInstalled(test *testing.T) {
	// The central case. An empty tree must be an error, never a bare name and never a guess.
	clearOverride(test)
	root := test.TempDir()

	path, resolveError := Resolve([]string{root})
	if resolveError == nil {
		test.Fatalf("resolved %q with nothing installed, which is the fallback that prints green over zero files", path)
	}
	if !errors.Is(resolveError, ErrNoBinary) {
		test.Fatalf(
			"error did not identify as ErrNoBinary, so a caller cannot tell a missing platform from a broken lookup: %v",
			resolveError,
		)
	}

	// The message has to be actionable on its own, because the person reading it is looking at a
	// failed install and has no other source of truth about what was wanted.
	message := resolveError.Error()
	for _, required := range []string{
		runtime.GOOS,
		runtime.GOARCH,
		CurrentPlatformPackageName(),
		DispatcherPackageName,
		BinaryOverrideVariable,
	} {
		if !strings.Contains(message, required) {
			test.Errorf("the failure never mentions %q, so it does not say what was missing: %s", required, message)
		}
	}
}

// Not parallel: clearOverride empties COHERE_BINARY with test.Setenv, which a parallel test may not call.
func TestResolveFindsThePlatformPackage(test *testing.T) {
	clearOverride(test)
	root := test.TempDir()
	expected := writePlatformBinary(test, root, 0o755)

	path, resolveError := Resolve([]string{root})
	if resolveError != nil {
		test.Fatalf("a correctly installed platform package did not resolve: %v", resolveError)
	}
	if path != expected {
		test.Fatalf("resolved %s, wanted the installed binary at %s", path, expected)
	}
}

// Not parallel: clearOverride empties COHERE_BINARY with test.Setenv, which a parallel test may not call.
func TestResolveWalksUpToAWorkspaceRoot(test *testing.T) {
	// The monorepo case: the binary is hoisted to the workspace root, and cohere runs from a
	// package several directories down. A resolver that only looks beside itself reports a missing
	// platform for an install that is actually fine.
	clearOverride(test)
	workspace := test.TempDir()
	expected := writePlatformBinary(test, workspace, 0o755)

	nested := filepath.Join(workspace, "applications", "web")
	if createDirectoryError := os.MkdirAll(nested, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}

	path, resolveError := Resolve(SearchRoots(nested, ""))
	if resolveError != nil {
		test.Fatalf("a hoisted install did not resolve from a nested package: %v", resolveError)
	}
	if path != expected {
		test.Fatalf("resolved %s, wanted %s", path, expected)
	}
}

// Not parallel: clearOverride empties COHERE_BINARY with test.Setenv, which a parallel test may not call.
func TestResolveReportsABinaryThatIsNotExecutable(test *testing.T) {
	// A packaging step that drops the mode bit ships a file that is present and unrunnable. Left to
	// exec, it surfaces as a permission error that reads like a broken machine. Reported here, it
	// names the actual cause.
	if runtime.GOOS == "windows" {
		test.Skip("Windows has no executable bit; the extension decides")
	}
	clearOverride(test)
	root := test.TempDir()
	path := writePlatformBinary(test, root, 0o644)

	resolved, resolveError := Resolve([]string{root})
	if resolveError == nil {
		test.Fatalf("resolved %s despite it being unrunnable", resolved)
	}
	if !strings.Contains(resolveError.Error(), path) {
		test.Fatalf("the failure does not name the file that cannot run: %v", resolveError)
	}
	if errors.Is(resolveError, ErrNoBinary) {
		test.Fatalf(
			"a present-but-unrunnable binary reported as a missing platform, which sends the reader hunting the wrong bug: %v",
			resolveError,
		)
	}
}

// Not parallel: it points COHERE_BINARY at its override with test.Setenv, which a parallel test may not call.
func TestOverrideTakesPrecedence(test *testing.T) {
	root := test.TempDir()
	writePlatformBinary(test, root, 0o755)

	override := filepath.Join(test.TempDir(), "cohere-local")
	writeExecutable(test, override, 0o755)
	test.Setenv(BinaryOverrideVariable, override)

	path, resolveError := Resolve([]string{root})
	if resolveError != nil {
		test.Fatalf("the override did not resolve: %v", resolveError)
	}
	if path != override {
		test.Fatalf(
			// The dash is written as its escape, the same bytes, so the message reads as it always has.
			"resolved %s while %s pointed at %s \u2014 a developer would read results from the wrong binary as their own",
			path,
			BinaryOverrideVariable,
			override,
		)
	}
}

// Not parallel: it points COHERE_BINARY at its override with test.Setenv, which a parallel test may not call.
func TestOverrideFailsLoudlyWhenItPointsAtNothing(test *testing.T) {
	// A stale export in a shell profile. Falling back to the installed binary here would silently
	// run something other than what was asked for, and the results would be read as the local
	// build's. So a bad override is fatal even when a perfectly good install is sitting right there.
	root := test.TempDir()
	writePlatformBinary(test, root, 0o755)

	missing := filepath.Join(test.TempDir(), "not-here")
	test.Setenv(BinaryOverrideVariable, missing)

	path, resolveError := Resolve([]string{root})
	if resolveError == nil {
		test.Fatalf("a broken override silently fell back to %s", path)
	}
	if !strings.Contains(resolveError.Error(), missing) ||
		!strings.Contains(resolveError.Error(), BinaryOverrideVariable) {
		test.Fatalf("the failure does not say what was set or where it pointed: %v", resolveError)
	}
}

// Not parallel: it points COHERE_BINARY at its override with test.Setenv, which a parallel test may not call.
func TestOverrideRejectsADirectory(test *testing.T) {
	// Pointing at the build output directory rather than the binary inside it. Without this the
	// error arrives from exec as a format error, which reads like a corrupt download.
	directory := test.TempDir()
	test.Setenv(BinaryOverrideVariable, directory)

	if path, resolveError := Resolve([]string{test.TempDir()}); resolveError == nil {
		test.Fatalf("resolved a directory as a binary: %s", path)
	}
}

func TestPlatformPackageNameTranslatesGoNamesToNpmNames(test *testing.T) {
	test.Parallel()

	// Go and npm disagree in two places: windows/win32 and amd64/x64. A package published under the
	// Go spelling installs correctly and is never found, which on the machine is indistinguishable
	// from a platform we never shipped. The keys here are Go's names, which is the contract.
	cases := map[string]string{
		"darwin/amd64":  "@system-inc/cohere-darwin-x64",
		"darwin/arm64":  "@system-inc/cohere-darwin-arm64",
		"linux/amd64":   "@system-inc/cohere-linux-x64",
		"linux/arm64":   "@system-inc/cohere-linux-arm64",
		"windows/amd64": "@system-inc/cohere-win32-x64",
		"windows/arm64": "@system-inc/cohere-win32-arm64",
	}
	for platform, expected := range cases {
		goOperatingSystem, goArchitecture, _ := strings.Cut(platform, "/")
		if got := PlatformPackageName(goOperatingSystem, goArchitecture); got != expected {
			test.Errorf("%s resolved to %s, wanted %s", platform, got, expected)
		}
	}
}

func TestBinaryFileNameCarriesTheWindowsExtension(test *testing.T) {
	test.Parallel()

	// Without the extension the file does not execute on Windows at all.
	if got := BinaryFileName("windows"); got != "cohere.exe" {
		test.Errorf("windows binary named %s", got)
	}
	if got := BinaryFileName("darwin"); got != "cohere" {
		test.Errorf("darwin binary named %s", got)
	}
}

// clearOverride makes a test independent of the developer's own shell.
//
// Without this, a machine with COHERE_BINARY exported would pass the "nothing is installed"
// test by resolving the developer's local build, the exact green-over-nothing result these tests
// exist to catch.
func clearOverride(test *testing.T) {
	test.Helper()
	test.Setenv(BinaryOverrideVariable, "")
}

// writePlatformBinary installs a fake platform package under root and returns the binary's path.
func writePlatformBinary(test *testing.T, root string, mode os.FileMode) string {
	test.Helper()
	path := filepath.Join(
		root, "node_modules",
		filepath.FromSlash(CurrentPlatformPackageName()),
		"bin", BinaryFileName(runtime.GOOS),
	)
	writeExecutable(test, path, mode)
	return path
}

// writeExecutable writes a placeholder file with an explicit mode.
func writeExecutable(test *testing.T, path string, mode os.FileMode) {
	test.Helper()
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := executable_testing.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), mode); writeError != nil {
		test.Fatal(writeError)
	}
	// WriteFile's mode is masked by umask, so it is set explicitly. A test that meant to write an
	// unrunnable file and wrote a runnable one would pass while asserting nothing.
	if changeModeError := os.Chmod(path, mode); changeModeError != nil {
		test.Fatal(changeModeError)
	}
}
