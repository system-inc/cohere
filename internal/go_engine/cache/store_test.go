package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/cache_directory"
)

// sampleKey is a key whose entry's path is known: its first byte names its directory.
var sampleKey = Key{0xAB, 0xCD, 0xEF}

// An entry put is got back whole, under `.cache/cohere/go/ab/abcdef...`, and the store makes nothing on disk until
// the first Put. An empty entry is an entry.
func TestAnEntryPutIsGotBack(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := Open(root)
	if _, found := store.Get(sampleKey); found {
		test.Fatal("an empty store had an entry")
	}
	if _, statError := os.Stat(filepath.Join(root, ".cache")); !os.IsNotExist(statError) {
		test.Fatalf("opening and reading the store made .cache: %v", statError)
	}

	entry := []byte("findings and facts")
	if putError := store.Put(sampleKey, entry); putError != nil {
		test.Fatal(putError)
	}
	entryPath := filepath.Join(root, ".cache", "cohere", "go", "ab", sampleKey.String())
	if _, statError := os.Stat(entryPath); statError != nil {
		test.Errorf("the entry is not where the store keeps it: %v", statError)
	}
	got, found := store.Get(sampleKey)
	if !found || !bytes.Equal(got, entry) {
		test.Errorf("want %q back, got %q (found %v)", entry, got, found)
	}

	emptyKey := Key{0x01}
	if putError := store.Put(emptyKey, nil); putError != nil {
		test.Fatal(putError)
	}
	if got, found := store.Get(emptyKey); !found || len(got) != 0 {
		test.Errorf("want the empty entry back, got %q (found %v)", got, found)
	}
}

// An entry file cut short, changed, unreadable or not a file at all is a miss, never an error.
func TestABrokenEntryIsAMiss(test *testing.T) {
	test.Parallel()

	for name, breakEntry := range map[string]func(path string) error{
		"cut short":      func(path string) error { return os.Truncate(path, 40) },
		"cut in its sum": func(path string) error { return os.Truncate(path, 10) },
		"emptied":        func(path string) error { return os.Truncate(path, 0) },
		"unreadable":     func(path string) error { return os.Chmod(path, 0o000) },
		"a directory":    func(path string) error { return replaceWithDirectory(path) },
		"a byte changed": func(path string) error { return changeLastByte(path) },
		"removed":        func(path string) error { return os.Remove(path) },
		"written over":   func(path string) error { return os.WriteFile(path, []byte("short"), 0o644) },
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			store := Open(test.TempDir())
			if putError := store.Put(sampleKey, bytes.Repeat([]byte("entry "), 20)); putError != nil {
				test.Fatal(putError)
			}
			if breakError := breakEntry(store.path(sampleKey)); breakError != nil {
				test.Fatal(breakError)
			}
			if got, found := store.Get(sampleKey); found {
				test.Errorf("a broken entry was found: %q", got)
			}
		})
	}
}

// replaceWithDirectory puts a directory where the file at path was.
func replaceWithDirectory(path string) error {
	if removeError := os.Remove(path); removeError != nil {
		return removeError
	}
	return os.Mkdir(path, 0o755)
}

// changeLastByte changes the file's last byte, so its length is right and its bytes are not.
func changeLastByte(path string) error {
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return readError
	}
	contents[len(contents)-1]++
	return os.WriteFile(path, contents, 0o644)
}

// Many Puts of one key at once leave one of their entries whole, and no temporary file behind.
func TestConcurrentPutsOfOneKeyLeaveAWholeEntry(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir())
	const writers = 16
	const size = 1 << 20
	putErrors := make([]error, writers)
	var started sync.WaitGroup
	for writer := range writers {
		started.Go(func() {
			putErrors[writer] = store.Put(sampleKey, bytes.Repeat([]byte{byte(writer)}, size))
		})
	}
	started.Wait()
	for writer, putError := range putErrors {
		if putError != nil {
			test.Errorf("Put %d: %v", writer, putError)
		}
	}
	got, found := store.Get(sampleKey)
	if !found || len(got) != size || !bytes.Equal(got, bytes.Repeat(got[:1], size)) {
		test.Fatalf("want one writer's whole entry, got %d bytes (found %v)", len(got), found)
	}
	entries, readError := os.ReadDir(filepath.Dir(store.path(sampleKey)))
	if readError != nil {
		test.Fatal(readError)
	}
	if len(entries) != 1 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		test.Errorf("want the entry alone in its directory, got %s", strings.Join(names, ", "))
	}
}

// A Put that cannot write is an error, and leaves no entry.
func TestAPutThatCannotWriteIsAnError(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	// A file where the store's directory goes, so no directory can be made under it.
	if writeError := os.WriteFile(filepath.Join(root, ".cache"), nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	store := Open(root)
	if putError := store.Put(sampleKey, []byte("entry")); putError == nil {
		test.Error("a Put under a file succeeded")
	}
	if _, found := store.Get(sampleKey); found {
		test.Error("a failed Put left an entry")
	}
}

// A store remembers each shard directory it made, so a Put stats its directory once per store rather than once per
// entry (#m8hcs4n): after puts into two shards, many into one, it holds exactly those two.
func TestAStoreMakesEachShardDirectoryOnce(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir())
	for index := range 20 {
		if putError := store.Put(Key{0xAB, byte(index)}, []byte("entry")); putError != nil {
			test.Fatal(putError)
		}
	}
	if putError := store.Put(Key{0x01}, []byte("entry")); putError != nil {
		test.Fatal(putError)
	}
	var made []string
	store.made.Range(func(directory any, _ any) bool {
		path, isPath := directory.(string)
		if !isPath {
			test.Errorf("the store remembers %v, a %T, want a directory's path", directory, directory)
			return true
		}
		made = append(made, filepath.Base(path))
		return true
	})
	slices.Sort(made)
	if want := []string{"01", "ab"}; !slices.Equal(made, want) {
		test.Errorf("the store remembers the directories %v, want %v", made, want)
	}
}

// A shard directory removed while the store runs, as someone clearing the cache would, is made again by the next Put
// into it, which writes its entry rather than fail on the directory the store remembered (#m8hcs4n).
func TestAShardDirectoryRemovedUnderTheStoreIsMadeAgain(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := Open(root)
	if putError := store.Put(sampleKey, []byte("first")); putError != nil {
		test.Fatal(putError)
	}
	shard := filepath.Join(root, ".cache", "cohere", "go", "ab")
	if removeError := os.RemoveAll(shard); removeError != nil {
		test.Fatal(removeError)
	}
	second := Key{0xAB, 0x01}
	if putError := store.Put(second, []byte("second")); putError != nil {
		test.Fatalf("a Put into a removed shard directory failed: %v", putError)
	}
	if got, found := store.Get(second); !found || string(got) != "second" {
		test.Errorf("want the second entry back, got %q (found %v)", got, found)
	}
}

// The store that creates the project's `.cache/cohere` writes the ignore file there, so an engine run on its own leaves
// no untracked cache in a repository whose rules say nothing of it (#6vhp0vd). A cache directory cleared whole while
// the store runs is made again with its file, and one that was there already is left without one.
func TestAStoreThatMakesTheCacheDirectoryLeavesItIgnoringItself(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := Open(root)
	if putError := store.Put(sampleKey, []byte("first")); putError != nil {
		test.Fatal(putError)
	}
	ignoreFile := filepath.Join(cache_directory.Of(root), ".gitignore")
	if contents, readError := os.ReadFile(ignoreFile); readError != nil ||
		string(contents) != cache_directory.IgnoreFileContents {
		test.Fatalf("the store made the cache directory without its ignore file: %q (%v)", contents, readError)
	}

	if removeError := os.RemoveAll(filepath.Join(root, ".cache")); removeError != nil {
		test.Fatal(removeError)
	}
	if putError := store.Put(sampleKey, []byte("again")); putError != nil {
		test.Fatalf("a Put after the cache was cleared failed: %v", putError)
	}
	if _, statError := os.Stat(ignoreFile); statError != nil {
		test.Errorf("the cache directory made again has no ignore file: %v", statError)
	}

	existing := test.TempDir()
	if createDirectoryError := os.MkdirAll(cache_directory.Of(existing), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if putError := Open(existing).Put(sampleKey, []byte("entry")); putError != nil {
		test.Fatal(putError)
	}
	if _, statError := os.Lstat(filepath.Join(cache_directory.Of(existing), ".gitignore")); !os.IsNotExist(statError) {
		test.Errorf("the store wrote an ignore file in a cache directory it did not make (%v)", statError)
	}
}
