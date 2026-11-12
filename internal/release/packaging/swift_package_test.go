package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/build_inputs"
	"github.com/system-inc/cohere/internal/executable_testing"
)

// smallMacBinary builds a tiny Go program for macOS on goArchitecture and returns its path. The guards
// below read real Mach-O load commands, so they are tested against a real Mach-O rather than a byte
// pattern written to look like one.
func smallMacBinary(test *testing.T, goArchitecture string) string {
	test.Helper()

	if runtime.GOOS != "darwin" {
		test.Skip("NOT MEASURED: lipo and vtool are macOS tools, and the Swift engine is only packaged on macOS")
	}

	directory := test.TempDir()
	writeFile(test, filepath.Join(directory, "go.mod"), "module small\n\ngo 1.21\n")
	writeFile(test, filepath.Join(directory, "main.go"), "package main\n\nfunc main() {}\n")

	binary := filepath.Join(directory, "small")
	command := exec.Command("go", "build", "-o", binary, ".")
	command.Dir = build_inputs.OutsideModule(test, directory)
	command.Env = append(command.Environ(), "GOOS=darwin", "GOARCH="+goArchitecture, "CGO_ENABLED=0")
	if output, outputError := command.CombinedOutput(); outputError != nil {
		test.Fatalf("building the sample binary: %v\n%s", outputError, output)
	}
	return binary
}

// TestArchitectureIsReadFromTheBinary holds both directions: a binary for the package's architecture
// passes, and one for the other Mac is refused, naming what it was built for.
func TestArchitectureIsReadFromTheBinary(test *testing.T) {
	test.Parallel()

	arm := smallMacBinary(test, "arm64")
	if requireArchitectureError := requireArchitecture(arm, "arm64"); requireArchitectureError != nil {
		test.Fatalf("an arm64 binary was refused as arm64: %v", requireArchitectureError)
	}

	requireArchitectureError := requireArchitecture(arm, "x86_64")
	if requireArchitectureError == nil {
		test.Fatal("an arm64 binary was accepted for an x86_64 package")
	}
	if !strings.Contains(requireArchitectureError.Error(), "arm64") {
		test.Fatalf("the refusal does not say what the binary was built for: %v", requireArchitectureError)
	}
}

// TestMinimumMacOsIsReadFromTheBinary uses one binary twice: as Go built it, which passes, and with its
// recorded minimum rewritten to macOS 27 by vtool, which is refused. The rewritten copy is the case the
// guard exists for, an engine built against a deployment target newer than the release promises.
func TestMinimumMacOsIsReadFromTheBinary(test *testing.T) {
	test.Parallel()

	binary := smallMacBinary(test, "arm64")
	if requireMinimumMacOsError := requireMinimumMacOs(
		binary,
		MinimumSwiftEngineMacOs,
	); requireMinimumMacOsError != nil {
		test.Fatalf("a binary Go built for an old macOS was refused: %v", requireMinimumMacOsError)
	}

	tooNew := filepath.Join(test.TempDir(), "too-new")
	rewrite := exec.Command(
		"vtool",
		"-set-build-version",
		"macos",
		"27.0",
		"27.0",
		"-replace",
		"-output",
		tooNew,
		binary,
	)
	if output, outputError := rewrite.CombinedOutput(); outputError != nil {
		test.Fatalf("rewriting the minimum macOS: %v\n%s", outputError, output)
	}

	requireMinimumMacOsError := requireMinimumMacOs(tooNew, MinimumSwiftEngineMacOs)
	if requireMinimumMacOsError == nil {
		test.Fatal("a binary requiring macOS 27 was accepted for a release promising macOS 15")
	}
	if !strings.Contains(requireMinimumMacOsError.Error(), "requires macOS 27.0") {
		test.Fatalf("the refusal does not name the minimum it found: %v", requireMinimumMacOsError)
	}
}

// TestMacOsVersionsCompareByNumber holds the comparison the guard rests on, including the case a string
// comparison gets wrong: 9 sorts after 15 as text and before it as a version.
func TestMacOsVersionsCompareByNumber(test *testing.T) {
	test.Parallel()

	cases := []struct {
		version string
		than    string
		newer   bool
	}{
		{"27.0", "15.0", true},
		{"15.0", "15.0", false},
		{"15", "15.0", false},
		{"15.1", "15.0", true},
		{"14.9", "15.0", false},
		{"9.0", "15.0", false},
		{"15.0.1", "15.0", false},
	}
	for _, testCase := range cases {
		newer, macOsVersionIsNewerError := macOsVersionIsNewer(testCase.version, testCase.than)
		if macOsVersionIsNewerError != nil {
			test.Fatalf("comparing %s with %s: %v", testCase.version, testCase.than, macOsVersionIsNewerError)
		}
		if newer != testCase.newer {
			test.Errorf("%s newer than %s: got %v, want %v", testCase.version, testCase.than, newer, testCase.newer)
		}
	}

	if _, macOsVersionIsNewerError := macOsVersionIsNewer("fifteen", "15.0"); macOsVersionIsNewerError == nil {
		test.Error("a version that is not a number was compared")
	}
}

// TestSwiftArchitectureNamesEveryMacTarget ties the Go-to-Swift architecture names to Targets, so a Mac
// target added later without a Swift name fails here rather than in a release.
func TestSwiftArchitectureNamesEveryMacTarget(test *testing.T) {
	test.Parallel()

	for _, target := range Targets {
		if !ShipsSwiftEngine(target.GoOperatingSystem) {
			continue
		}
		if _, swiftArchitectureError := swiftArchitecture(target.GoArchitecture); swiftArchitectureError != nil {
			test.Errorf("%s ships the Swift engine and has no Swift architecture: %v", target, swiftArchitectureError)
		}
	}
}

// contractPair writes a stand-in cohere whose `--version` prints cohereVersion, and a stand-in engine that
// runs engineScript with its arguments in "$@". Shell scripts rather than real builds, because what is
// under test is how the release reads the two answers, and a real engine takes fifteen minutes to build.
func contractPair(test *testing.T, cohereVersion string, engineScript string) (coherePath string, enginePath string) {
	test.Helper()

	directory := test.TempDir()
	coherePath = filepath.Join(directory, "cohere")
	enginePath = filepath.Join(directory, SwiftEngineFileName)
	writeExecutable(test, coherePath, 0o755)
	if writeError := executable_testing.WriteFile(
		coherePath,
		[]byte("#!/bin/sh\n[ \"$1\" = --version ] || exit 9\nprintf '%s\\n' '"+
			strings.ReplaceAll(cohereVersion, "\n", "' '")+
			"'\n"),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	writeExecutable(test, enginePath, 0o755)
	if writeError := executable_testing.WriteFile(
		enginePath,
		[]byte("#!/bin/sh\n"+engineScript+"\n"),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	return coherePath, enginePath
}

// stampedCommit is the commit the stand-in engines below say they were built from, and the one the
// release is told it stamped.
const stampedCommit = "0123456789abcdef0123456789abcdef01234567"

// speaksContract is an engine stamped with stampedCommit that answers `--contract <its> --version` with its
// provenance record and refuses any other contract the way the real one does: a sentence on stderr and
// exit 2.
func speaksContract(contract string) string {
	return `if [ "$1" != --contract ] || [ "$3" != --version ]; then exit 9; fi
if [ "$2" != ` + contract + ` ]; then ` +
		`echo "cohere-swift speaks contract ` + contract + `, and the front door asked for $2" >&2; exit 2; fi
echo '{"kind":"provenance","contract":` + contract +
		`,"engine":"cohere-swift","version":"0.1.0","commit":"` + stampedCommit + `"}'`
}

// TestSwiftContractAcceptsAMatchingPair is the positive half. Without it, a check that refused every pair
// would pass the controls below.
func TestSwiftContractAcceptsAMatchingPair(test *testing.T) {
	test.Parallel()

	coherePath, enginePath := contractPair(test, "cohere 1.0.0\n  swift contract: 3", speaksContract("3"))
	if requireSwiftContractError := requireSwiftContract(
		coherePath,
		enginePath,
		stampedCommit,
	); requireSwiftContractError != nil {
		test.Fatalf("a pair speaking the same contract was refused: %v", requireSwiftContractError)
	}
}

// TestSwiftContractRefusesAMismatchedPair holds each way a pair can disagree, and that each refusal says
// which.
func TestSwiftContractRefusesAMismatchedPair(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name          string
		cohereVersion string
		engineScript  string
		says          string
	}{
		{"the engine refuses the flag", "cohere 1.0.0\n  swift contract: 4", speaksContract("3"), "refused contract 4"},
		{"the engine accepts the flag and its record names another contract", "cohere 1.0.0\n  swift contract: 3",
			`echo '{"kind":"provenance","contract":2}'`, "speaks contract 2"},
		{"the engine answers with something that is not provenance", "cohere 1.0.0\n  swift contract: 3",
			`echo '{"kind":"summary","contract":3}'`, "not a provenance record"},
		{"the engine's record names no contract", "cohere 1.0.0\n  swift contract: 3",
			`echo '{"kind":"provenance"}'`, "not a provenance record"},
		{"cohere names no contract", "cohere 1.0.0", speaksContract("3"), "names no Swift contract"},
		{"the engine was not stamped", "cohere 1.0.0\n  swift contract: 3",
			`echo '{"kind":"provenance","contract":3,"commit":"dev"}'`, `built from "dev"`},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			coherePath, enginePath := contractPair(test, testCase.cohereVersion, testCase.engineScript)
			requireSwiftContractError := requireSwiftContract(coherePath, enginePath, stampedCommit)
			if requireSwiftContractError == nil {
				test.Fatal("a mismatched pair was staged")
			}
			if !strings.Contains(requireSwiftContractError.Error(), testCase.says) {
				test.Fatalf("refused, but without saying %q: %v", testCase.says, requireSwiftContractError)
			}
		})
	}
}

// TestSwiftEngineBuildReadsTheTreeAsItIsNow builds a stand-in engine through buildSwiftEngine twice in one
// repository: clean, then with a tracked file edited. It is laid out as cohere-swift is, a CohereSwift
// target with the stamp gitignored under Command/, its manifest defines a flag from
// `Context.gitInformation.hasUncommittedChanges` as cohere-swift's does, and the product prints that flag
// and the commit it was stamped with.
//
// SwiftPM's shared manifest cache would answer the second build from the first evaluation and stamp a
// modified tree as clean; the release turns that cache off. The first build also proves the stamp: it
// reaches the binary, it does not count as a modification, and it is gone afterwards.
func TestSwiftEngineBuildReadsTheTreeAsItIsNow(test *testing.T) {
	test.Parallel()

	if runtime.GOOS != "darwin" {
		test.Skip("NOT MEASURED: the Swift engine is only built on macOS")
	}
	if _, lookPathError := exec.LookPath("swift"); lookPathError != nil {
		test.Skip("NOT MEASURED: no swift toolchain on PATH")
	}

	module := test.TempDir()
	writeFile(test, filepath.Join(module, "swift", "Package.swift"), `// swift-tools-version:6.2
import PackageDescription
let modified = Context.gitInformation?.hasUncommittedChanges ?? true
let package = Package(name: "Stand", products: [.executable(name: "cohere-swift", targets: ["CohereSwift"])],
    targets: [.executableTarget(name: "CohereSwift", swiftSettings: modified ? [.define("TREE_MODIFIED")] : [])])
`)
	source := filepath.Join(module, "swift", "Sources", "CohereSwift", "main.swift")
	program := "#if TREE_MODIFIED\nlet tree = \"modified\"\n#else\nlet tree = \"clean\"\n#endif\n" +
		"#if COHERE_RELEASE_STAMP\nprint(tree, EngineReleaseStamp.commit)\n#else\nprint(tree, \"dev\")\n#endif\n"
	writeFile(test, source, program)
	writeFile(
		test,
		filepath.Join(module, "swift", "Sources", "CohereSwift", "Command", "Placeholder.swift"),
		"enum Placeholder {}\n",
	)
	writeFile(
		test,
		filepath.Join(module, "swift", ".gitignore"),
		"/Sources/CohereSwift/Command/"+SwiftEngineStampFileName+"\n",
	)
	writeFile(test, filepath.Join(module, ".gitignore"), ".scratch/\n")
	gitIn(test, module, "init", "--quiet")
	gitIn(test, module, "add", ".")
	gitIn(test, module, "commit", "--quiet", "-m", "stand-in engine")
	commit, readReleaseCommitError := readReleaseCommit(module)
	if readReleaseCommitError != nil {
		test.Fatal(readReleaseCommitError)
	}

	target := Target{GoOperatingSystem: "darwin", GoArchitecture: runtime.GOARCH}
	scratch := filepath.Join(module, ".scratch")
	says := func() string {
		test.Helper()
		destination := filepath.Join(test.TempDir(), SwiftEngineFileName)
		if buildSwiftEngineError := buildSwiftEngine(
			module,
			scratch,
			target,
			destination,
			commit,
		); buildSwiftEngineError != nil {
			test.Fatal(buildSwiftEngineError)
		}
		output, outputError := exec.Command(destination).Output()
		if outputError != nil {
			test.Fatal(outputError)
		}
		return strings.TrimSpace(string(output))
	}

	if got := says(); got != "clean "+commit {
		test.Fatalf("an engine built from a clean tree says %q, and should say clean and its commit %s", got, commit)
	}
	if _, statError := os.Stat(
		filepath.Join(module, "swift", "Sources", "CohereSwift", "Command", SwiftEngineStampFileName),
	); statError == nil {
		test.Fatal("the stamp was left in the checkout after the build")
	}
	writeFile(test, source, program+"// edited\n")
	if got := says(); got != "modified "+commit {
		test.Fatalf(
			"an engine built after a tracked file was edited says %q, so its provenance would call the tree clean",
			got,
		)
	}
}
