package dispatch

import (
	"fmt"
	"path/filepath"
	"runtime"
)

// Paths locates the pieces of the cache relative to a module.
type Paths struct {
	// ModuleDirectory is the root of the cohere module.
	ModuleDirectory string

	// CacheDirectory holds both the built binaries and the Go build cache. It lives inside the
	// module so that other Go work neither shares it nor evicts it: Go's default cache trims
	// entries unused for about five days, which would silently turn a warm rebuild into a cold one.
	CacheDirectory string
}

// BinaryDirectory holds the hash-named binaries.
func (paths Paths) BinaryDirectory() string {
	return filepath.Join(paths.CacheDirectory, "bin")
}

// BinaryPath is where the committed-tree binary with this hash lives.
//
// The name carries the platform as well as the hash. A cache directory can outlive a change of
// machine, and a binary for the wrong architecture fails in a far more confusing way than a miss.
func (paths Paths) BinaryPath(hash string) string {
	return filepath.Join(paths.BinaryDirectory(), platformBinaryPrefix()+hash+executableSuffix)
}

// DevelopmentBinaryPath is the stable path used by `--dev`.
//
// A hash-named binary is a new filename on every change, and Go does not cache link output, so
// every rule edit pays a full link. Measured: 2.03s for a leaf rule edit against a 0.29s no-op when
// nothing changed. The stable path is what makes the no-op reachable, so the authoring loop gets
// the cheap case whenever the tree has not actually moved.
func (paths Paths) DevelopmentBinaryPath() string {
	return filepath.Join(paths.BinaryDirectory(), developmentBinaryName)
}

// developmentHashPath records which inputs the development binary was built from.
func (paths Paths) developmentHashPath() string {
	return paths.DevelopmentBinaryPath() + ".hash"
}

// SnapshotDirectory holds the per-build extractions of this repository. Each is removed once its
// binary is built, because the binary is self-contained and the next commit needs its own.
func (paths Paths) SnapshotDirectory() string {
	return filepath.Join(paths.CacheDirectory, "snapshot")
}

// CompilerDirectory holds compiler extractions, one per pinned commit.
//
// These are kept, because the compiler is 66,000 files and takes about seven seconds to extract,
// while the pin changes rarely. Two commits of this repository that pin the same compiler build
// against the same directory.
func (paths Paths) CompilerDirectory() string {
	return filepath.Join(paths.CacheDirectory, "compiler")
}

// PruneLogPath is where every prune records what it examined and removed.
func (paths Paths) PruneLogPath() string {
	return filepath.Join(paths.CacheDirectory, "prune.log")
}

// SwiftEngineDirectory is where the Swift engine's package lives inside the cohere module.
func (paths Paths) SwiftEngineDirectory() string {
	return filepath.Join(paths.ModuleDirectory, "swift")
}

// SwiftBuildDirectory is the scratch path the engine is built in.
//
// Its own, rather than the package's `.build`: that one belongs to whoever is developing the engine,
// and a build here taking its lock, or replacing its products mid-test, would be one process breaking
// another for no reason a reader of either could see.
func (paths Paths) SwiftBuildDirectory() string {
	return filepath.Join(paths.CacheDirectory, "swift-build")
}

// SwiftEngineBinaryPath is where the engine built from inputs with this hash lives, named the way
// BinaryPath names cohere's, platform included.
func (paths Paths) SwiftEngineBinaryPath(hash string) string {
	return filepath.Join(
		paths.BinaryDirectory(),
		fmt.Sprintf("cohere-swift-%s-%s-%s", runtime.GOOS, runtime.GOARCH, hash),
	)
}

// SwiftSourceWorktree is the worktree the committed engine is built from.
func (paths Paths) SwiftSourceWorktree() string {
	return filepath.Join(paths.CacheDirectory, "swift-source")
}

// SwiftCommittedBuildDirectory is the SwiftPM scratch path for committed builds. Separate from the
// working-tree engine's, because one scratch path fed two package directories would rebuild cold every
// time the other was used.
func (paths Paths) SwiftCommittedBuildDirectory() string {
	return filepath.Join(paths.CacheDirectory, "swift-build-committed")
}

// swiftSourceLock serializes moving the worktree and building in it.
func (paths Paths) swiftSourceLock() string {
	return filepath.Join(paths.CacheDirectory, "swift-source.lock")
}
