package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

// This file stands in for hand A's store, as key.go says, and is replaced by hand A's when it lands.

// Store is the directory of entries.
type Store struct {
	directory string
}

// Open is the store under root, `<root>/.cache/cohere/go`, created on the first Put.
func Open(root string) *Store {
	return &Store{directory: filepath.Join(root, ".cache", "cohere", "go")}
}

// Get is the entry stored under key. Any read error is a miss.
func (store *Store) Get(key Key) ([]byte, bool) {
	contents, readError := os.ReadFile(filepath.Join(store.directory, key.String()))
	if readError != nil {
		return nil, false
	}
	return contents, true
}

// Put stores entry under key: written to a temporary file, then renamed into place, so a reader never sees half.
func (store *Store) Put(key Key, entry []byte) error {
	if makeError := os.MkdirAll(store.directory, 0o755); makeError != nil {
		return fmt.Errorf("creating %s: %w", store.directory, makeError)
	}
	temporary, createError := os.CreateTemp(store.directory, key.String()+".*.tmp")
	if createError != nil {
		return fmt.Errorf("creating an entry in %s: %w", store.directory, createError)
	}
	_, writeError := temporary.Write(entry)
	closeError := temporary.Close()
	if writeError == nil {
		writeError = closeError
	}
	if writeError == nil {
		writeError = os.Rename(temporary.Name(), filepath.Join(store.directory, key.String()))
	}
	if writeError != nil {
		// Ignored: the temporary file is the only thing left to undo, and the write error is what is returned.
		_ = os.Remove(temporary.Name())
		return fmt.Errorf("writing the entry %s: %w", key, writeError)
	}
	return nil
}
