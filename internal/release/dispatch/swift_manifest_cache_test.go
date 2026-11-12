package dispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// An engine rebuilt after a tracked file was edited says its tree was modified. The manifest reads git's
// answer into the build, and SwiftPM's shared manifest cache would otherwise hand back the answer from an
// earlier evaluation of the same commit, so the rebuilt engine would call an edited tree clean. The stand-in
// package's product prints the flag its manifest read, and the real build runs twice, clean then edited.
func TestASwiftEngineRebuiltAfterAnEditSaysTheTreeWasModified(test *testing.T) {
	test.Parallel()

	if runtime.GOOS != "darwin" {
		test.Skip("the Swift engine is built only on macOS")
	}
	if _, lookPathError := exec.LookPath("swift"); lookPathError != nil {
		test.Skip("no swift toolchain on PATH")
	}

	module := test.TempDir()
	packageDirectory := filepath.Join(module, "swift")
	writeFile(test, filepath.Join(packageDirectory, "Package.swift"), `// swift-tools-version:6.2
import PackageDescription
let modified = Context.gitInformation?.hasUncommittedChanges ?? true
let package = Package(name: "Stand", products: [.executable(name: "cohere-swift", targets: ["Stand"])],
    targets: [.executableTarget(name: "Stand", swiftSettings: modified ? [.define("TREE_MODIFIED")] : [])])
`)
	source := filepath.Join(packageDirectory, "Sources", "Stand", "main.swift")
	writeFile(test, source, "#if TREE_MODIFIED\nprint(\"modified\")\n#else\nprint(\"clean\")\n#endif\n")
	writeFile(test, filepath.Join(module, ".gitignore"), ".scratch/\n")
	git := func(arguments ...string) {
		test.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = module
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com")
		if output, outputError := command.CombinedOutput(); outputError != nil {
			test.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), outputError, output)
		}
	}
	git("init", "--quiet")
	git("add", ".")
	git("commit", "--quiet", "-m", "stand-in engine")

	// One scratch path for both builds, as the cache directory is for every development build.
	scratch := filepath.Join(module, ".scratch")
	says := func() string {
		test.Helper()
		binaryPath := filepath.Join(test.TempDir(), "cohere-swift")
		if buildSwiftProductWithSwiftPmError := buildSwiftProductWithSwiftPm(
			packageDirectory,
			scratch,
			binaryPath,
			nil,
		); buildSwiftProductWithSwiftPmError != nil {
			test.Fatal(buildSwiftProductWithSwiftPmError)
		}
		output, outputError := exec.Command(binaryPath).Output()
		if outputError != nil {
			test.Fatal(outputError)
		}
		return strings.TrimSpace(string(output))
	}

	if got := says(); got != "clean" {
		test.Fatalf("an engine built from a clean tree says %q", got)
	}
	writeFile(test, source, "#if TREE_MODIFIED\nprint(\"modified\")\n#else\nprint(\"clean\")\n#endif\n// edited\n")
	if got := says(); got != "modified" {
		test.Fatalf(
			"an engine rebuilt after a tracked file was edited says %q, so its provenance would call the tree clean",
			got,
		)
	}
}
