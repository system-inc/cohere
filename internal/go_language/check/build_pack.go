package check

import (
	"bytes"
	"sync"

	"github.com/system-inc/cohere/internal/go_language/cache"
)

// buildPack is one store's pack of an extra build's session: what it held when read, and what the run read and wrote
// of that store's records, which the next pack holds.
type buildPack struct {
	store *cache.Store
	place cache.PackPlace

	// system is whether the pack is the system cache's, whose write failing is why the run could not keep the system
	// cache, never a problem of the project's store (systemCacheFailed).
	system bool

	// served are the entries the pack held when read, by the key each was asked by, and stamps the stamp it holds of
	// each shard they were read from, both nil for a pack missing or refused. Read only after open, so readHits'
	// workers read them without the lock.
	served map[cache.Key]cache.PackEntry
	stamps map[byte]int64

	// lock guards held and changed, which readHits' workers and the save's writers add to at once.
	lock sync.Mutex
	// held are the entries the run read or wrote of the store, by the key each was asked by: what the next pack holds.
	held map[cache.Key]cache.PackEntry
	// changed is set when the run read or wrote an entry the pack did not hold as it was, or the pack was missing or
	// refused, and cleared once the pack is written, so a save after a reload writes it again only when something
	// changed since. A shard's stamp that moved changes the pack too, which the save finds (writePacks).
	changed bool
}

// hold has the next pack hold entry, which the pack served as it held it.
func (pack *buildPack) hold(entry cache.PackEntry) {
	pack.lock.Lock()
	defer pack.lock.Unlock()
	pack.held[entry.Asked] = entry
}

// keep adds entry, read from the store or written to it, to what the next pack holds, and marks the pack changed when
// it did not hold entry as it is: a record written again with more answers under a key the pack served is changed.
func (pack *buildPack) keep(entry cache.PackEntry) {
	if pack == nil {
		return
	}
	pack.lock.Lock()
	defer pack.lock.Unlock()
	pack.held[entry.Asked] = entry
	if was, held := pack.served[entry.Asked]; !held || was.Stored != entry.Stored ||
		!bytes.Equal(was.Entry, entry.Entry) {
		pack.changed = true
	}
}
