package load

import (
	"crypto/sha256"
	"sync"
)

// listingMap is a ListingStore in memory.
type listingMap struct {
	lock     sync.Mutex
	listings map[[sha256.Size]byte][]byte
}

func (store *listingMap) Kept(key [sha256.Size]byte) ([]byte, bool) {
	store.lock.Lock()
	defer store.lock.Unlock()
	listing, held := store.listings[key]
	return listing, held
}

func (store *listingMap) Keep(key [sha256.Size]byte, listing []byte) error {
	store.lock.Lock()
	defer store.lock.Unlock()
	store.listings[key] = listing
	return nil
}
