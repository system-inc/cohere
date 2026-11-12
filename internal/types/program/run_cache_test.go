package program_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

// runCacheTree is a small project: two sources in nested directories, a config, and the key.
type runCacheTree struct {
	root          string
	sourceA       string
	sourceB       string
	configuration string
	subdir        string
	files         []string
	absent        []string
	key           string
}

func newRunCacheTree(test *testing.T) *runCacheTree {
	test.Helper()
	root := test.TempDir()
	tree := &runCacheTree{
		root:          root,
		sourceA:       filepath.Join(root, "source", "a.ts"),
		subdir:        filepath.Join(root, "source", "nested"),
		configuration: filepath.Join(root, "tsconfig.json"),
	}
	tree.sourceB = filepath.Join(tree.subdir, "b.ts")
	writeFile(test, tree.sourceA, "export const a = 1;\n")
	writeFile(test, tree.sourceB, "export const b = 2;\n")
	writeFile(test, tree.configuration, `{"include":["source"]}`)
	tree.files = []string{tree.sourceA, tree.sourceB, tree.configuration}
	// Absent inputs sit in their own directory, which no recorded file shares, so the absence check is
	// the only thing that can notice one appearing. See the "absent now exists" case.
	tree.absent = []string{filepath.Join(root, "unwatched", ".cohererc")}
	if createDirectoryError := os.MkdirAll(filepath.Join(root, "unwatched"), 0o755); createDirectoryError != nil {
		test.Fatalf("mkdir: %v", createDirectoryError)
	}

	key, runCacheKeyError := program.RunCacheKey([]string{"--lint"}, root)
	if runCacheKeyError != nil {
		test.Fatalf("key: %v", runCacheKeyError)
	}
	tree.key = key
	return tree
}

func writeFile(test *testing.T, path string, contents string) {
	test.Helper()
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		test.Fatalf("mkdir: %v", createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		test.Fatalf("write %s: %v", path, writeError)
	}
}

// record captures the tree as a run that printed one finding and exited 1.
func (tree *runCacheTree) record(test *testing.T) *program.RunCache {
	test.Helper()
	cache, recordRunCacheError := program.RecordRunCache(
		tree.key,
		tree.files,
		nil,
		tree.absent,
		nil,
		[]byte("a.ts:1 finding\n"),
		1,
		time.Time{},
	)
	if recordRunCacheError != nil {
		test.Fatalf("record: %v", recordRunCacheError)
	}
	return cache
}

// waitForClock guarantees the next write lands on a later modification time.
//
// Every invalidation below is detected by an mtime moving. Writing twice within the filesystem's
// timestamp granularity would leave it unchanged and turn a real change into a hit, so a test that
// skipped this could pass by never actually changing anything. That is the vacuous shape this whole
// suite is guarding against, so it is made impossible here rather than left to luck.
func waitForClock(test *testing.T, path string) {
	test.Helper()
	before, statError := os.Stat(path)
	if statError != nil {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		probe := filepath.Join(test.TempDir(), "clock")
		if writeError := os.WriteFile(probe, nil, 0o644); writeError != nil {
			test.Fatalf("writing the clock probe: %v", writeError)
		}
		if info, statError := os.Stat(probe); statError == nil && info.ModTime().After(before.ModTime()) {
			return
		}
	}
}

// A run cache with nothing changed is a hit. This is the control for every case below: if it were
// a miss, each "changing X invalidates" test would pass because the cache never hits at all.
func TestRunCacheHitsWhenNothingChanged(test *testing.T) {
	test.Parallel()
	tree := newRunCacheTree(test)
	cache := tree.record(test)
	if checkError := cache.Check(tree.key); checkError != nil {
		test.Fatalf("an untouched tree missed, so every invalidation test below is vacuous: %v", checkError)
	}
}

// Each case changes exactly one input category and must turn a hit into a miss.
//
// One row per category the task names. A category without a row is a category nobody has shown the
// cache noticing, and the failure mode there is silence: a stale verdict replayed as current.
func TestRunCacheInvalidatesOnEveryInputCategory(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name   string
		change func(test *testing.T, tree *runCacheTree)
	}{
		{"a source file's contents change", func(test *testing.T, tree *runCacheTree) {
			waitForClock(test, tree.sourceA)
			writeFile(test, tree.sourceA, "export const a = 100;\n")
		}},
		{"a source file is deleted", func(test *testing.T, tree *runCacheTree) {
			if removeError := os.Remove(tree.sourceB); removeError != nil {
				test.Fatal(removeError)
			}
		}},
		{"a file is added at the top level", func(test *testing.T, tree *runCacheTree) {
			waitForClock(test, filepath.Dir(tree.sourceA))
			writeFile(test, filepath.Join(filepath.Dir(tree.sourceA), "new.ts"), "export const n = 3;\n")
		}},
		// The case the design missed. A file added one directory down changes only that
		// directory's mtime. A cache watching only the include roots reports clean here.
		{"a file is added in a nested directory", func(test *testing.T, tree *runCacheTree) {
			waitForClock(test, tree.subdir)
			writeFile(test, filepath.Join(tree.subdir, "added.ts"), "export const x = 4;\n")
		}},
		{"the config changes", func(test *testing.T, tree *runCacheTree) {
			waitForClock(test, tree.configuration)
			writeFile(test, tree.configuration, `{"include":["source","more"]}`)
		}},
		// The absent input lives in a directory nothing else is watched in, on purpose. Placed beside
		// a watched file, creating it would also move that directory's mtime and the directory check
		// would catch it, so this case would pass without ever exercising the absence check. It did
		// exactly that until the mutant "absent inputs always match" survived it.
		{"an input that was absent now exists", func(test *testing.T, tree *runCacheTree) {
			writeFile(test, tree.absent[0], "{}")
		}},
		{"a file is replaced by a directory of the same name", func(test *testing.T, tree *runCacheTree) {
			if removeError := os.Remove(tree.sourceA); removeError != nil {
				test.Fatal(removeError)
			}
			if createDirectoryError := os.MkdirAll(tree.sourceA, 0o755); createDirectoryError != nil {
				test.Fatal(createDirectoryError)
			}
		}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			tree := newRunCacheTree(test)
			cache := tree.record(test)
			if checkError := cache.Check(tree.key); checkError != nil {
				test.Fatalf("the control missed before the change, so this case proves nothing: %v", checkError)
			}

			testCase.change(test, tree)

			checkError := cache.Check(tree.key)
			if checkError == nil {
				test.Fatal("the change was not noticed: a stale run would be replayed as current")
			}
			if !errors.Is(checkError, program.ErrRunCacheMiss) {
				test.Fatalf("a change produced an unexpected error rather than a clean miss: %v", checkError)
			}
		})
	}
}

// The key covers what is not a file: the flags, the directory, the binary. A run with different
// flags over identical files is a different run.
func TestRunCacheKeySeparatesRuns(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	lint, runCacheKeyError := program.RunCacheKey([]string{"--lint"}, root)
	if runCacheKeyError != nil {
		test.Fatalf("key: %v", runCacheKeyError)
	}
	same, sameKeyError := program.RunCacheKey([]string{"--lint"}, root)
	if sameKeyError != nil {
		test.Fatalf("key: %v", sameKeyError)
	}
	if lint != same {
		test.Fatal("the same run produced two keys, so the cache could never hit")
	}

	for name, other := range map[string]func() (string, error){
		"different flags": func() (string, error) { return program.RunCacheKey([]string{"--types"}, root) },
		"an extra flag": func() (
			string,
			error,
		) {
			return program.RunCacheKey([]string{"--lint", "--fix"}, root)
		},
		"a different directory": func() (
			string,
			error,
		) {
			return program.RunCacheKey([]string{"--lint"}, test.TempDir())
		},
		"arguments split differently": func() (
			string,
			error,
		) {
			return program.RunCacheKey([]string{"--li", "nt"}, root)
		},
	} {
		key, otherError := other()
		if otherError != nil {
			test.Fatalf("%s: %v", name, otherError)
		}
		if key == lint {
			test.Errorf("%s produced the same key, so one run would replay the other", name)
		}
	}
}

// A run over the right files with the wrong key misses. The binary is part of the key, so this is
// also the binary-changed case: a rebuilt cohere has a new mtime and therefore a new key.
func TestRunCacheMissesOnADifferentKey(test *testing.T) {
	test.Parallel()
	tree := newRunCacheTree(test)
	cache := tree.record(test)
	if checkError := cache.Check(tree.key + "x"); !errors.Is(checkError, program.ErrRunCacheMiss) {
		test.Fatalf("a different key was accepted: %v", checkError)
	}
}

// Doubt is a miss. Each of these is a manifest the cache cannot prove anything from.
func TestRunCacheDoubtIsAMiss(test *testing.T) {
	test.Parallel()
	tree := newRunCacheTree(test)

	test.Run("no cache", func(test *testing.T) {
		test.Parallel()
		var cache *program.RunCache
		if checkError := cache.Check(tree.key); !errors.Is(checkError, program.ErrRunCacheMiss) {
			test.Fatalf("a nil cache did not miss: %v", checkError)
		}
	})
	test.Run("a different format version", func(test *testing.T) {
		test.Parallel()
		cache := tree.record(test)
		cache.Version++
		if checkError := cache.Check(tree.key); !errors.Is(checkError, program.ErrRunCacheMiss) {
			test.Fatalf("an unknown version was read: %v", checkError)
		}
	})
	// A manifest with no inputs matches every tree, so it would replay forever. It can only come
	// from a writer that recorded nothing, and that writer is the bug.
	test.Run("a manifest with no inputs", func(test *testing.T) {
		test.Parallel()
		cache := tree.record(test)
		cache.Inputs = nil
		if checkError := cache.Check(tree.key); !errors.Is(checkError, program.ErrRunCacheMiss) {
			test.Fatalf("an empty manifest hit, so it would match any tree: %v", checkError)
		}
	})
	// What a table holds when no run was recorded under an invocation, so the caller's lookup must miss.
	test.Run("no recorded run", func(test *testing.T) {
		test.Parallel()
		var absent *program.RunCache
		if checkError := absent.Check(tree.key); !errors.Is(checkError, program.ErrRunCacheMiss) {
			test.Fatalf("a missing record was not a miss: %v", checkError)
		}
	})
}

// Recording watches the directories itself, so a caller cannot ship a cache blind to added files.
//
// The design's hole was a file added under an include glob. If the directories were the caller's
// job, one caller forgetting them would reintroduce it silently. So Record derives them, and this
// asserts it did: every source's directory appears as a directory entry.
func TestRunCacheRecordWatchesEveryDirectoryItself(test *testing.T) {
	test.Parallel()
	tree := newRunCacheTree(test)
	cache := tree.record(test)

	watched := map[string]bool{}
	for _, input := range cache.Inputs {
		if input.Directory {
			watched[input.Path] = true
		}
	}
	for _, file := range tree.files {
		if !watched[filepath.Dir(file)] {
			test.Errorf("%s's directory is not watched, so a file added beside it would go unnoticed", file)
		}
	}
}

// The replayed run is the recorded run, output and exit code both, through a real write and read.
//
// The exit code matters as much as the output. A cache that printed a failing run's findings and
// exited zero would pass CI while describing a failure.
func TestRunCacheReplaysOutputAndExitCodeThroughDisk(test *testing.T) {
	test.Parallel()
	tree := newRunCacheTree(test)
	directory := filepath.Join(test.TempDir(), "nested")
	table := program.NewCacheTable()
	table.Runs["--no-fix"] = tree.record(test)
	sections := program.CacheTableSections{Runs: []string{"--no-fix"}}
	if writeCacheTableError := program.WriteCacheTable(
		directory,
		table,
		testIdentity,
		sections,
		program.PathAnchor{},
	); writeCacheTableError != nil {
		test.Fatalf("write: %v", writeCacheTableError)
	}
	read, readCacheTableError := program.ReadCacheTable(directory, testIdentity, sections, program.PathAnchor{})
	if readCacheTableError != nil {
		test.Fatalf("read: %v", readCacheTableError)
	}
	loaded := read.Runs["--no-fix"]
	if checkError := loaded.Check(tree.key); checkError != nil {
		test.Fatalf("a cache read back from disk did not hit: %v", checkError)
	}
	if string(loaded.Output) != "a.ts:1 finding\n" {
		test.Errorf("output: %q", loaded.Output)
	}
	if loaded.ExitCode != 1 {
		test.Errorf("exit code %d, want 1: a failing run would replay as passing", loaded.ExitCode)
	}

	entries, readDirectoryError := os.ReadDir(directory)
	if readDirectoryError != nil {
		test.Fatal(readDirectoryError)
	}
	// The writer created the directory, so beside the manifest it holds the ignore file it wrote there (#6vhp0vd).
	if len(entries) != 2 || entries[0].Name() != ".gitignore" || !strings.HasPrefix(entries[1].Name(), "run-") {
		test.Errorf(
			"the directory holds %v, want the ignore file and the manifest only: a temporary was left behind",
			entries,
		)
	}
}

// A directory handed in among the files is recorded as a directory, and the untouched tree still hits.
//
// The input recorder reports every path the compiler touched, and the compiler lists and probes
// directories as well as reading files. An earlier Record stamped every entry of files as a file, so a
// directory arrived as one and every check reported it "changed between file and directory". The cache
// then never hit, which no other test here could see: they all handed in files only. Found by running
// the untouched-tree check against the real ahra tree.
func TestRunCacheRecordsADirectoryAmongTheFilesAsADirectory(test *testing.T) {
	test.Parallel()
	tree := newRunCacheTree(test)
	withDirectory := append(append([]string(nil), tree.files...), tree.subdir, tree.root)
	cache, recordRunCacheError := program.RecordRunCache(
		tree.key,
		withDirectory,
		nil,
		tree.absent,
		nil,
		[]byte("x"),
		0,
		time.Time{},
	)
	if recordRunCacheError != nil {
		test.Fatalf("record: %v", recordRunCacheError)
	}
	if checkError := cache.Check(tree.key); checkError != nil {
		test.Fatalf("an untouched tree missed because a directory was recorded as a file: %v", checkError)
	}
	seen := map[string]int{}
	for _, input := range cache.Inputs {
		seen[input.Path]++
	}
	for path, count := range seen {
		if count > 1 {
			test.Errorf("%s recorded %d times, so every check stats it %d times", path, count, count)
		}
	}
}

// A fact the key ignores is a change the cache cannot see. The command hashes what git reports as
// changed into a fact, so a commit or a new untracked file must move the key even when no source file
// the build read has changed.
func TestRunCacheKeyCoversEveryFact(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	base, runCacheKeyError := program.RunCacheKey(nil, root, "root=/a", "scope=x")
	if runCacheKeyError != nil {
		test.Fatalf("key: %v", runCacheKeyError)
	}
	for name, facts := range map[string][]string{
		"a fact changed":            {"root=/a", "scope=y"},
		"a fact removed":            {"root=/a"},
		"a fact added":              {"root=/a", "scope=x", "extra"},
		"facts reordered":           {"scope=x", "root=/a"},
		"two facts joined into one": {"root=/ascope=x"},
	} {
		key, runCacheKeyError := program.RunCacheKey(nil, root, facts...)
		if runCacheKeyError != nil {
			test.Fatalf("%s: %v", name, runCacheKeyError)
		}
		if key == base {
			test.Errorf("%s left the key unchanged, so that change would replay a stale run", name)
		}
	}
}

// The record stats its inputs across workers, and that changes nothing a serial record would give: the same
// entries in the same order (files sorted, then their directories, then what was absent), each with its own
// signature, and a path that vanished before the record is still an error rather than an absence (#a66sfmh).
func TestRunCacheRecordsInParallelInTheSerialOrder(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	var files []string
	for directory := range 12 {
		for file := range 60 {
			path := filepath.Join(root, fmt.Sprintf("d%02d", directory), fmt.Sprintf("f%02d.ts", file))
			if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
				test.Fatal(createDirectoryError)
			}
			if writeError := os.WriteFile(
				path,
				[]byte(strings.Repeat("x", directory*60+file)),
				0o644,
			); writeError != nil {
				test.Fatal(writeError)
			}
			files = append(files, path)
		}
	}
	shuffled := append([]string(nil), files...)
	for index := range shuffled {
		other := (index * 7919) % len(shuffled)
		shuffled[index], shuffled[other] = shuffled[other], shuffled[index]
	}
	absent := []string{filepath.Join(root, "missing.ts")}
	cache, recordRunCacheError := program.RecordRunCache("key", shuffled, nil, absent, nil, nil, 0, time.Time{})
	if recordRunCacheError != nil {
		test.Fatal(recordRunCacheError)
	}

	var want []string
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	want = append(want, sorted...)
	for directory := range 12 {
		want = append(want, filepath.Join(root, fmt.Sprintf("d%02d", directory)))
	}
	want = append(want, absent...)
	if len(cache.Inputs) != len(want) {
		test.Fatalf("%d inputs recorded, want %d", len(cache.Inputs), len(want))
	}
	for index, input := range cache.Inputs {
		if input.Path != want[index] {
			test.Fatalf("input %d is %s, want %s: the order is not the serial one", index, input.Path, want[index])
		}
		information, statError := os.Stat(input.Path)
		switch {
		case input.Path == absent[0]:
			if input.Exists {
				test.Errorf("%s is recorded present", input.Path)
			}
		case statError != nil ||
			!input.Exists ||
			input.Size != information.Size() ||
			input.Directory != information.IsDir() ||
			input.ModifiedNanoseconds != information.ModTime().UnixNano():
			test.Errorf("%s is recorded with another file's signature: %+v", input.Path, input)
		}
	}

	if removeError := os.Remove(files[len(files)/2]); removeError != nil {
		test.Fatal(removeError)
	}
	if _, recordRunCacheError := program.RecordRunCache(
		"key",
		shuffled,
		nil,
		absent,
		nil,
		nil,
		0,
		time.Time{},
	); recordRunCacheError == nil {
		test.Error("a file that vanished before the record was recorded anyway")
	}
}
