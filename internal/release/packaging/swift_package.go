package release

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// MinimumSwiftEngineMacOs is the oldest macOS a released cohere-swift may require.
//
// The engine's own floor is macOS 15, set by Synchronization's Mutex, and @system_cohere_swift
// measured it. The release reads the minimum from the built binary itself and refuses anything above
// this. Otherwise a Package.swift that drifted back to a newer deployment target would ship an engine
// that will not even launch on most of the Macs people have, and nothing would say so until a user
// ran it.
const MinimumSwiftEngineMacOs = "15.0"

// SwiftSourceTreeModifiedVariable states the engine's `sourceTreeModified` to its manifest, "true" or
// "false", in place of the manifest reading git (swift/Package.swift).
//
// The development launcher states it, because it names the engine it caches by the same answer, read
// once, so the engine it serves reports the state its name holds (#50wtb2s). A release states nothing:
// its manifest reads git, and an inherited value is taken out of its environment.
const SwiftSourceTreeModifiedVariable = "COHERE_SWIFT_SOURCE_TREE_MODIFIED"

// WithoutVariable is environment with every entry for name taken out.
func WithoutVariable(environment []string, name string) []string {
	kept := make([]string, 0, len(environment))
	for _, variable := range environment {
		if variableName, _, _ := strings.Cut(variable, "="); variableName != name {
			kept = append(kept, variable)
		}
	}
	return kept
}

// swiftArchitecture is Swift's name for a Go architecture, which is what `swift build --arch` takes.
func swiftArchitecture(goArchitecture string) (string, error) {
	switch goArchitecture {
	case "arm64":
		return "arm64", nil
	case "amd64":
		return "x86_64", nil
	}
	return "", fmt.Errorf("no Swift architecture is known for GOARCH %s", goArchitecture)
}

// buildSwiftEngine builds cohere-swift for target into destination, and refuses a product that is not
// the executable it should be.
//
// It resolves first and then builds with automatic resolution off, so the engine is built from exactly
// the dependency versions Package.resolved records. It asks SwiftPM where the product went rather than
// assuming a path, because Swift 6.3 and 6.4 put it in different places.
//
// The engine names commit as what it was built from. Swift has no `-ldflags -X`, so the commit goes
// into EngineReleaseStamp.generated.swift beside EngineVersion, which reads it only under
// COHERE_RELEASE_STAMP, and the file is removed after the build. It is gitignored, so writing it
// leaves the manifest's `hasUncommittedChanges` as it was (measured by @system_cohere_swift and here).
// The build runs in the checkout rather than an export because outside git the manifest reads no
// git information and calls the tree modified.
func buildSwiftEngine(
	moduleDirectory string,
	scratchRoot string,
	target Target,
	destination string,
	commit string,
) error {
	architecture, swiftArchitectureError := swiftArchitecture(target.GoArchitecture)
	if swiftArchitectureError != nil {
		return swiftArchitectureError
	}

	packageDirectory := filepath.Join(moduleDirectory, "swift")
	// The manifest evaluated now, never a cached evaluation. The manifest reads
	// `Context.gitInformation.hasUncommittedChanges` into the engine's provenance, and SwiftPM's shared
	// manifest cache answers from an earlier evaluation of the same commit. Measured: a tracked file
	// edited after a clean build still built an engine that said its tree was clean, and with the
	// cache off the same build said modified.
	location := []string{
		"--package-path",
		packageDirectory,
		"--scratch-path",
		filepath.Join(scratchRoot, architecture),
		"--manifest-cache",
		"none",
	}

	stamp := filepath.Join(packageDirectory, "Sources", "CohereSwift", "Command", SwiftEngineStampFileName)
	stampSource := "#if COHERE_RELEASE_STAMP\n    enum EngineReleaseStamp { static let commit = \"" +
		commit +
		"\" }\n#endif\n"
	if writeError := os.WriteFile(stamp, []byte(stampSource), 0o644); writeError != nil {
		return fmt.Errorf("writing the Swift engine's commit stamp: %w", writeError)
	}
	// Ignored: the stamp is gitignored and read only under COHERE_RELEASE_STAMP, which only a release build sets,
	// and every release writes it again before building, so one left behind changes no release.
	defer os.Remove(stamp)

	resolve := exec.Command("swift", append([]string{"package", "resolve"}, location...)...)
	resolve.Stdout = os.Stderr
	resolve.Stderr = os.Stderr
	if runError := resolve.Run(); runError != nil {
		return fmt.Errorf("resolving the Swift engine's dependencies: %w", runError)
	}

	arguments := append([]string{"build", "-c", "release", "--arch", architecture, "--product", SwiftEngineFileName,
		"--disable-automatic-resolution", "-Xswiftc", "-DCOHERE_RELEASE_STAMP"}, location...)
	build := exec.Command("swift", arguments...)
	// The manifest reads git for a release. A stated answer inherited from whoever ran the release would be
	// compiled into the engine instead.
	build.Env = WithoutVariable(os.Environ(), SwiftSourceTreeModifiedVariable)
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if runError := build.Run(); runError != nil {
		return fmt.Errorf(
			"building the Swift engine for %s (swift %s): %w",
			target,
			strings.Join(arguments, " "),
			runError,
		)
	}

	binPath, outputError := exec.Command("swift", append(arguments, "--show-bin-path")...).Output()
	if outputError != nil {
		return fmt.Errorf("asking SwiftPM where it put the Swift engine for %s: %w", target, outputError)
	}
	product := filepath.Join(strings.TrimSpace(string(binPath)), SwiftEngineFileName)

	contents, readError := os.ReadFile(product)
	if readError != nil {
		return fmt.Errorf("swift build reported success but left no engine at %s: %w", product, readError)
	}
	if writeError := os.WriteFile(destination, contents, 0o755); writeError != nil {
		return fmt.Errorf("staging the Swift engine: %w", writeError)
	}

	if requireExecutableFormatError := requireExecutableFormat(
		destination,
		target,
	); requireExecutableFormatError != nil {
		return requireExecutableFormatError
	}
	if requireArchitectureError := requireArchitecture(destination, architecture); requireArchitectureError != nil {
		return requireArchitectureError
	}
	return requireMinimumMacOs(destination, MinimumSwiftEngineMacOs)
}

// requireSwiftContract refuses a package whose cohere and cohere-swift would refuse each other.
//
// Each side refuses a version it does not speak, so a mismatched pair fails on its first Swift run,
// which on a released package is a user's first Swift run. The contract is read from the staged cohere's
// `--version` rather than from a constant here, because a copy of the number is a third version that can
// disagree with both binaries while agreeing with the test that checks it. The engine is asked with the
// same `--contract` flag the front door passes, and its record is checked too, because the contract
// counts the flag and the record as two independent ways for the versions to disagree.
//
// Both binaries are run, so the darwin-amd64 package is checked under Rosetta on Apple silicon. A host
// that cannot run one fails here by name rather than skipping the check.
//
// The record's commit must be the one the engine was stamped with, so an engine built without the
// stamp, which says `dev`, is never staged.
func requireSwiftContract(coherePath string, enginePath string, commit string) error {
	var cohereError bytes.Buffer
	versionCommand := exec.Command(coherePath, "--version")
	versionCommand.Stderr = &cohereError
	versionOutput, outputError := versionCommand.Output()
	if outputError != nil {
		return fmt.Errorf(
			"running the staged %s --version to read its Swift contract: %w: %s",
			coherePath,
			outputError,
			strings.TrimSpace(cohereError.String()),
		)
	}

	contract := ""
	for _, line := range strings.Split(string(versionOutput), "\n") {
		if after, found := strings.CutPrefix(strings.TrimSpace(line), "swift contract:"); found {
			contract = strings.TrimSpace(after)
		}
	}
	expected, parseError := strconv.Atoi(contract)
	if parseError != nil {
		return fmt.Errorf(
			"the staged %s names no Swift contract in --version (it printed %q), so whether %s speaks it cannot be checked",
			coherePath,
			strings.TrimSpace(string(versionOutput)),
			SwiftEngineFileName,
		)
	}

	var engineError bytes.Buffer
	engineCommand := exec.Command(enginePath, "--contract", contract, "--version")
	engineCommand.Stderr = &engineError
	engineOutput, outputError := engineCommand.Output()
	if outputError != nil {
		return fmt.Errorf("the staged %s refused contract %d, which the cohere beside it speaks: %w: %s",
			enginePath, expected, outputError, strings.TrimSpace(engineError.String()))
	}

	firstLine, _, _ := strings.Cut(strings.TrimSpace(string(engineOutput)), "\n")
	var record struct {
		Kind     string `json:"kind"`
		Contract *int   `json:"contract"`
		Commit   string `json:"commit"`
	}
	if decodeError := json.Unmarshal(
		[]byte(firstLine),
		&record,
	); decodeError != nil || record.Kind != "provenance" || record.Contract == nil {
		return fmt.Errorf(
			"the staged %s answered --version with %q, not a provenance record naming its contract",
			enginePath,
			firstLine,
		)
	}
	if *record.Contract != expected {
		return fmt.Errorf(
			"the staged %s speaks contract %d, and the cohere beside it speaks contract %d",
			enginePath,
			*record.Contract,
			expected,
		)
	}
	if record.Commit != commit {
		return fmt.Errorf(
			"the staged %s says it was built from %q, and this release builds commit %s",
			enginePath,
			record.Commit,
			commit,
		)
	}
	return nil
}

// readReleaseCommit is the full commit the release builds from, which the Swift engine is stamped with.
func readReleaseCommit(moduleDirectory string) (string, error) {
	output, outputError := exec.Command("git", "-C", moduleDirectory, "rev-parse", "HEAD").Output()
	if outputError != nil {
		return "", fmt.Errorf("reading the commit this release builds from: %w", outputError)
	}
	commit := strings.TrimSpace(string(output))
	if len(commit) != 40 || strings.Trim(commit, "0123456789abcdef") != "" {
		return "", fmt.Errorf("the commit this release builds from came back as %q, not a 40-hex commit", commit)
	}
	return commit, nil
}

// requireArchitecture refuses a binary that is not built for exactly architecture.
//
// The Go binary gets its architecture from `GOARCH` and cannot be wrong without failing to build. The
// Swift engine gets its from a separate `--arch` flag, so a package could carry an engine for the other
// Mac, and on Apple silicon Rosetta would hide it.
func requireArchitecture(path string, architecture string) error {
	output, outputError := exec.Command("lipo", "-archs", path).Output()
	if outputError != nil {
		return fmt.Errorf("reading the architectures of %s: %w", path, outputError)
	}
	if found := strings.TrimSpace(string(output)); found != architecture {
		return fmt.Errorf("%s is built for %q, and its package is for %s", path, found, architecture)
	}
	return nil
}

// requireMinimumMacOs refuses a binary whose minimum macOS, as recorded in the binary, is newer than
// allowed.
//
// It reads `minos` from the load commands with vtool, which is what the loader enforces, rather than
// trusting Package.swift to say what it built.
func requireMinimumMacOs(path string, allowed string) error {
	output, outputError := exec.Command("vtool", "-show-build", path).Output()
	if outputError != nil {
		return fmt.Errorf("reading the minimum macOS of %s: %w", path, outputError)
	}

	minimum := ""
	for _, line := range strings.Split(string(output), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "minos" {
			minimum = fields[1]
		}
	}
	if minimum == "" {
		return fmt.Errorf(
			"%s records no minimum macOS, so whether it launches on macOS %s cannot be known",
			path,
			allowed,
		)
	}

	newer, macOsVersionIsNewerError := macOsVersionIsNewer(minimum, allowed)
	if macOsVersionIsNewerError != nil {
		return macOsVersionIsNewerError
	}
	if newer {
		return fmt.Errorf(
			"%s requires macOS %s, and a released engine must launch on macOS %s; lower the deployment target in swift/Package.swift",
			path,
			minimum,
			allowed,
		)
	}
	return nil
}

// macOsVersionIsNewer reports whether version is newer than than, comparing major then minor.
func macOsVersionIsNewer(version string, than string) (bool, error) {
	// A missing minor reads as 0, so "15" and "15.0" compare equal; a patch is ignored.
	parse := func(text string) ([2]int, error) {
		var parts [2]int
		fields := strings.Split(text, ".")
		for index := 0; index < len(fields) && index < 2; index++ {
			number, parseError := strconv.Atoi(fields[index])
			if parseError != nil {
				return parts, fmt.Errorf("%q is not a macOS version", text)
			}
			parts[index] = number
		}
		return parts, nil
	}

	left, parseError := parse(version)
	if parseError != nil {
		return false, parseError
	}
	right, parseError := parse(than)
	if parseError != nil {
		return false, parseError
	}
	if left[0] != right[0] {
		return left[0] > right[0], nil
	}
	return left[1] > right[1], nil
}
