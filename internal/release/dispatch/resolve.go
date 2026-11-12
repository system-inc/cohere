package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ReleaseBuildFlags strip the symbol table and `DWARF` from the binary.
//
// Measured on typescript-go, a comparable binary: these make the link marginally faster (0.92s
// against 1.06s) and cut the binary by 29%, so there is no tradeoff to weigh. They are fixed rather
// than optional because `-trimpath` is a build-input change, not a link flag: turning it on
// invalidated a fully warm 2.0 GB compile cache and cost 33s to repopulate. Flipping it per-run
// would pay that repeatedly, so the flags are constant for every build this package performs.
//
// The builds share the Go cache every other `go` command on the machine uses rather than keeping one of
// their own (#3kr3x59): the house sets `GOFLAGS=-trimpath`, so a member's `go build` and `go test` compile
// the same packages the launcher does, and the launcher's builds reuse them. A private cache held 40 GB,
// largely the same packages stored twice.
var ReleaseBuildFlags = []string{"-trimpath", "-ldflags=-s -w"}

// DefaultPaths puts the cache at `.cache/cohere/` inside the module, which the repo already
// ignores.
func DefaultPaths(moduleDirectory string) Paths {
	return Paths{
		ModuleDirectory: moduleDirectory,
		CacheDirectory:  filepath.Join(moduleDirectory, ".cache", "cohere"),
	}
}

// executableSuffix ends every binary the launcher builds, `.exe` on Windows and nothing elsewhere.
// Windows runs a file by its extension, and Go's exec there will not run a path that has none.
var executableSuffix = func() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}()

// developmentBinaryName is the `--dev` binary's file name.
var developmentBinaryName = "cohere-dev" + executableSuffix

// ResolveWorkingTree returns the `--dev` binary, built from the working tree as it is on disk.
//
// This is the explicit way to run uncommitted work, and only that. The binary holds every member's
// uncommitted edits in the shared checkout, not just the caller's, so it is never what a gate runs:
// ResolveCommitted is. The caller says so on every run.
//
// The returned boolean reports whether a build ran, so a caller can tell the user why a normally
// instant command took a second.
func ResolveWorkingTree(paths Paths, packagePath string) (string, bool, error) {
	inputs, collectInputsError := CollectInputs(paths.ModuleDirectory, packagePath, ReleaseBuildFlags)
	if collectInputsError != nil {
		return "", false, collectInputsError
	}

	hash, computeError := inputs.Compute()
	if computeError != nil {
		return "", false, computeError
	}

	// The binary name never changes, so its existence says nothing about whether it is current. The
	// hash is recorded beside it and compared instead. Skipping this would serve a stale binary from
	// a stable path, which is the same lie as a wrong hash.
	binaryPath := paths.DevelopmentBinaryPath()
	if upToDate, developmentBinaryIsCurrentError := developmentBinaryIsCurrent(
		paths,
		hash,
	); developmentBinaryIsCurrentError != nil {
		return "", false, developmentBinaryIsCurrentError
	} else if upToDate {
		return binaryPath, false, nil
	}

	if buildError := build(paths, packagePath, binaryPath, inputs); buildError != nil {
		return "", false, buildError
	}
	if recordDevelopmentHashError := recordDevelopmentHash(paths, hash); recordDevelopmentHashError != nil {
		return "", false, recordDevelopmentHashError
	}

	return binaryPath, true, nil
}

// developmentBinaryIsCurrent reports whether the stable-path binary matches the current inputs.
func developmentBinaryIsCurrent(paths Paths, hash string) (bool, error) {
	if _, statError := os.Stat(paths.DevelopmentBinaryPath()); statError != nil {
		return false, nil
	}

	recorded, readError := os.ReadFile(paths.developmentHashPath())
	if readError != nil {
		// A binary with no recorded hash is of unknown provenance. Rebuilding costs a second;
		// trusting it could run rules that are not the ones on disk.
		return false, nil
	}

	return strings.TrimSpace(string(recorded)) == hash, nil
}

// recordDevelopmentHash stores the hash the development binary was built from.
//
// This is one of the launcher's three writes, with the binary and the Go build cache beside it, and
// all three land under Paths.CacheDirectory: `.cache/cohere/` in the cohere checkout, which that
// checkout's .gitignore excludes. None of them is relative to the working directory or to the project
// being checked, so `cohere --no-fix` keeps its promise about the project while this still runs. The
// promise is about the tree cohere checks, not about cohere's own build, and a rebuild has to be
// allowed under `--no-fix` or the flag would run a binary that does not match the rules on disk.
//
// The checkout is found from the launcher's own resolved path before the working directory (see
// FindModuleDirectory in module.go), so an installed launcher writes into the checkout
// it lives in wherever it is run from. The one arrangement where these bytes sit inside a checked
// project is a cohere checkout placed inside that project's tree, and then they are still that
// checkout's ignored cache rather than the project's files.
func recordDevelopmentHash(paths Paths, hash string) error {
	if writeError := os.WriteFile(paths.developmentHashPath(), []byte(hash+"\n"), 0o644); writeError != nil {
		return fmt.Errorf("recording the development build hash: %w", writeError)
	}
	return nil
}

// build compiles the binary to binaryPath, stamped with the formatter's identity as a committed build is, so
// a `--dev` run reads and keeps the same format record. It builds through the Go cache every other `go`
// command uses (see ReleaseBuildFlags).
//
// VCS stamping is off, and the tree's state is stamped from inputs instead: the commit, whether anything
// compiled in is uncommitted, and the compiler's commit, every one of them held by the hash. Go's own stamp was read at link time, a moment
// apart from the hash that names the binary, and was left out of that hash, so a binary built in one state
// was served to a tree in another (#50wtb2s). Stamped from inputs, it reports exactly what its name holds.
// Turning stamping off also lets `--dev` build in a worktree whose TypeScript is a symlink, where Go's own
// git status fails.
func build(paths Paths, packagePath string, binaryPath string, inputs Inputs) error {
	if createDirectoryError := os.MkdirAll(paths.BinaryDirectory(), 0o755); createDirectoryError != nil {
		return fmt.Errorf("creating the binary cache directory: %w", createDirectoryError)
	}

	const packagingPath = "github.com/system-inc/cohere/internal/release/packaging"
	formatter, formatterIdentityError := FormatterIdentity(paths.ModuleDirectory, inputs.GoVersion, nil)
	if formatterIdentityError != nil {
		return formatterIdentityError
	}
	stamps := []string{
		"-X", packagingPath + ".formatterIdentity=" + formatter,
		"-X", packagingPath + ".sourceTreeModified=" + strconv.FormatBool(inputs.SourceTreeModified),
		"-X", packagingPath + ".compilerCommit=" + inputs.TypeScriptGoCommit,
	}
	if inputs.SelfCommit != "" {
		stamps = append(stamps, "-X", packagingPath+".selfCommit="+inputs.SelfCommit)
	}
	stamp := strings.Join(stamps, " ")
	arguments := []string{"build", "-buildvcs=false"}
	stamped := false
	for _, flag := range ReleaseBuildFlags {
		// Joined onto the release flags' own `-ldflags`, since Go keeps only the last one given.
		if value, isLinkerFlags := strings.CutPrefix(flag, "-ldflags="); isLinkerFlags {
			flag = "-ldflags=" + value + " " + stamp
			stamped = true
		}
		arguments = append(arguments, flag)
	}
	if !stamped {
		arguments = append(arguments, "-ldflags="+stamp)
	}
	arguments = append(arguments, "-o", binaryPath, packagePath)

	command := exec.Command("go", arguments...)
	command.Dir = paths.ModuleDirectory
	command.Env = append(os.Environ(),
		// Nothing here needs cgo, and disabling it keeps the build from depending on a C toolchain
		// that a fresh machine may not have. It costs the shared cache little: only packages that use
		// cgo compile differently, measured at 46 MB beside a plain build's 3.2 GB (#3kr3x59).
		"CGO_ENABLED=0",
	)
	command.Stderr = os.Stderr

	if runError := command.Run(); runError != nil {
		return fmt.Errorf("building cohere: %w", runError)
	}

	// Go reports success by exit code, but the thing we are about to exec is the file. Checking it
	// exists closes the gap between "the build said it worked" and "there is a binary here", which
	// is exactly the kind of gap that lets a missing binary reach an exec.
	if _, statError := os.Stat(binaryPath); statError != nil {
		return fmt.Errorf("go build reported success but produced no binary at %s: %w", binaryPath, statError)
	}
	return nil
}
