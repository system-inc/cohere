package program_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

func sampleCache(directories []string) *program.ResolutionCache {
	return &program.ResolutionCache{
		Fingerprint: program.FingerprintDirectories(directories),
		Directories: directories,
		Entries: []program.ResolutionEntry{
			{
				FromFile:         "/project/source/main.ts",
				Specifier:        "./helper",
				ResolvedFileName: "/project/source/helper.ts",
				Extension:        ".ts",
				Mode:             1,
				Flags:            program.ResolutionFlagUsedTsExtension,
			},
			{
				FromFile:         "/project/source/main.ts",
				Specifier:        "react",
				ResolvedFileName: "/project/node_modules/react/index.d.ts",
				Extension:        ".d.ts",
				Mode:             1,
				Flags:            program.ResolutionFlagExternalLibrary,
			},
			{
				// A failed resolution is a real answer worth storing: replaying it is what
				// saves the failed lookups, which are the majority of the traffic.
				FromFile:         "/project/source/main.ts",
				Specifier:        "does-not-exist",
				ResolvedFileName: "",
				Extension:        "",
				Mode:             1,
			},
		},
	}
}

// TestResolutionCacheRoundTripsEveryField is the guard against the bug this format invites.
// The artifact is raw offsets into a blob, so a field the encoder forgets does not fail to
// decode, it decodes to empty while everything around it looks correct. Compare every field
// rather than spot-checking, because a spot check is what lets one dropped field through.
func TestResolutionCacheRoundTripsEveryField(t *testing.T) {
	original := sampleCache([]string{t.TempDir()})

	decoded, err := program.DecodeResolutionCache(original.Encode())
	if err != nil {
		t.Fatalf("decoding what we just encoded: %v", err)
	}

	if decoded.Fingerprint != original.Fingerprint {
		t.Errorf("fingerprint did not survive: %x against %x", decoded.Fingerprint, original.Fingerprint)
	}
	if len(decoded.Directories) != len(original.Directories) {
		t.Fatalf("directories: %d back from %d", len(decoded.Directories), len(original.Directories))
	}
	for index := range original.Directories {
		if decoded.Directories[index] != original.Directories[index] {
			t.Errorf("directory %d: %q against %q", index, decoded.Directories[index], original.Directories[index])
		}
	}

	if len(decoded.Entries) != len(original.Entries) {
		t.Fatalf("entries: %d back from %d", len(decoded.Entries), len(original.Entries))
	}
	for index, want := range original.Entries {
		got := decoded.Entries[index]
		if got.FromFile != want.FromFile {
			t.Errorf("entry %d FromFile: %q against %q", index, got.FromFile, want.FromFile)
		}
		if got.Specifier != want.Specifier {
			t.Errorf("entry %d Specifier: %q against %q", index, got.Specifier, want.Specifier)
		}
		if got.ResolvedFileName != want.ResolvedFileName {
			t.Errorf("entry %d ResolvedFileName: %q against %q", index, got.ResolvedFileName, want.ResolvedFileName)
		}
		if got.Extension != want.Extension {
			t.Errorf("entry %d Extension: %q against %q", index, got.Extension, want.Extension)
		}
		if got.Mode != want.Mode {
			t.Errorf("entry %d Mode: %d against %d", index, got.Mode, want.Mode)
		}
		if got.Flags != want.Flags {
			t.Errorf("entry %d Flags: %d against %d", index, got.Flags, want.Flags)
		}
	}
}

// TestResolutionCacheRejectsBadArtifacts proves the decoder fails loudly rather than
// returning confident nonsense. Every case here would, without the length checks, produce
// either a panic or entries pointing at the wrong strings.
func TestResolutionCacheRejectsBadArtifacts(t *testing.T) {
	valid := sampleCache([]string{t.TempDir()}).Encode()

	cases := []struct {
		name   string
		buffer []byte
	}{
		{"empty", nil},
		{"shorter than the header", valid[:10]},
		{"truncated mid-record", valid[:len(valid)/2]},
		{"wrong magic", append([]byte("NOTVFYRS"), valid[8:]...)},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decoded, err := program.DecodeResolutionCache(testCase.buffer)
			if err == nil {
				t.Fatalf("a %s artifact decoded without error into %d entries; the decoder "+
					"cannot detect corruption and every clean read from it is vacuous",
					testCase.name, len(decoded.Entries))
			}
			if !errors.Is(err, program.ErrResolutionCacheUnreadable) {
				t.Errorf("error should be ErrResolutionCacheUnreadable so callers can treat it "+
					"as a cold cache rather than a failure, got: %v", err)
			}
		})
	}
}

// TestFingerprintDetectsShapeChanges is the load-bearing test of this whole cache, and it is
// written to fail in the direction that matters.
//
// A resolution depends on the shape of the filesystem, not on file contents. If the
// fingerprint misses a shape change, the cache serves a stale resolution and the tool reports
// a clean tree over a real error. So every event that can change a resolution is exercised
// here, and the content-edit case is included specifically to show the fingerprint does NOT
// fire on it: that is correct, and it is why this cache cannot be keyed on content hashes.
func TestFingerprintDetectsShapeChanges(t *testing.T) {
	directory := t.TempDir()
	directories := []string{directory}

	seed := filepath.Join(directory, "seed.ts")
	if err := os.WriteFile(seed, []byte("export const a = 1;\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Filesystems store mtime at limited resolution, so a change made in the same tick as the
	// baseline can hash identically. Settling makes the test measure the filesystem rather
	// than the clock.
	settle := func() { time.Sleep(20 * time.Millisecond) }

	cases := []struct {
		name       string
		act        func()
		wantChange bool
	}{
		{"add a file", func() {
			_ = os.WriteFile(filepath.Join(directory, "added.ts"), []byte("export const b = 2;\n"), 0o644)
		}, true},
		{"remove a file", func() {
			_ = os.Remove(filepath.Join(directory, "added.ts"))
		}, true},
		{"rename a file", func() {
			_ = os.Rename(seed, filepath.Join(directory, "renamed.ts"))
		}, true},
		{"add a subdirectory", func() {
			_ = os.Mkdir(filepath.Join(directory, "sub"), 0o755)
		}, true},
		{"edit a file's contents", func() {
			_ = os.WriteFile(filepath.Join(directory, "renamed.ts"), []byte("export const a = 999;\n"), 0o644)
		}, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			settle()
			before := program.FingerprintDirectories(directories)
			testCase.act()
			settle()
			changed := program.FingerprintDirectories(directories) != before

			if changed != testCase.wantChange {
				if testCase.wantChange {
					t.Fatalf("the fingerprint did not move when a file was %s. A resolution "+
						"cache guarded by it would serve stale answers after this event and "+
						"report a clean tree", testCase.name)
				}
				t.Fatalf("the fingerprint moved on a content edit, which invalidates the cache " +
					"on every ordinary file change and makes it never hit")
			}
		})
	}
}

// TestFingerprintIsOrderIndependent guards a failure that is invisible and expensive: a
// fingerprint that depended on slice order would differ run to run over an identical
// filesystem, so the cache would never hit and nobody would see an error, only a tool that
// is not faster.
func TestFingerprintIsOrderIndependent(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	third := t.TempDir()

	forward := program.FingerprintDirectories([]string{first, second, third})
	backward := program.FingerprintDirectories([]string{third, second, first})

	if forward != backward {
		t.Fatal("the fingerprint depends on the order directories are passed in, so it would " +
			"differ between runs over an identical filesystem and the cache would never hit")
	}
}

// TestStaleCacheIsDetected checks the caller-facing decision rather than the hash underneath
// it, and pins the bias: any doubt resolves toward stale. A cache wrongly treated as fresh
// reports a clean tree; one wrongly treated as stale costs a slow run.
func TestStaleCacheIsDetected(t *testing.T) {
	directory := t.TempDir()
	cache := sampleCache([]string{directory})

	if cache.IsStale() {
		t.Fatal("a cache fingerprinted moments ago reads as stale, so it would never be used")
	}

	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(directory, "new.ts"), []byte("export const c = 3;\n"), 0o644); err != nil {
		t.Fatalf("adding a file: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	if !cache.IsStale() {
		t.Fatal("a file appeared and the cache still reads as fresh; this is the silent " +
			"failure the whole domain exists to prevent")
	}

	t.Run("no directories reads as stale", func(t *testing.T) {
		empty := &program.ResolutionCache{}
		if !empty.IsStale() {
			t.Fatal("a cache covering no directories reads as fresh, so it would be trusted " +
				"while guarding nothing")
		}
	})

	t.Run("nil reads as stale", func(t *testing.T) {
		var nilCache *program.ResolutionCache
		if !nilCache.IsStale() {
			t.Fatal("a nil cache reads as fresh")
		}
	})
}
