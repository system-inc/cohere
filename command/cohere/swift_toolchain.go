package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/release/dispatch"
	"github.com/system-inc/cohere/internal/release/packaging"
)

// The Swift toolchain and engine are asked once per process tree, not once per process (#nkk91tv). A run of
// ahraos-presence asked `swift --version` 23 times: the front door once in itself and once in every project's
// run, each also hashing the engine's sources again, and every engine asked for itself. So the first cohere
// process to need them asks, and publishes the answers in its environment, where every process it starts
// inherits them: project runs, coverage queries and engines alike.
//
// The answers feed cache keys: the toolchain is in the engine's hash and in the engine's build-input snapshot.
// A value left in a shell from another toolchain would let a run replay results compiled under that one, so an
// inherited value is trusted only when this process's parent published it: swiftResolvedByVariable names the
// process that did, and only a process whose parent that is keeps the values. Every other process, a cohere run
// from a shell above all, drops them before it does anything (see trustInheritedSwiftResolution).

// swiftVersionOutputVariable carries what `swift --version` printed, unchanged. The engine reads it too.
const swiftVersionOutputVariable = "COHERE_SWIFT_VERSION_OUTPUT"

// swiftExecutableVariable carries the `swift` that `xcrun --find swift` names. The engine reads it too.
const swiftExecutableVariable = "COHERE_SWIFT_EXECUTABLE"

// swiftEnginePathVariable carries the engine binary the tree resolved, so a project's run neither hashes the
// engine's sources nor builds it again.
const swiftEnginePathVariable = "COHERE_SWIFT_ENGINE_PATH"

// swiftEngineCommitVariable carries the commit that engine is guaranteed to match, or nothing. See
// resolveSwiftEngineBinary.
const swiftEngineCommitVariable = "COHERE_SWIFT_ENGINE_COMMIT"

// swiftResolvedByVariable names the process that published the values above, by its pid.
const swiftResolvedByVariable = "COHERE_SWIFT_RESOLVED_BY"

// swiftResolutionVariables are every value a process tree shares about Swift, dropped together when untrusted.
var swiftResolutionVariables = []string{
	swiftVersionOutputVariable,
	swiftExecutableVariable,
	swiftEnginePathVariable,
	swiftEngineCommitVariable,
	swiftResolvedByVariable,
}

// trustInheritedSwiftResolution keeps the inherited Swift values when this process's parent published them, and
// drops them otherwise, before anything reads them or starts a process that would inherit them. A kept value is
// vouched for again in this process's name, so a process it starts trusts it too.
func trustInheritedSwiftResolution() {
	if resolvedBy := os.Getenv(swiftResolvedByVariable); resolvedBy != "" && resolvedBy == strconv.Itoa(os.Getppid()) {
		vouchForSwiftResolution()
		return
	}
	for _, name := range swiftResolutionVariables {
		// Ignored: Unsetenv fails only on a name with a zero byte or an equals sign, and these hold neither.
		_ = os.Unsetenv(name)
	}
}

// vouchForSwiftResolution marks this process as the one whose children may trust the values.
func vouchForSwiftResolution() {
	// Ignored: Setenv fails only on a name or value with a zero byte, and a pid holds none.
	_ = os.Setenv(swiftResolvedByVariable, strconv.Itoa(os.Getpid()))
}

// publishSwiftResolution sets one shared value, for every process this one starts.
func publishSwiftResolution(name string, value string) {
	// Ignored: Setenv fails only on a zero byte, which neither a command's output nor a path carries.
	_ = os.Setenv(name, value)
	vouchForSwiftResolution()
}

// swiftVersionOutput is what `swift --version` printed, asked once per process tree: inherited when a parent
// asked, and otherwise asked here and published with the `swift` xcrun names, since every engine wants both.
var swiftVersionOutput = sync.OnceValues(func() (string, error) {
	if inherited := os.Getenv(swiftVersionOutputVariable); inherited != "" {
		return inherited, nil
	}
	output, swiftVersionOutputError := dispatch.SwiftVersionOutput()
	if swiftVersionOutputError != nil {
		return "", swiftVersionOutputError
	}
	publishSwiftResolution(swiftVersionOutputVariable, output)
	// Only published when xcrun answers with a path: an engine without it asks xcrun itself, as it always did.
	if found, findError := exec.Command("xcrun", "--find", "swift").Output(); findError == nil {
		if executable := strings.TrimSpace(string(found)); filepath.IsAbs(executable) {
			publishSwiftResolution(swiftExecutableVariable, executable)
		}
	}
	return output, nil
})

// resolvedSwiftEngineBinary is resolveSwiftEngineBinary once per process tree: the engine a parent resolved
// when it published one, and otherwise resolved here and published. An engine the caller named with
// COHERE_SWIFT_ENGINE is never published, so each run goes on saying it runs a binary no hash vouches for.
func resolvedSwiftEngineBinary(location projectLocation) (string, string, error) {
	if inherited := os.Getenv(swiftEnginePathVariable); inherited != "" && isRegularFile(inherited) {
		return inherited, os.Getenv(swiftEngineCommitVariable), nil
	}
	binaryPath, commit, resolveSwiftEngineBinaryError := resolveSwiftEngineBinary(location, release.Current())
	if resolveSwiftEngineBinaryError != nil {
		return "", "", resolveSwiftEngineBinaryError
	}
	if os.Getenv(dispatch.SwiftEngineOverrideVariable) == "" {
		publishSwiftResolution(swiftEngineCommitVariable, commit)
		publishSwiftResolution(swiftEnginePathVariable, binaryPath)
	}
	return binaryPath, commit, nil
}
