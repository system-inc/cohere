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

	"github.com/system-inc/cohere/internal/cache_directory"
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

	// cacheRoot is made once per store, before the first shard, through cache_directory.Make, so a store that
	// creates the project's `.cache/cohere` leaves it ignoring itself (#6vhp0vd).
	cacheRoot sync.Once
	// cacheRootError is the error making the cache root ended with, which every Put that needs it returns.
	cacheRootError error
}

// Open is the store under root, at `<root>/.cache/cohere/go`, beside the TypeScript cache in `.cache/cohere`.
// Nothing is made until the first Put.
func Open(root string) *Store {
	return &Store{directory: filepath.Join(cache_directory.Of(root), "go")}
}

// Get is the entry under key. Any failure to read it whole is a miss, never an error: the package is analyzed
// again and the entry written over. A file that is there but not whole, cut short or written over, is removed, so a
// PutAbsent of the same key, which leaves a file that is there alone, writes it again rather than keep the damage.
func (store *Store) Get(key Key) ([]byte, bool) {
	path := store.path(key)
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return nil, false
	}
	if len(contents) < sha256.Size {
		store.forget(path)
		return nil, false
	}
	entry := contents[sha256.Size:]
	sum := sha256.Sum256(entry)
	if !bytes.Equal(contents[:sha256.Size], sum[:]) {
		store.forget(path)
		return nil, false
	}
	return entry, true
}

// forget removes the damaged file at path. A Put that renamed a whole file into place since it was read may be
// removed with it, which leaves a miss, never a wrong read.
func (store *Store) forget(path string) {
	// Ignored: a file that cannot be removed reads as a miss again, as it just did.
	_ = os.Remove(path)
}

// Holds reports whether a file is under key, whole or not: a reader that finds it damaged removes it (Get), so a
// writer that names it rather than write it names it at worst as a miss.
func (store *Store) Holds(key Key) bool {
	information, statError := os.Stat(store.path(key))
	return statError == nil && information.Mode().IsRegular()
}

// PutAbsent keeps entry under key unless a file of exactly its size is there already, for a key named by its entry's
// own digest, whose file holds the same bytes whoever wrote it: an export data's (#5s1x2hg). A file of that size that
// is not whole reads as a miss, and Get removes it, so the next PutAbsent writes it again. Two writers of one key at
// once both write, and the rename leaves one whole file.
func (store *Store) PutAbsent(key Key, entry []byte) error {
	if information, statError := os.Stat(store.path(key)); statError == nil &&
		information.Size() == int64(sha256.Size+len(entry)) {
		return nil
	}
	return store.Put(key, entry)
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
	// A directory removed while the store runs, by someone clearing the cache, is made again once, the cache root
	// through Make again so a root this store makes a second time ignores itself too.
	if errors.Is(createError, fs.ErrNotExist) {
		if makeError := cache_directory.Make(filepath.Dir(store.directory)); makeError != nil {
			return fmt.Errorf("making the cache directory %s: %w", filepath.Dir(store.directory), makeError)
		}
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

// makeDirectory makes directory and every parent it needs, and remembers it made. The project's cache directory, the
// store's parent, is made first by cache_directory.Make, so the run that creates it writes its ignore file.
func (store *Store) makeDirectory(directory string) error {
	store.cacheRoot.Do(func() {
		store.cacheRootError = cache_directory.Make(filepath.Dir(store.directory))
	})
	if store.cacheRootError != nil {
		return fmt.Errorf("making the cache directory %s: %w", filepath.Dir(store.directory), store.cacheRootError)
	}
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
