package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ReleaseBuildFlags strip the symbol table and DWARF from the binary.
//
// Measured on typescript-go, a comparable binary: these make the link marginally faster (0.92s
// against 1.06s) and cut the binary by 29%, so there is no tradeoff to weigh. They are fixed rather
// than optional because `-trimpath` is a build-input change, not a link flag: turning it on
// invalidated a fully warm 2.0 GB compile cache and cost 33s to repopulate. Flipping it per-run
// would pay that repeatedly, so the flags are constant for every build this package performs.
var ReleaseBuildFlags = []string{"-trimpath", "-ldflags=-s -w"}

// Paths locates the pieces of the cache relative to a module.
type Paths struct {
	// ModuleDirectory is the root of the cohere module.
	ModuleDirectory string

	// CacheDirectory holds both the built binaries and the Go build cache. It lives inside the
	// module so that other Go work neither shares it nor evicts it: Go's default cache trims
	// entries unused for about five days, which would silently turn a warm rebuild into a cold one.
	CacheDirectory string
}

// DefaultPaths puts the cache at `.cache/cohere/` inside the module, which the repo already
// ignores.
func DefaultPaths(moduleDirectory string) Paths {
	return Paths{
		ModuleDirectory: moduleDirectory,
		CacheDirectory:  filepath.Join(moduleDirectory, ".cache", "cohere"),
	}
}

// BinaryDirectory holds the hash-named binaries.
func (paths Paths) BinaryDirectory() string {
	return filepath.Join(paths.CacheDirectory, "bin")
}

// GoCacheDirectory is the pinned GOCACHE.
func (paths Paths) GoCacheDirectory() string {
	return filepath.Join(paths.CacheDirectory, "gocache")
}

// BinaryPath is where the binary with this hash lives.
//
// The name carries the platform as well as the hash. A cache directory can outlive a change of
// machine, and a binary for the wrong architecture fails in a far more confusing way than a miss.
func (paths Paths) BinaryPath(hash string) string {
	return filepath.Join(paths.BinaryDirectory(), fmt.Sprintf("cohere-%s-%s-%s", runtime.GOOS, runtime.GOARCH, hash))
}

// DevelopmentBinaryPath is the stable path used by `--dev`.
//
// A hash-named binary is a new filename on every change, and Go does not cache link output, so
// every rule edit pays a full link. Measured: 2.03s for a leaf rule edit against a 0.29s no-op when
// nothing changed. The stable path is what makes the no-op reachable, so the authoring loop gets
// the cheap case whenever the tree has not actually moved.
func (paths Paths) DevelopmentBinaryPath() string {
	return filepath.Join(paths.BinaryDirectory(), "cohere-dev")
}

// Resolve returns the path to a binary matching the current inputs, building it if needed.
//
// The returned boolean reports whether a build ran, so a caller can tell the user why a normally
// instant command took a second.
func Resolve(paths Paths, packagePath string, development bool) (string, bool, error) {
	inputs, err := CollectInputs(paths.ModuleDirectory, packagePath, ReleaseBuildFlags)
	if err != nil {
		return "", false, err
	}

	hash, err := inputs.Compute()
	if err != nil {
		return "", false, err
	}

	binaryPath := paths.BinaryPath(hash)
	if development {
		binaryPath = paths.DevelopmentBinaryPath()
	}

	// In development the binary name never changes, so its existence says nothing about whether it
	// is current. The hash is recorded beside it and compared instead. Skipping this would serve a
	// stale binary from a stable path, which is the same lie as a wrong hash.
	if development {
		if upToDate, err := developmentBinaryIsCurrent(paths, hash); err != nil {
			return "", false, err
		} else if upToDate {
			return binaryPath, false, nil
		}
	} else if _, err := os.Stat(binaryPath); err == nil {
		return binaryPath, false, nil
	}

	if err := build(paths, packagePath, binaryPath); err != nil {
		return "", false, err
	}

	if development {
		if err := recordDevelopmentHash(paths, hash); err != nil {
			return "", false, err
		}
	}

	return binaryPath, true, nil
}

// developmentHashPath records which inputs the development binary was built from.
func (paths Paths) developmentHashPath() string {
	return paths.DevelopmentBinaryPath() + ".hash"
}

// developmentBinaryIsCurrent reports whether the stable-path binary matches the current inputs.
func developmentBinaryIsCurrent(paths Paths, hash string) (bool, error) {
	if _, err := os.Stat(paths.DevelopmentBinaryPath()); err != nil {
		return false, nil
	}

	recorded, err := os.ReadFile(paths.developmentHashPath())
	if err != nil {
		// A binary with no recorded hash is of unknown provenance. Rebuilding costs a second;
		// trusting it could run rules that are not the ones on disk.
		return false, nil
	}

	return strings.TrimSpace(string(recorded)) == hash, nil
}

// recordDevelopmentHash stores the hash the development binary was built from.
func recordDevelopmentHash(paths Paths, hash string) error {
	if err := os.WriteFile(paths.developmentHashPath(), []byte(hash+"\n"), 0o644); err != nil {
		return fmt.Errorf("recording the development build hash: %w", err)
	}
	return nil
}

// build compiles the binary to binaryPath, with the cache pinned inside the module.
func build(paths Paths, packagePath string, binaryPath string) error {
	if err := os.MkdirAll(paths.BinaryDirectory(), 0o755); err != nil {
		return fmt.Errorf("creating the binary cache directory: %w", err)
	}
	if err := os.MkdirAll(paths.GoCacheDirectory(), 0o755); err != nil {
		return fmt.Errorf("creating the Go build cache directory: %w", err)
	}

	arguments := append([]string{"build"}, ReleaseBuildFlags...)
	arguments = append(arguments, "-o", binaryPath, packagePath)

	command := exec.Command("go", arguments...)
	command.Dir = paths.ModuleDirectory
	command.Env = append(os.Environ(),
		"GOCACHE="+paths.GoCacheDirectory(),
		// Nothing here needs cgo, and disabling it keeps the build from depending on a C toolchain
		// that a fresh machine may not have.
		"CGO_ENABLED=0",
	)
	command.Stderr = os.Stderr

	if err := command.Run(); err != nil {
		return fmt.Errorf("building cohere: %w", err)
	}

	// Go reports success by exit code, but the thing we are about to exec is the file. Checking it
	// exists closes the gap between "the build said it worked" and "there is a binary here", which
	// is exactly the kind of gap that lets a missing binary reach an exec.
	if _, err := os.Stat(binaryPath); err != nil {
		return fmt.Errorf("go build reported success but produced no binary at %s: %w", binaryPath, err)
	}

	return nil
}
