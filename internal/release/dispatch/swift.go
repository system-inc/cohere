package dispatch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/system-inc/cohere/internal/release/packaging"
)

// SwiftEngineOverrideVariable points cohere at a specific Swift engine binary, the counterpart of
// COHERE_BINARY for cohere itself.
//
// For whoever is developing the engine, and for running a known build while the sources on disk are
// mid-edit. An explicit binary wins over the rebuild for the reason resolveBinary gives: someone who
// names a binary has said what they want to run. It is announced on every run, because the findings
// then come from a binary no hash vouches for.
const SwiftEngineOverrideVariable = "COHERE_SWIFT_ENGINE"

// buildSwiftProduct builds the engine package at packageDirectory and copies the product to binaryPath,
// with environment added to the build's own. A development build states the tree's state there; a
// committed build states nothing, so its manifest reads git, and its proof checks what it read.
//
// A variable so a test can stand in for SwiftPM: a cold build compiles swift-syntax and swift-format,
// measured at 225s, and the decisions worth testing are which tree is built and when, not SwiftPM.
var buildSwiftProduct = buildSwiftProductWithSwiftPm

// ResolveSwiftEngine returns the path to a cohere-swift binary matching the engine's sources and the
// installed toolchain, building it when there is none.
//
// The same bargain Resolve makes for cohere, for the same reason: the rules are compiled in, so a
// stale binary runs old rules and looks exactly like a current one. The hash covers what decides the
// binary, which for Swift is the package's sources and manifests and the toolchain. The toolchain is
// in the hash because the engine links its libraries and reads its compiler's serialized diagnostics,
// so the same sources under a new Xcode are a different engine.
//
// The hash also holds the bit the engine reports as `sourceTreeModified`: whether its own inputs, the
// files swiftEngineInputs names, differ from the commit, tracked or untracked. Nothing else in the
// repository counts, so the bit and the hash cover the same files. It was once left out of the hash, on the
// argument that a stale bit errs toward the warning. It errs both ways. A clean tree was served an engine
// built while an unrelated file was dirty, which reported a modified build and was refused at land, and a
// dirty tree could be served one built clean, claiming a clean build that never happened (#50wtb2s). So the
// bit is read fresh on every resolve (workingTreeModified, 14 ms on cohere's checkout), named into the hash
// so each state has its own binary, and handed to the build, so the engine reports the state its name holds
// rather than whatever SwiftPM reads from git a moment later.
//
// That is the working-tree half. When commit is not empty the engine is built from that commit
// instead, which is what a gate gets: see resolveCommittedSwiftEngine. The contract is the one the
// caller speaks, used only to ask a freshly built committed engine for its provenance.
//
// versionOutput is what `swift --version` printed, as SwiftVersionOutput returns it. The caller asks, once per
// process tree, since every project's run and every engine needs the same answer (#nkk91tv).
//
// The returned boolean reports whether a build ran.
func ResolveSwiftEngine(paths Paths, commit string, contract int, versionOutput string) (string, bool, error) {
	toolchain, swiftToolchainError := swiftToolchain(versionOutput)
	if swiftToolchainError != nil {
		return "", false, swiftToolchainError
	}

	if commit != "" {
		return resolveCommittedSwiftEngine(paths, commit, toolchain, contract)
	}

	modified := SwiftEngineTreeModified(paths.ModuleDirectory)
	hash, swiftEngineHashError := swiftEngineHash(paths.SwiftEngineDirectory(), toolchain, modified)
	if swiftEngineHashError != nil {
		return "", false, swiftEngineHashError
	}

	binaryPath := paths.SwiftEngineBinaryPath(hash)
	if _, statError := os.Stat(binaryPath); statError == nil {
		return binaryPath, false, nil
	}

	if buildSwiftProductError := buildSwiftProduct(
		paths.SwiftEngineDirectory(),
		paths.SwiftBuildDirectory(),
		binaryPath,
		[]string{release.SwiftSourceTreeModifiedVariable + "=" + strconv.FormatBool(modified)},
	); buildSwiftProductError != nil {
		return "", false, buildSwiftProductError
	}
	return binaryPath, true, nil
}

// SwiftVersionOutput asks the installed Swift toolchain who it is, and returns what `swift --version` printed,
// unchanged.
//
// A missing toolchain is an error naming the command that failed, never a fallback. Without one the
// engine cannot be built and could not read the compiler's diagnostics if it were, so there is no
// engine to fall back to: only a run that checks nothing.
func SwiftVersionOutput() (string, error) {
	output, outputError := exec.Command("swift", "--version").Output()
	if outputError != nil {
		return "", fmt.Errorf(
			"no Swift toolchain answered `swift --version` (%w), so the Swift engine cannot be built and nothing was checked",
			outputError,
		)
	}
	return string(output), nil
}

// swiftToolchain identifies the installed Swift toolchain from what `swift --version` printed, its words
// joined by single spaces, so the same toolchain reads the same however its output was carried.
func swiftToolchain(versionOutput string) (string, error) {
	identity := strings.Join(strings.Fields(versionOutput), " ")
	if identity == "" {
		return "", fmt.Errorf(
			"`swift --version` printed nothing, so the toolchain the Swift engine would link against is unknown",
		)
	}
	return identity, nil
}

// swiftEngineHash digests every input that decides what the engine binary does.
//
// `Package.swift` and `Package.resolved` pin the package graph, and every file under `Sources/` is
// compiled in. Tests are left out because they are not in the binary, and `.build` because it is
// output. Contents are hashed, never times, for the reason Inputs.Compute gives. modified is the tree's
// state the build is handed, which the engine compiles in and reports (see ResolveSwiftEngine).
func swiftEngineHash(engineDirectory string, toolchain string, modified bool) (string, error) {
	files := []string{
		filepath.Join(engineDirectory, "Package.swift"),
		filepath.Join(engineDirectory, "Package.resolved"),
	}

	sources := filepath.Join(engineDirectory, "Sources")
	walkError := filepath.WalkDir(sources, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if walkError != nil {
		return "", fmt.Errorf("listing the Swift engine's sources under %s: %w", sources, walkError)
	}
	if len(files) == 2 {
		// The same guard CollectInputs keeps: an empty source set hashes to a stable digest for a tree
		// holding nothing, and every later run would hit it.
		return "", fmt.Errorf("found no Swift engine sources under %s, which cannot be right", sources)
	}
	sort.Strings(files)

	digest := sha256.New()
	fmt.Fprintf(digest, "toolchain\x00%s\x00", toolchain)
	fmt.Fprintf(digest, "platform\x00%s/%s\x00", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(digest, "sourceTreeModified\x00%t\x00", modified)
	for _, path := range files {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return "", fmt.Errorf("reading Swift engine input %s: %w", path, readError)
		}
		// The path relative to the package, rather than the base name Inputs.Compute uses: two files
		// named `Rule.swift` in different directories are different inputs, and moving one between
		// targets changes the binary.
		relative, relativePathError := filepath.Rel(engineDirectory, path)
		if relativePathError != nil {
			return "", relativePathError
		}
		fmt.Fprintf(digest, "file\x00%s\x00%d\x00", filepath.ToSlash(relative), len(contents))
		digest.Write(contents)
	}
	return hex.EncodeToString(digest.Sum(nil))[:hashLength], nil
}

// lastLines is a tool's kept output for an error: its last count lines, on lines of their own after the
// error's, or nothing when it printed nothing.
func lastLines(output string, count int) string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return "\n" + strings.Join(lines, "\n")
}

// buildToolGrace is how long a build tool's group has to end after a terminate before it is killed.
const buildToolGrace = 5 * time.Second

// buildToolWaitDelay bounds how long Wait waits on a tool's output pipes once the tool has ended, so a
// compiler that left the group while holding a pipe cannot hold this process with it.
const buildToolWaitDelay = 2 * time.Second

// runBuildTool runs a build tool as the leader of a process group of its own, and ends the whole group if
// this process is interrupted, terminated or hung up while the tool runs.
//
// Without it a signal ended this process alone and left `swift build` and its compilers running, with
// nobody to read their result, and holding SwiftPM's lock on the scratch path so the next run's build waited
// behind them (#nqb3mjv). A kill this process cannot catch still leaves them: nothing can forward that.
func runBuildTool(command *exec.Cmd) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	return runBuildToolUntil(command, signals)
}

// runBuildToolUntil is runBuildTool with the signals given, so a test can send one without signaling
// itself.
//
// Mutates: command its process group and wait delay are set here, before it starts, so the caller hands it
// over unstarted
func runBuildToolUntil(command *exec.Cmd, signals <-chan os.Signal) error {
	startInOwnGroup(command)
	command.WaitDelay = buildToolWaitDelay
	if startError := command.Start(); startError != nil {
		return startError
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	select {
	case waitError := <-done:
		return waitError
	case received := <-signals:
		signalGroup(command, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(buildToolGrace):
			signalGroup(command, syscall.SIGKILL)
			<-done
		}
		return fmt.Errorf("stopped by %v, and everything it started was ended with it", received)
	}
}

// buildSwiftProductWithSwiftPm builds the engine in release mode and copies the product to binaryPath.
//
// A cold build compiles swift-syntax and swift-format and takes minutes, so it announces itself on
// stderr before starting, whatever the verbosity: a command that is silent for three minutes reads as
// hung. SwiftPM's progress goes to stderr too, because this process's stdout is the report.
//
// Copied rather than linked to SwiftPM's product, because the next build replaces the product in place,
// and a link would turn every hash-named binary into whichever engine was built last. The copy is
// installed without replacing a binary already at binaryPath, for the reason copyExecutable gives.
func buildSwiftProductWithSwiftPm(
	packageDirectory string,
	scratchDirectory string,
	binaryPath string,
	environment []string,
) error {
	if createDirectoryError := os.MkdirAll(filepath.Dir(binaryPath), 0o755); createDirectoryError != nil {
		return fmt.Errorf("creating the binary cache directory: %w", createDirectoryError)
	}

	Report.Step(
		"cohere: building the Swift engine from %s (its sources or the toolchain changed; a cold build takes minutes)",
		packageDirectory,
	)

	// `--manifest-cache none`, because a committed build's manifest reads the tree's git state into the
	// engine's `sourceTreeModified`, and SwiftPM's shared manifest cache answers it from an earlier
	// evaluation of the same commit, whatever the scratch path. An engine rebuilt after an edit to a tracked
	// file would then say it was built from a clean tree. Measured by @system_cohere_release, whose release
	// build passes the same flag. A development build states the bit in its environment, which the cache
	// does key on (measured 2026-11-12), so it is kept for the committed build; it costs about 0.4 s of
	// manifest evaluation per build, measured on a stand-in package.
	arguments := []string{
		"build", "-c", "release",
		"--package-path", packageDirectory,
		"--scratch-path", scratchDirectory,
		"--manifest-cache", "none",
		"--product", "cohere-swift",
	}
	// SwiftPM's own lines stream under --verbose. Otherwise they are kept, so a default run prints cohere's
	// report alone (#ytqqv8v), and a build that fails quotes their end in its error.
	// The tree's state comes from the caller alone. One inherited from this process's environment would be a
	// stated answer nobody here computed.
	inherited := release.WithoutVariable(os.Environ(), release.SwiftSourceTreeModifiedVariable)
	buildEnvironment := append(inherited, environment...)
	build := exec.Command("swift", arguments...)
	build.Env = buildEnvironment
	var kept bytes.Buffer
	build.Stdout, build.Stderr = &kept, &kept
	if stream := Report.ToolOutput(); stream != nil {
		build.Stdout, build.Stderr = stream, stream
	}
	if runBuildToolError := runBuildTool(build); runBuildToolError != nil {
		return fmt.Errorf(
			"building the Swift engine (swift %s): %w%s",
			strings.Join(arguments, " "),
			runBuildToolError,
			lastLines(kept.String(), 40),
		)
	}

	showBinPath := exec.Command("swift", append(arguments, "--show-bin-path")...)
	showBinPath.Env = buildEnvironment
	binPath, outputError := showBinPath.Output()
	if outputError != nil {
		return fmt.Errorf("asking SwiftPM where it put the Swift engine: %w", outputError)
	}
	product := filepath.Join(strings.TrimSpace(string(binPath)), "cohere-swift")

	// The build said it worked; the file is what gets run. The same gap build() closes for cohere.
	if copyExecutableError := copyExecutable(product, binaryPath); copyExecutableError != nil {
		return fmt.Errorf(
			"swift build reported success but the engine could not be taken from %s: %w",
			product,
			copyExecutableError,
		)
	}
	return nil
}

// copyExecutable copies a file to a new path through a temporary name in the same directory, so a
// concurrent run never sees, and execs, a half-written binary.
//
// It never replaces a file already at destination. The temporary file is hard-linked into place, which
// fails atomically when the name exists, and a binary already there is kept. Two processes can both find
// the engine missing and both build it; with a rename the second install replaced the first one's binary
// under a run already reading it, and that run, seeing an input change, refused its record (#p5ndwqa).
// Keeping the first is safe because the name is swiftEngineHash's digest of everything that decides the
// binary, so whatever is already there is the same engine.
func copyExecutable(source string, destination string) error {
	input, openError := os.Open(source)
	if openError != nil {
		return openError
	}
	defer input.Close() // Ignored: the source was only read, so a failed close loses nothing.

	temporary, temporaryFileError := os.CreateTemp(filepath.Dir(destination), filepath.Base(destination)+".partial-*")
	if temporaryFileError != nil {
		return temporaryFileError
	}
	// Removed on every path below: once linked, the destination holds its own name for the same file, and
	// otherwise the partial file is left over.
	// Ignored: on a failure path the error returned is the one that says what went wrong, and a leftover
	// partial file is never read, only its destination.
	defer os.Remove(temporary.Name())

	if _, copyError := io.Copy(temporary, input); copyError != nil {
		// Ignored: the copy failed and its error is the one returned, and the partial file is removed above.
		temporary.Close()
		return copyError
	}
	if chmodError := temporary.Chmod(0o755); chmodError != nil {
		// Ignored: the chmod failed and its error is the one returned, and the partial file is removed above.
		temporary.Close()
		return chmodError
	}
	if closeError := temporary.Close(); closeError != nil {
		return closeError
	}
	linkError := os.Link(temporary.Name(), destination)
	if errors.Is(linkError, fs.ErrExist) {
		// Another process installed the same engine first, and a run may already be reading it.
		return nil
	}
	return linkError
}
