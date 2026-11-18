package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A store opened at a directory keeps its entries exactly there, makes nothing until the first Put, and writes no
// ignore file in that directory or its parent: the system cache is in no repository, and the directory's parent is
// not the store's to write into.
func TestAStoreOpenedAtADirectoryWritesOnlyThere(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	directory := filepath.Join(root, "go", "standard-library")
	store := OpenAt(directory, nil)
	if _, found := store.Get(sampleKey); found {
		test.Fatal("an empty store had an entry")
	}
	if _, statError := os.Lstat(filepath.Join(root, "go")); !os.IsNotExist(statError) {
		test.Fatalf("opening and reading the store made its directory: %v", statError)
	}

	entry := []byte("a standard library table")
	if putError := store.Put(sampleKey, entry); putError != nil {
		test.Fatal(putError)
	}
	if _, statError := os.Stat(filepath.Join(directory, "ab", sampleKey.String())); statError != nil {
		test.Errorf("the entry is not under the directory given: %v", statError)
	}
	if got, found := store.Get(sampleKey); !found || !bytes.Equal(got, entry) {
		test.Errorf("want %q back, got %q (found %v)", entry, got, found)
	}
	for _, ignoreFile := range []string{
		filepath.Join(directory, ".gitignore"),
		filepath.Join(root, "go", ".gitignore"),
		filepath.Join(root, ".gitignore"),
	} {
		if _, statError := os.Lstat(ignoreFile); !os.IsNotExist(statError) {
			test.Errorf("a store opened at a directory wrote %s (%v)", ignoreFile, statError)
		}
	}

	// A directory removed while the store runs is made again by the next Put, still without an ignore file.
	if removeError := os.RemoveAll(filepath.Join(root, "go")); removeError != nil {
		test.Fatal(removeError)
	}
	if putError := store.Put(sampleKey, entry); putError != nil {
		test.Fatalf("a Put after the directory was removed failed: %v", putError)
	}
	if _, statError := os.Lstat(filepath.Join(root, "go", ".gitignore")); !os.IsNotExist(statError) {
		test.Errorf("a store opened at a directory wrote an ignore file making it again (%v)", statError)
	}
}
