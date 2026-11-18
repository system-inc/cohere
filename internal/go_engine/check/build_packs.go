package check

import (
	"sync/atomic"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// buildPacks are an extra build's session's packs, one for each store it reads records from, opened once a run.
type buildPacks struct {
	// project is the project the build checks, which names the system cache's pack beside the build, since every
	// project on the machine reads that store and each reaches its own part of the standard library.
	project string

	// stamps are the run's shard stamps, which every build's session of the run shares, so each shard of each store is
	// read once a run, and each judged trusted or not as it is read (cache.ShardStamps).
	stamps *cache.ShardStamps

	// opened is set once the first Restore has read the packs; a reload's Restore serves from what that read.
	opened bool

	// byStore is each store's pack, in the order the packs were opened, the project's first.
	byStore []*buildPack

	// served, stale and missed count the keys entryAt served from a pack, those a pack held whose shard's stamp was not
	// the one the pack holds, and those no pack held, both read from the store, and shardReads the shard stamps this
	// session read, rather than another build's of the run, from every worker of readHits at once.
	served     atomic.Int64
	stale      atomic.Int64
	missed     atomic.Int64
	shardReads atomic.Int64

	// counts are the reads, misses, refusals and writes, which only Restore and save make, one at a time.
	counts PackCounts
}

// open reads store's pack named name into the packs, and counts it read, missing or refused.
func (packs *buildPacks) open(store *cache.Store, system bool, name cache.Key) {
	read, refused := store.ReadPack(name)
	pack := &buildPack{store: store, name: name, system: system, served: read.Entries, stamps: read.Stamps,
		held: make(map[cache.Key]cache.PackEntry, len(read.Entries)), changed: refused != ""}
	packs.byStore = append(packs.byStore, pack)
	switch refused {
	case "":
		packs.counts.Read++
		if system {
			store.MarkPackUsed(name)
		}
	case cache.PackMissing:
		// No refusal: a first run, or a new engine's, has none to read.
		packs.counts.Missing++
	default:
		packs.counts.RefusedBy[refused]++
	}
}

// of is store's pack, nil when the session keeps no packs or none of store.
func (packs *buildPacks) of(store *cache.Store) *buildPack {
	if packs == nil {
		return nil
	}
	for _, pack := range packs.byStore {
		if pack.store == store {
			return pack
		}
	}
	return nil
}

// stamp is shard's stamp in store as the run read it, counting the read when this asked it.
func (packs *buildPacks) stamp(store *cache.Store, shard byte) cache.ShardReading {
	reading, read := packs.stamps.Of(store, shard)
	if read {
		packs.shardReads.Add(1)
	}
	return reading
}

// fresh reports whether every shard of shards, which an entry of pack was read from, holds the stamp pack holds of it,
// so the store holds exactly what the pack was made of there, and the entry may be served.
func (packs *buildPacks) fresh(pack *buildPack, shards []byte) bool {
	for _, shard := range shards {
		recorded, trusted := pack.stamps[shard]
		if !trusted {
			return false
		}
		// The pack records only a stamp trusted as it was read, more than racyStampWindow behind the clock, so no
		// write since can have left the shard's time where it was, and an equal stamp is an unchanged shard.
		if reading := packs.stamp(pack.store, shard); !reading.Known || reading.Stamp != recorded {
			return false
		}
	}
	return true
}
