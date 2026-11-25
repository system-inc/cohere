package load

import (
	"crypto/sha256"
	"slices"
	"sync"
)

// memoryStore is a ListingStore in memory, for a test that reads what a load kept, under lock, since a lister's
// commands run at once.
type memoryStore struct {
	lock    sync.Mutex
	entries map[[sha256.Size]byte][]byte
}

// newMemoryStore is an empty memoryStore.
func newMemoryStore() *memoryStore {
	return &memoryStore{entries: map[[sha256.Size]byte][]byte{}}
}

// Kept is what the store holds under key.
func (store *memoryStore) Kept(key [sha256.Size]byte) ([]byte, bool) {
	store.lock.Lock()
	defer store.lock.Unlock()
	contents, stored := store.entries[key]
	return contents, stored
}

// Keep holds a copy of contents under key.
func (store *memoryStore) Keep(key [sha256.Size]byte, contents []byte) error {
	store.lock.Lock()
	defer store.lock.Unlock()
	store.entries[key] = slices.Clone(contents)
	return nil
}

// count is how many entries the store holds.
func (store *memoryStore) count() int {
	store.lock.Lock()
	defer store.lock.Unlock()
	return len(store.entries)
}
