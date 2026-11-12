package cache

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Store is the directory of entries, one file per key under a directory named for the key's first two hex
// characters, so no one directory holds them all. Each file is the sha256 of the entry, then the entry: a file
// cut short, or written over by something else, no longer matches its sum, and reads as a miss.
type Store struct {
	directory string

	// made holds each shard directory this store has made or found, by path, so a Put stats its directory once per
	// store rather than once per entry: a cold run at cohere's root writes about 31,000 files into 256 directories,
	// and a MkdirAll before each cost 2.5 s of stat and lstat calls summed (#m8hcs4n).
	made sync.Map
}

// Open is the store under root, at `<root>/.cache/cohere/go`, beside the TypeScript cache in `.cache/cohere`.
// Nothing is made until the first Put.
func Open(root string) *Store {
	return &Store{directory: filepath.Join(root, ".cache", "cohere", "go")}
}

// Get is the entry under key. Any failure to read it whole is a miss, never an error: the package is analyzed
// again and the entry written over.
func (store *Store) Get(key Key) ([]byte, bool) {
	contents, readError := os.ReadFile(store.path(key))
	if readError != nil || len(contents) < sha256.Size {
		return nil, false
	}
	entry := contents[sha256.Size:]
	sum := sha256.Sum256(entry)
	if !bytes.Equal(contents[:sha256.Size], sum[:]) {
		return nil, false
	}
	return entry, true
}

// Put keeps entry under key. It writes a temporary file beside the entry's and renames it into place, so a reader,
// or another Put of the same key, sees a whole file or none.
func (store *Store) Put(key Key, entry []byte) error {
	path := store.path(key)
	directory := filepath.Dir(path)
	if _, known := store.made.Load(directory); !known {
		if makeError := store.makeDirectory(directory); makeError != nil {
			return makeError
		}
	}
	temporary, createError := os.CreateTemp(directory, filepath.Base(path)+".*.temporary")
	// A directory removed while the store runs, by someone clearing the cache, is made again once.
	if errors.Is(createError, fs.ErrNotExist) {
		if makeError := store.makeDirectory(directory); makeError != nil {
			return makeError
		}
		temporary, createError = os.CreateTemp(directory, filepath.Base(path)+".*.temporary")
	}
	if createError != nil {
		return fmt.Errorf("creating a temporary entry in %s: %w", directory, createError)
	}
	sum := sha256.Sum256(entry)
	_, writeSumError := temporary.Write(sum[:])
	_, writeEntryError := temporary.Write(entry)
	closeError := temporary.Close()
	if writeError := errors.Join(writeSumError, writeEntryError, closeError); writeError != nil {
		// Ignored: the write already failed, and a temporary file left behind is never read as an entry.
		_ = os.Remove(temporary.Name())
		return fmt.Errorf("writing the entry %s: %w", temporary.Name(), writeError)
	}
	if renameError := os.Rename(temporary.Name(), path); renameError != nil {
		// Ignored: the rename already failed, and a temporary file left behind is never read as an entry.
		_ = os.Remove(temporary.Name())
		return fmt.Errorf("renaming the entry into place at %s: %w", path, renameError)
	}
	return nil
}

// makeDirectory makes directory and every parent it needs, and remembers it made.
func (store *Store) makeDirectory(directory string) error {
	if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
		return fmt.Errorf("making the cache directory %s: %w", directory, makeError)
	}
	store.made.Store(directory, true)
	return nil
}

// path is where key's entry lives: `ab/abcdef...`.
func (store *Store) path(key Key) string {
	name := key.String()
	return filepath.Join(store.directory, name[:2], name)
}
