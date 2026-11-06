package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Freezing runs a binary whose inputs were never compared against the rules on disk. The only thing
// separating that from the stale-binary defect is that the operator is told, so every test here
// asserts some form of "it refuses to be silent."

func TestResolveFrozenRefusesWhenTheCacheIsEmpty(test *testing.T) {
	test.Parallel()

	// "Frozen with nothing to freeze" is a missing binary, and a missing binary is loud here like
	// everywhere else. Returning a path that does not exist, or an empty one, would hand the caller
	// something to exec that fails later and further from the cause.
	paths := Paths{ModuleDirectory: test.TempDir(), CacheDirectory: test.TempDir()}
	if createDirectoryError := os.MkdirAll(paths.BinaryDirectory(), 0o755); createDirectoryError != nil {
		test.Fatalf("creating the binary directory: %v", createDirectoryError)
	}

	frozen, resolveFrozenError := ResolveFrozen(paths)
	if resolveFrozenError == nil {
		test.Fatalf("freezing an empty cache succeeded and selected %q", frozen.Path)
	}
	if !strings.Contains(resolveFrozenError.Error(), "nothing was run") {
		test.Errorf(
			"the error does not say that nothing ran, so a reader could take it for a warning: %v",
			resolveFrozenError,
		)
	}
}

func TestResolveFrozenRefusesWhenTheCacheIsMissing(test *testing.T) {
	test.Parallel()

	// A cache directory that was never created is a different failure from an empty one (it means
	// no build has ever run here), and it must be just as loud rather than treated as "no results".
	paths := Paths{ModuleDirectory: test.TempDir(), CacheDirectory: filepath.Join(test.TempDir(), "absent")}

	if _, resolveFrozenError := ResolveFrozen(paths); resolveFrozenError == nil {
		test.Fatal("freezing against a cache directory that does not exist succeeded")
	}
}

func TestResolveFrozenNamesWhatItSelected(test *testing.T) {
	test.Parallel()

	// The caller prints the hash and the build time, and it can only do that if this reports them.
	// A frozen run that could not say which binary it ran would be indistinguishable from an
	// ordinary one, which is the whole thing being guarded against.
	paths := newCacheWithBinaries(test, map[string]string{platformBinaryPrefix() + "abc123def4560000": "binary"})

	frozen, resolveFrozenError := ResolveFrozen(paths)
	if resolveFrozenError != nil {
		test.Fatalf("freezing a cache with one binary failed: %v", resolveFrozenError)
	}

	if frozen.Hash != "abc123def4560000" {
		test.Errorf("hash is %q, so the announcement would not name the binary that ran", frozen.Hash)
	}
	if frozen.ModifiedAt == "" {
		test.Error("no build time, so a reader cannot judge how old the frozen binary is")
	}
	if !strings.HasSuffix(frozen.Path, platformBinaryPrefix()+"abc123def4560000") {
		test.Errorf("selected %q, which is not the binary in the cache", frozen.Path)
	}
}

func TestResolveFrozenPicksTheNewestBinary(test *testing.T) {
	test.Parallel()

	// The most recent successful build is the closest thing to the rules on disk the cache can
	// offer. Picking any other one would be arbitrarily staler for no reason a reader could predict.
	paths := newCacheWithBinaries(test, map[string]string{
		platformBinaryPrefix() + "1111111111111111": "old",
		platformBinaryPrefix() + "2222222222222222": "newer",
		platformBinaryPrefix() + "3333333333333333": "newest",
	})

	setModificationTimes(test, paths, []string{
		platformBinaryPrefix() + "1111111111111111",
		platformBinaryPrefix() + "2222222222222222",
		platformBinaryPrefix() + "3333333333333333",
	})

	frozen, resolveFrozenError := ResolveFrozen(paths)
	if resolveFrozenError != nil {
		test.Fatalf("freezing failed: %v", resolveFrozenError)
	}
	if frozen.Hash != "3333333333333333" {
		test.Errorf("selected %q rather than the newest binary", frozen.Hash)
	}
}

func TestResolveFrozenIgnoresNonExecutableEntries(test *testing.T) {
	test.Parallel()

	// The sidecar recording the development build's hash sits in the same directory and is not
	// something to exec. Selecting it would produce an exec failure whose message is about file
	// formats rather than about the cache.
	paths := newCacheWithBinaries(test, map[string]string{platformBinaryPrefix() + "abc123def4560000": "binary"})

	hashSidecar := filepath.Join(paths.BinaryDirectory(), "cohere-dev.hash")
	if writeError := os.WriteFile(hashSidecar, []byte("abc123def4560000\n"), 0o644); writeError != nil {
		test.Fatalf("writing the sidecar: %v", writeError)
	}

	frozen, resolveFrozenError := ResolveFrozen(paths)
	if resolveFrozenError != nil {
		test.Fatalf("freezing failed: %v", resolveFrozenError)
	}
	if strings.HasSuffix(frozen.Path, ".hash") {
		test.Errorf("selected the hash sidecar %q, which is metadata rather than a binary", frozen.Path)
	}
}

// newCacheWithBinaries builds a cache directory holding the named executables.
func newCacheWithBinaries(test *testing.T, binaries map[string]string) Paths {
	test.Helper()

	paths := Paths{ModuleDirectory: test.TempDir(), CacheDirectory: test.TempDir()}
	if createDirectoryError := os.MkdirAll(paths.BinaryDirectory(), 0o755); createDirectoryError != nil {
		test.Fatalf("creating the binary directory: %v", createDirectoryError)
	}

	for name, contents := range binaries {
		path := filepath.Join(paths.BinaryDirectory(), name)
		if writeError := os.WriteFile(path, []byte(contents), 0o755); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	return paths
}

// setModificationTimes stamps the named binaries oldest-first, one second apart.
//
// Written rather than relying on creation order because a map iterates in a random order and the
// files would otherwise share a timestamp, which would make the newest-wins assertion pass or fail
// by chance.
func setModificationTimes(test *testing.T, paths Paths, oldestFirst []string) {
	test.Helper()

	base := time.Date(2026, time.August, 23, 1, 0, 0, 0, time.UTC)
	for index, name := range oldestFirst {
		stamp := base.Add(time.Duration(index) * time.Second)
		if changeTimesError := os.Chtimes(
			filepath.Join(paths.BinaryDirectory(), name),
			stamp,
			stamp,
		); changeTimesError != nil {
			test.Fatalf("stamping %s: %v", name, changeTimesError)
		}
	}
}
