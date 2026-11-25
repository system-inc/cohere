//go:build unix

// The stand-in children here are /bin/sh scripts, so the file builds on Unix, where the child ships.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/build_inputs"
	"github.com/system-inc/cohere/internal/executable_testing"
	"github.com/system-inc/cohere/internal/needs"
	"github.com/system-inc/cohere/internal/release/dispatch"
	"github.com/system-inc/cohere/internal/release/packaging"
)

// installCohere puts cohere at `<install>/bin/cohere`, the layout of a platform package, stamped with
// version when it is not empty, and returns the binary. The install directory has no cohere module above
// it, so the only cohere-swift a run can find is one placed beside the binary.
//
// Each install is a directory of its own, holding a hard link to a build shared by every install of that
// stamp: the package's binary for no stamp, and stampedCohere's for a version. Linking cohere once per
// install was three links of about 50 MB in each package run, for two distinct binaries (#nxgt2ca). Nothing
// writes to an installed binary; a test places files beside it.
func installCohere(test *testing.T, version string) string {
	test.Helper()
	binary := filepath.Join(test.TempDir(), "bin", "cohere")
	if createDirectoryError := os.MkdirAll(filepath.Dir(binary), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	source := buildCohere(test)
	if version != "" {
		source = stampedCohere(test, version)
	}
	if linkError := os.Link(source, binary); linkError != nil {
		// Across filesystems a link fails, and a copy is the same install.
		contents, readError := os.ReadFile(source)
		if readError != nil {
			test.Fatal(readError)
		}
		if writeError := executable_testing.WriteFile(binary, contents, 0o755); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return binary
}

// stampedCohere returns this command's binary stamped with version, built on the first call for it.
func stampedCohere(test *testing.T, version string) string {
	test.Helper()
	stampedBinaries.Lock()
	stamped := stampedBinaries.byVersion[version]
	if stamped == nil {
		stamped = &stampedBinary{}
		stampedBinaries.byVersion[version] = stamped
	}
	stampedBinaries.Unlock()
	build_inputs.Record(test, ".", ".")
	stamped.once.Do(func() {
		stamped.directory, stamped.buildError = os.MkdirTemp("", "cohere-test-stamped-binary-")
		if stamped.buildError != nil {
			return
		}
		stamped.path = filepath.Join(stamped.directory, "cohere")
		build := exec.Command("go", "build", "-o", stamped.path,
			"-ldflags=-X github.com/system-inc/cohere/internal/release/packaging.version="+version, ".")
		stamped.output, stamped.buildError = build.CombinedOutput()
	})
	if stamped.buildError != nil {
		test.Fatalf("cannot build cohere stamped %s: %v\n%s", version, stamped.buildError, stamped.output)
	}
	return stamped.path
}

// placeSiblingSwiftBinary puts a stand-in cohere-swift beside binary that records its arguments in a file
// and streams a clean run, and returns the file it records into.
func placeSiblingSwiftBinary(test *testing.T, binary string) string {
	test.Helper()
	clean, absolutePathError := filepath.Abs(filepath.Join("..", "..", "swift_language", "Contract", "Clean.jsonl"))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	invoked := filepath.Join(test.TempDir(), "invoked")
	script := "#!/bin/sh\necho \"$@\" > '" + invoked + "'\ncat '" + clean + "'\n"
	if writeError := executable_testing.WriteFile(
		filepath.Join(filepath.Dir(binary), release.SwiftBinaryFileName),
		[]byte(script),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	return invoked
}

// swiftPackage writes a Swift package, a Package.swift and one source, under parent, or under a new
// temporary directory when parent is empty.
func swiftPackage(test *testing.T, parent string) string {
	test.Helper()
	parentDirectory := parent
	if parent == "" {
		parentDirectory = test.TempDir()
	}
	root := filepath.Join(parentDirectory, "Toy")
	writeTree(test, root, map[string]string{
		"Package.swift":         "// swift-tools-version:6.0\nimport PackageDescription\nlet package = Package(name: \"Toy\")\n",
		"Sources/Toy/Toy.swift": "let toy = 1\n",
	})
	return root
}

// runInstalled runs an installed cohere from directory with no child named by the override, so the
// run resolves the child the way an installed one does. The override is cleared in the child's
// environment alone, not this process's, so the tests that call it run in parallel (#nxgt2ca).
func runInstalled(test *testing.T, binary string, directory string, arguments ...string) (string, int) {
	test.Helper()
	return runCohereWithEnvironment(
		test,
		binary,
		directory,
		[]string{dispatch.SwiftBinaryOverrideVariable + "="},
		arguments...,
	)
}

// swiftShipsForMacOnly is why a test of a shipped cohere-swift runs on macOS alone.
const swiftShipsForMacOnly = "cohere-swift ships for macOS alone, and elsewhere a Swift package is refused by name"

// A released cohere runs the child its package ships beside it, with no checkout anywhere, reached
// through a symlink the way a package manager's `bin` link reaches it.
func TestAReleasedCohereRunsTheSwiftBinaryShippedBesideIt(test *testing.T) {
	test.Parallel()
	needs.Platform(test, "darwin", swiftShipsForMacOnly)

	binary := installCohere(test, "1.0.0")
	invoked := placeSiblingSwiftBinary(test, binary)
	link := filepath.Join(test.TempDir(), "cohere")
	if symlinkError := os.Symlink(binary, link); symlinkError != nil {
		test.Fatal(symlinkError)
	}
	root := swiftPackage(test, "")

	// --verbose, so a child build would say so: by default a build is silent off a terminal (#ytqqv8v),
	// and the check below would pass over one.
	output, exitCode := runInstalled(test, link, root, "--no-fix", "--verbose")
	if exitCode != 0 {
		test.Fatalf("a released cohere with the child beside it exited %d:\n%s", exitCode, output)
	}
	recorded, readError := os.ReadFile(invoked)
	if readError != nil {
		test.Fatalf("the child beside cohere was never run:\n%s", output)
	}
	resolvedRoot, evalSymlinksError := filepath.EvalSymlinks(root)
	if evalSymlinksError != nil {
		test.Fatal(evalSymlinksError)
	}
	if want := "--contract 4 --root "; !strings.HasPrefix(string(recorded), want) ||
		!(strings.Contains(string(recorded), root) || strings.Contains(string(recorded), resolvedRoot)) {
		test.Fatalf(
			"the shipped cohere-swift was run with %q, expected the contract and the package root %s",
			recorded,
			root,
		)
	}
	if strings.Contains(output, "building cohere-swift") {
		test.Fatalf("a released cohere built a child:\n%s", output)
	}

	// The release reads the contract from the cohere it stages, outside any project, and refuses a
	// child that will not speak it, so the line is held here in the shape it parses.
	version, exitCode := runInstalled(test, binary, test.TempDir(), "--version")
	if exitCode != 0 || !strings.HasPrefix(version, "cohere 1.0.0\n") ||
		!strings.Contains(version, "\n  swift contract: "+strconv.Itoa(contractVersion)+"\n") {
		test.Fatalf("a released cohere's --version does not state its Swift contract (exit %d):\n%s", exitCode, version)
	}
}

// A released cohere with no child beside it is a broken install, and says so, even standing inside a
// cohere checkout it could build one from: a child built there would run unpaired rules under the
// release's version.
func TestAReleasedCohereWithNoSwiftBinaryBesideItNeverBuildsOne(test *testing.T) {
	test.Parallel()
	needs.Platform(test, "darwin", swiftShipsForMacOnly)

	binary := installCohere(test, "1.0.0")
	checkout := test.TempDir()
	writeTree(test, checkout, map[string]string{
		"go.mod":                       "module github.com/system-inc/cohere\n",
		"swift_language/Package.swift": "// swift-tools-version:6.0\n",
		"swift_language/Sources/Language/A.swift": "let a = 1\n",
	})
	root := swiftPackage(test, checkout)

	// --verbose, so a build from the checkout would say so (see above).
	output, exitCode := runInstalled(test, binary, root, "--no-fix", "--verbose")
	if exitCode != 1 {
		test.Fatalf("a released cohere with no child exited %d, expected 1:\n%s", exitCode, output)
	}
	// Named as the run names it, with the executable's symlinks resolved, which on macOS turns /tmp into
	// /private/tmp.
	installed, resolveSymlinksError := filepath.EvalSymlinks(filepath.Dir(binary))
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	expected := filepath.Join(installed, release.SwiftBinaryFileName)
	if !strings.Contains(output, "This cohere 1.0.0 install has no cohere-swift: expected "+expected) ||
		!strings.Contains(output, "reinstall cohere") {
		test.Fatalf("the missing cohere-swift was not named with the way out:\n%s", output)
	}
	if strings.Contains(output, "building cohere-swift") || strings.Contains(output, "swift --version") {
		test.Fatalf("a released cohere reached for the checkout:\n%s", output)
	}
}

// A development cohere never takes a child from beside itself, so a stale cohere-swift next to a
// `go build -o` cannot run in place of the checkout's. Outside a checkout it has none, and says so.
func TestADevelopmentCohereIgnoresASwiftBinaryBesideIt(test *testing.T) {
	test.Parallel()
	needs.Platform(test, "darwin", swiftShipsForMacOnly)

	binary := installCohere(test, "")
	invoked := placeSiblingSwiftBinary(test, binary)
	root := swiftPackage(test, "")

	output, exitCode := runInstalled(test, binary, root, "--no-fix")
	if exitCode != 1 || !strings.Contains(output, "cohere-swift is built from a cohere checkout") {
		test.Fatalf("a development cohere outside a checkout exited %d:\n%s", exitCode, output)
	}
	if _, statError := os.Stat(invoked); statError == nil {
		test.Fatalf("a development cohere ran the child beside it:\n%s", output)
	}
}

// Where the child does not ship, a Swift package is refused by name before anything is looked for or
// built, whichever kind of cohere is running. The child named by the override still runs: it is the
// child developer's, and naming one is saying what to run.
// Not parallel: it sets cohere-swift override variable with test.Setenv, the working directory with test.Chdir,
// and the package-level swiftBinaryPlatform
func TestSwiftIsRefusedByNameWhereCohereSwiftDoesNotShip(test *testing.T) {
	test.Setenv(dispatch.SwiftBinaryOverrideVariable, "")
	// Outside any cohere module, so a resolver that skipped the refusal fails at once on the checkout it
	// cannot find, rather than starting a Swift build from the module this test runs in.
	test.Chdir(test.TempDir())
	original := swiftBinaryPlatform
	test.Cleanup(func() { swiftBinaryPlatform = original })
	location := projectLocation{Root: "/work/Toy"}
	for _, platform := range []release.Target{
		{GoOperatingSystem: "linux", GoArchitecture: "amd64"},
		{GoOperatingSystem: "linux", GoArchitecture: "arm64"},
		{GoOperatingSystem: "windows", GoArchitecture: "amd64"},
		{GoOperatingSystem: "windows", GoArchitecture: "arm64"},
	} {
		swiftBinaryPlatform = platform
		for _, provenance := range []release.Provenance{{Version: "1.0.0"}, {Version: "dev"}} {
			_, _, resolveSwiftBinaryError := resolveSwiftBinary(location, provenance)
			want := "Swift is not available on this platform (" + platform.String() +
				"): cohere-swift ships only for macOS, so nothing in /work/Toy was checked"
			if resolveSwiftBinaryError == nil || resolveSwiftBinaryError.Error() != want {
				test.Errorf(
					"%s, cohere %s: got %v\nwant %s",
					platform,
					provenance.Version,
					resolveSwiftBinaryError,
					want,
				)
			}
		}
	}

	// The control: on macOS the same released cohere gets past the platform, to its sibling.
	swiftBinaryPlatform = release.Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"}
	if _, _, resolveSwiftBinaryError := resolveSwiftBinary(
		location,
		release.Provenance{Version: "1.0.0"},
	); resolveSwiftBinaryError == nil ||
		!strings.Contains(resolveSwiftBinaryError.Error(), "install has no cohere-swift") {
		test.Errorf("on macOS a released cohere was not sent to its sibling: %v", resolveSwiftBinaryError)
	}

	swiftBinary := fakeSwiftBinary(test, "")
	test.Setenv(dispatch.SwiftBinaryOverrideVariable, swiftBinary)
	swiftBinaryPlatform = release.Target{GoOperatingSystem: "linux", GoArchitecture: "amd64"}
	if binaryPath, _, resolveSwiftBinaryError := resolveSwiftBinary(
		location,
		release.Provenance{Version: "1.0.0"},
	); resolveSwiftBinaryError != nil || binaryPath != swiftBinary {
		test.Errorf("the override was refused on linux: %q, %v", binaryPath, resolveSwiftBinaryError)
	}
}
