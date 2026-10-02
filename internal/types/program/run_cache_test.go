package program_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

// runCacheTree is a small project: two sources in nested directories, a config, and the key.
type runCacheTree struct {
	root    string
	sourceA string
	sourceB string
	config  string
	subdir  string
	files   []string
	absent  []string
	key     string
}

func newRunCacheTree(t *testing.T) *runCacheTree {
	t.Helper()
	root := t.TempDir()
	tree := &runCacheTree{
		root:    root,
		sourceA: filepath.Join(root, "source", "a.ts"),
		subdir:  filepath.Join(root, "source", "nested"),
		config:  filepath.Join(root, "tsconfig.json"),
	}
	tree.sourceB = filepath.Join(tree.subdir, "b.ts")
	writeFile(t, tree.sourceA, "export const a = 1;\n")
	writeFile(t, tree.sourceB, "export const b = 2;\n")
	writeFile(t, tree.config, `{"include":["source"]}`)
	tree.files = []string{tree.sourceA, tree.sourceB, tree.config}
	// Absent inputs sit in their own directory, which no recorded file shares, so the absence check is
	// the only thing that can notice one appearing. See the "absent now exists" case.
	tree.absent = []string{filepath.Join(root, "unwatched", ".cohererc")}
	if err := os.MkdirAll(filepath.Join(root, "unwatched"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	key, err := program.RunCacheKey([]string{"--lint"}, root)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tree.key = key
	return tree
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// record captures the tree as a run that printed one finding and exited 1.
func (tree *runCacheTree) record(t *testing.T) *program.RunCache {
	t.Helper()
	cache, err := program.RecordRunCache(tree.key, tree.files, nil, tree.absent, []byte("a.ts:1 finding\n"), 1)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	return cache
}

// waitForClock guarantees the next write lands on a later modification time.
//
// Every invalidation below is detected by an mtime moving. Writing twice within the filesystem's
// timestamp granularity would leave it unchanged and turn a real change into a hit, so a test that
// skipped this could pass by never actually changing anything. That is the vacuous shape this whole
// suite is guarding against, so it is made impossible here rather than left to luck.
func waitForClock(t *testing.T, path string) {
	t.Helper()
	before, err := os.Stat(path)
	if err != nil {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		probe := filepath.Join(t.TempDir(), "clock")
		os.WriteFile(probe, nil, 0o644)
		if info, err := os.Stat(probe); err == nil && info.ModTime().After(before.ModTime()) {
			return
		}
	}
}

// A run cache with nothing changed is a hit. This is the control for every case below: if it were
// a miss, each "changing X invalidates" test would pass because the cache never hits at all.
func TestRunCacheHitsWhenNothingChanged(t *testing.T) {
	tree := newRunCacheTree(t)
	cache := tree.record(t)
	if err := cache.Check(tree.key); err != nil {
		t.Fatalf("an untouched tree missed, so every invalidation test below is vacuous: %v", err)
	}
}

// Each case changes exactly one input category and must turn a hit into a miss.
//
// One row per category the task names. A category without a row is a category nobody has shown the
// cache noticing, and the failure mode there is silence: a stale verdict replayed as current.
func TestRunCacheInvalidatesOnEveryInputCategory(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, tree *runCacheTree)
	}{
		{"a source file's contents change", func(t *testing.T, tree *runCacheTree) {
			waitForClock(t, tree.sourceA)
			writeFile(t, tree.sourceA, "export const a = 100;\n")
		}},
		{"a source file is deleted", func(t *testing.T, tree *runCacheTree) {
			os.Remove(tree.sourceB)
		}},
		{"a file is added at the top level", func(t *testing.T, tree *runCacheTree) {
			waitForClock(t, filepath.Dir(tree.sourceA))
			writeFile(t, filepath.Join(filepath.Dir(tree.sourceA), "new.ts"), "export const n = 3;\n")
		}},
		// The case the design missed. A file added one directory down changes only that
		// directory's mtime. A cache watching only the include roots reports clean here.
		{"a file is added in a nested directory", func(t *testing.T, tree *runCacheTree) {
			waitForClock(t, tree.subdir)
			writeFile(t, filepath.Join(tree.subdir, "added.ts"), "export const x = 4;\n")
		}},
		{"the config changes", func(t *testing.T, tree *runCacheTree) {
			waitForClock(t, tree.config)
			writeFile(t, tree.config, `{"include":["source","more"]}`)
		}},
		// The absent input lives in a directory nothing else is watched in, on purpose. Placed beside
		// a watched file, creating it would also move that directory's mtime and the directory check
		// would catch it, so this case would pass without ever exercising the absence check. It did
		// exactly that until the mutant "absent inputs always match" survived it.
		{"an input that was absent now exists", func(t *testing.T, tree *runCacheTree) {
			writeFile(t, tree.absent[0], "{}")
		}},
		{"a file is replaced by a directory of the same name", func(t *testing.T, tree *runCacheTree) {
			os.Remove(tree.sourceA)
			os.MkdirAll(tree.sourceA, 0o755)
		}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tree := newRunCacheTree(t)
			cache := tree.record(t)
			if err := cache.Check(tree.key); err != nil {
				t.Fatalf("the control missed before the change, so this case proves nothing: %v", err)
			}

			testCase.change(t, tree)

			err := cache.Check(tree.key)
			if err == nil {
				t.Fatal("the change was not noticed: a stale run would be replayed as current")
			}
			if !errors.Is(err, program.ErrRunCacheMiss) {
				t.Fatalf("a change produced an unexpected error rather than a clean miss: %v", err)
			}
		})
	}
}

// The key covers what is not a file: the flags, the directory, the binary. A run with different
// flags over identical files is a different run.
func TestRunCacheKeySeparatesRuns(t *testing.T) {
	root := t.TempDir()
	lint, err := program.RunCacheKey([]string{"--lint"}, root)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	same, _ := program.RunCacheKey([]string{"--lint"}, root)
	if lint != same {
		t.Fatal("the same run produced two keys, so the cache could never hit")
	}

	for name, other := range map[string]func() (string, error){
		"different flags":             func() (string, error) { return program.RunCacheKey([]string{"--types"}, root) },
		"an extra flag":               func() (string, error) { return program.RunCacheKey([]string{"--lint", "--fix"}, root) },
		"a different directory":       func() (string, error) { return program.RunCacheKey([]string{"--lint"}, t.TempDir()) },
		"arguments split differently": func() (string, error) { return program.RunCacheKey([]string{"--li", "nt"}, root) },
	} {
		key, err := other()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if key == lint {
			t.Errorf("%s produced the same key, so one run would replay the other", name)
		}
	}
}

// A run over the right files with the wrong key misses. The binary is part of the key, so this is
// also the binary-changed case: a rebuilt cohere has a new mtime and therefore a new key.
func TestRunCacheMissesOnADifferentKey(t *testing.T) {
	tree := newRunCacheTree(t)
	cache := tree.record(t)
	if err := cache.Check(tree.key + "x"); !errors.Is(err, program.ErrRunCacheMiss) {
		t.Fatalf("a different key was accepted: %v", err)
	}
}

// Doubt is a miss. Each of these is a manifest the cache cannot prove anything from.
func TestRunCacheDoubtIsAMiss(t *testing.T) {
	tree := newRunCacheTree(t)

	t.Run("no cache", func(t *testing.T) {
		var cache *program.RunCache
		if err := cache.Check(tree.key); !errors.Is(err, program.ErrRunCacheMiss) {
			t.Fatalf("a nil cache did not miss: %v", err)
		}
	})
	t.Run("a different format version", func(t *testing.T) {
		cache := tree.record(t)
		cache.Version++
		if err := cache.Check(tree.key); !errors.Is(err, program.ErrRunCacheMiss) {
			t.Fatalf("an unknown version was read: %v", err)
		}
	})
	// A manifest with no inputs matches every tree, so it would replay forever. It can only come
	// from a writer that recorded nothing, and that writer is the bug.
	t.Run("a manifest with no inputs", func(t *testing.T) {
		cache := tree.record(t)
		cache.Inputs = nil
		if err := cache.Check(tree.key); !errors.Is(err, program.ErrRunCacheMiss) {
			t.Fatalf("an empty manifest hit, so it would match any tree: %v", err)
		}
	})
	t.Run("an unreadable file on disk", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "run.cache")
		writeFile(t, path, "{not json")
		if _, err := program.ReadRunCache(path); !errors.Is(err, program.ErrRunCacheMiss) {
			t.Fatalf("a corrupt manifest was not a miss: %v", err)
		}
	})
	t.Run("a missing file on disk", func(t *testing.T) {
		if _, err := program.ReadRunCache(filepath.Join(t.TempDir(), "absent")); !errors.Is(err, program.ErrRunCacheMiss) {
			t.Fatalf("a missing manifest was not a miss: %v", err)
		}
	})
}

// Recording watches the directories itself, so a caller cannot ship a cache blind to added files.
//
// The design's hole was a file added under an include glob. If the directories were the caller's
// job, one caller forgetting them would reintroduce it silently. So Record derives them, and this
// asserts it did: every source's directory appears as a directory entry.
func TestRunCacheRecordWatchesEveryDirectoryItself(t *testing.T) {
	tree := newRunCacheTree(t)
	cache := tree.record(t)

	watched := map[string]bool{}
	for _, input := range cache.Inputs {
		if input.Directory {
			watched[input.Path] = true
		}
	}
	for _, file := range tree.files {
		if !watched[filepath.Dir(file)] {
			t.Errorf("%s's directory is not watched, so a file added beside it would go unnoticed", file)
		}
	}
}

// The replayed run is the recorded run, output and exit code both, through a real write and read.
//
// The exit code matters as much as the output. A cache that printed a failing run's findings and
// exited zero would pass CI while describing a failure.
func TestRunCacheReplaysOutputAndExitCodeThroughDisk(t *testing.T) {
	tree := newRunCacheTree(t)
	path := filepath.Join(t.TempDir(), "nested", "run.cache")
	if err := program.WriteRunCache(path, tree.record(t)); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := program.ReadRunCache(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := loaded.Check(tree.key); err != nil {
		t.Fatalf("a cache read back from disk did not hit: %v", err)
	}
	if string(loaded.Output) != "a.ts:1 finding\n" {
		t.Errorf("output: %q", loaded.Output)
	}
	if loaded.ExitCode != 1 {
		t.Errorf("exit code %d, want 1: a failing run would replay as passing", loaded.ExitCode)
	}

	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("the directory holds %d entries, want only the manifest: a temporary was left behind", len(entries))
	}
}

// A directory handed in among the files is recorded as a directory, and the untouched tree still hits.
//
// The input recorder reports every path the compiler touched, and the compiler lists and probes
// directories as well as reading files. An earlier Record stamped every entry of files as a file, so a
// directory arrived as one and every check reported it "changed between file and directory". The cache
// then never hit, which no other test here could see: they all handed in files only. Found by running
// the untouched-tree check against the real ahra tree.
func TestRunCacheRecordsADirectoryAmongTheFilesAsADirectory(t *testing.T) {
	tree := newRunCacheTree(t)
	withDirectory := append(append([]string(nil), tree.files...), tree.subdir, tree.root)
	cache, err := program.RecordRunCache(tree.key, withDirectory, nil, tree.absent, []byte("x"), 0)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := cache.Check(tree.key); err != nil {
		t.Fatalf("an untouched tree missed because a directory was recorded as a file: %v", err)
	}
	seen := map[string]int{}
	for _, input := range cache.Inputs {
		seen[input.Path]++
	}
	for path, count := range seen {
		if count > 1 {
			t.Errorf("%s recorded %d times, so every check stats it %d times", path, count, count)
		}
	}
}

// A fact the key ignores is a change the cache cannot see. The command hashes what git reports as
// changed into a fact, so a commit or a new untracked file must move the key even when no source file
// the build read has changed.
func TestRunCacheKeyCoversEveryFact(t *testing.T) {
	root := t.TempDir()
	base, err := program.RunCacheKey(nil, root, "root=/a", "scope=x")
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	for name, facts := range map[string][]string{
		"a fact changed":            {"root=/a", "scope=y"},
		"a fact removed":            {"root=/a"},
		"a fact added":              {"root=/a", "scope=x", "extra"},
		"facts reordered":           {"scope=x", "root=/a"},
		"two facts joined into one": {"root=/ascope=x"},
	} {
		key, err := program.RunCacheKey(nil, root, facts...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if key == base {
			t.Errorf("%s left the key unchanged, so that change would replay a stale run", name)
		}
	}
}
