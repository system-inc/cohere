package check

import (
	"fmt"
	"maps"
	"slices"

	"github.com/system-inc/cohere/internal/go_language/cache"
)

// An extra build's packs (#b2t9xmf). Each extra build's session reads, before it reads its hits, one pack of each store
// it reads records from (storeFor), the project's and the system cache's, and serves each key the pack holds from it
// (entryAt), so a warm build reads two files rather than open every record it hits: about 1,560 a build at cohere's
// root, whose opens were most of the extra builds' kernel time. A pack serves a key only while the store holds exactly
// what the pack was made of: it holds the stamp of each shard directory its entries were read from, taken before they
// were read, and an entry whose shard's stamp has moved since, as every write, removal or trim of a file there moves
// it, is read from the store, as a run with no pack reads it, and so is one whose shard a store of the run has changed
// since the run read its stamp (cache.ShardStamps). Everything past entryAt reads as before, the record's
// body by BodyAt and its export data by digest from the store, so a record a pack serves whose body or export data the
// store has lost since is a miss, as it is read from the store. The pack is written at the save when the run read or
// wrote a record it did not hold, a shard's stamp moved, or it was missing or refused, so a warm run whose pack served
// everything writes nothing. The host's session keeps none. The project's packs are kept by executable identity, and
// the run's first write of one removes the packs of every other executable but the two most recently used
// (cache.Store.RemoveOtherIdentitiesPacks), so two executables taking turns over one tree each find their own.

// PackCounts is what an extra build's session did with its packs over every load of the run: the packs read whole and
// those missing, the records they served, those they held whose shard had moved, read from the store, the keys asked
// that no pack held, read from the store found or not, the shard stamps the session read, each pack refused by the
// first word of why (cache.PackSum and the rest), and the packs written.
type PackCounts struct {
	Read          int
	Missing       int
	RecordsServed int
	RecordsStale  int
	RecordsMissed int
	ShardReads    int
	RefusedBy     map[string]int
	Written       int
}

// KeepBuildPacks has the session, an extra build's, read and write a pack of each store it reads records from, the
// project's and the system cache's, named by the build and cohere-go, and the system cache's by project, the
// project's root, too (#b2t9xmf). Each store's pack is judged by the run's shard stamps the store holds (cache.Open),
// which every build of the run shares, and a store holding none keeps no pack.
func (session *CacheSession) KeepBuildPacks(project string) {
	session.packs = &buildPacks{project: project}
}

// openPacks reads the pack of each store the session reads records from, once a run, before its first Restore reads
// its hits: the project's store, and the system cache's when the session keeps it and keyed the standard library,
// each only when it holds the run's shard stamps, without which no write of the run would mark a shard moved. A pack
// missing or refused serves nothing and is written at the save; one read is marked used, in the system cache's store
// as each entry read there is, so its trim keeps it, and in the project's, so a sweep there keeps it (buildPacks.open).
func (session *CacheSession) openPacks(loader string) {
	packs := session.packs
	if packs == nil || packs.opened {
		return
	}
	packs.opened = true
	packs.counts.RefusedBy = map[string]int{}
	if store := session.caching.Store; store != nil && store.ShardStamps() != nil {
		keyContext := session.keyContextOf(loader)
		// The project's pack is named by every part of the context its keys hold, so another build, rule set or
		// executable never reads it, and a run with another loader has its own.
		packs.open(store, false, cache.PackPlace{Identity: cache.PackIdentity(keyContext.ExecutableIdentity),
			Name: cache.PackName([]string{"project"}, []string{keyContext.FormatVersion, keyContext.ExecutableIdentity},
				keyContext.Build, keyContext.RuleSet)})
	}
	if session.KeepsSystemCache() && session.standardLibraryContext != nil &&
		session.caching.StandardLibrary.System.ShardStamps() != nil {
		packs.open(session.caching.StandardLibrary.System, true,
			systemPackPlace(*session.standardLibraryContext, packs.project))
	}
}

// systemPackPlace is where the system cache's pack of a build is: named by every part of the standard library's
// context its keys hold, so a build under another context never reads it, and by the project, so two projects on the
// machine checking the same build never write over each other's pack each run.
func systemPackPlace(context cache.StandardLibraryContext, project string) cache.PackPlace {
	return cache.PackPlace{
		Identity: cache.PackIdentity(context.ExecutableIdentity),
		Name: cache.PackName([]string{"system", project},
			[]string{context.FormatVersion, context.ExecutableIdentity, context.Loader, context.Goroot}, context.Build,
			context.Facts, context.CgoToolchain),
	}
}

// entryAt is the entry store holds under key, or, when there is none, the entry the alias under key names, with the
// key it is stored under: from store's pack when it holds key and every shard it was read from holds the stamp the pack
// holds of it (fresh), and otherwise from the store, which the next pack then holds. Only the project's store has
// aliases: the system cache's store holds the standard library under first keys alone (keyStandardLibraryApart).
func (session *CacheSession) entryAt(store *cache.Store, key cache.Key) ([]byte, cache.Key, bool) {
	pack := session.packs.of(store)
	if pack == nil {
		return session.storedAt(store, key)
	}
	if entry, held := pack.served[key]; held {
		if session.packs.fresh(pack, session.shardsOf(entry)) {
			pack.hold(entry)
			session.packs.served.Add(1)
			return entry.Entry, entry.Stored, true
		}
		session.packs.stale.Add(1)
	} else {
		session.packs.missed.Add(1)
	}
	contents, storedKey, found := session.storedAt(store, key)
	// Only what was found is kept: a key missing from the store adds nothing to the pack.
	if found {
		pack.keep(cache.PackEntry{Asked: key, Stored: storedKey, Entry: contents})
	}
	return contents, storedKey, found
}

// shardsOf are the shards whose files entry was read from: its own key's, where the store looked first, the key it was
// stored under, and, for an entry read through an alias, the alias table's, so a change to any of them moves a stamp.
func (session *CacheSession) shardsOf(entry cache.PackEntry) []byte {
	shards := []byte{cache.ShardOf(entry.Asked), cache.ShardOf(entry.Stored)}
	if entry.Asked != entry.Stored && session.aliases != nil {
		shards = append(shards, cache.ShardOf(session.aliases.key))
	}
	return shards
}

// storedAt is entryAt read from the store alone: the entry under key, or the one the alias under key names. In a
// session with a pack of store, each shard is stamped before anything of it is read (beforeReading), so the stamp the
// next pack records of it is never newer than what it holds.
func (session *CacheSession) storedAt(store *cache.Store, key cache.Key) ([]byte, cache.Key, bool) {
	session.beforeReading(store, cache.ShardOf(key))
	if contents, stored := store.Get(key); stored {
		return contents, key, true
	}
	if session.aliases == nil || store != session.caching.Store {
		return nil, cache.Key{}, false
	}
	session.beforeReading(store, cache.ShardOf(session.aliases.key))
	storedKey, aliased := session.aliases.target(key)
	if !aliased {
		return nil, cache.Key{}, false
	}
	session.beforeReading(store, cache.ShardOf(storedKey))
	contents, stored := store.Get(storedKey)
	return contents, storedKey, stored
}

// beforeReading stamps shard of store, once a run, before the session reads a file of it, when the session keeps a
// pack of store.
func (session *CacheSession) beforeReading(store *cache.Store, shard byte) {
	if session.packs.of(store) != nil {
		session.packs.stamp(store, shard)
	}
}

// putRecord is putEntry, entry written under key in store, whose record the next pack of store holds once written.
// The key's shard is stamped before the write, as before a read (beforeReading), so its stamp is never one taken
// after the write: the write then marks it moved (cache.ShardStamps), and no pack records it this run. A stamp taken
// after would hold whatever else was written there since, another session's write of the same key among it, which the
// pack, holding this write's record, would then serve.
func (session *CacheSession) putRecord(store *cache.Store, key cache.Key, entry encodedEntry) error {
	session.beforeReading(store, cache.ShardOf(key))
	record, putError := putEntry(store, key, entry)
	if putError == nil {
		session.packs.of(store).keep(cache.PackEntry{Asked: key, Stored: key, Entry: record})
	}
	return putError
}

// writePacks writes each pack that changed, was missing or was refused, or whose shards' stamps moved, with what the
// run read and wrote of its store and the stamp of each shard it was read from, at the save, once every write of the
// save has run, and counts what the packs did over the run. Each shard's stamp is the run's, taken before anything of
// it was read or written (ShardStamps). A shard a store of the run has changed since, this session's writes included,
// is moved, and recorded as none, as is a stamp not trusted as it was read, within two seconds of the clock then
// (cache.ShardStamps): the next run reads that shard from the store. The first project pack the run writes removes
// the packs of every other executable but the two most recently used (cache.Store.RemoveOtherIdentitiesPacks).
// A pack that could not be written is a problem of its store, never a finding: the next run reads that store's records
// one by one, as it would with no pack.
func (session *CacheSession) writePacks() {
	packs := session.packs
	if packs == nil {
		return
	}
	for _, pack := range packs.byStore {
		pack.lock.Lock()
		entries := slices.Collect(maps.Values(pack.held))
		pack.lock.Unlock()
		stamps := map[byte]int64{}
		for _, entry := range entries {
			for _, shard := range session.shardsOf(entry) {
				if reading := packs.stamp(pack.store, shard); reading.Trusted && !reading.Moved {
					stamps[shard] = reading.Stamp
				}
			}
		}
		pack.lock.Lock()
		changed := pack.changed || !maps.Equal(stamps, pack.stamps)
		// Cleared before the write, under the lock, and set again when the write fails, so nothing kept meanwhile is
		// lost to a clear after it. Nothing is kept meanwhile today: every write of the save has run.
		pack.changed = false
		pack.lock.Unlock()
		if !changed {
			continue
		}
		writeError := pack.store.WritePack(pack.place, entries, stamps)
		if writeError == nil {
			packs.counts.Written++
			// The system cache's trim ages its packs out; nothing trims the project's but this.
			if !pack.system {
				pack.store.RemoveOtherIdentitiesPacks(pack.place.Identity)
			}
			// A reload's Restore serves from what this save wrote, and a save after it compares with it: the entries
			// and the stamps together, so no entry is ever served by a stamp taken of another's read.
			written := make(map[cache.Key]cache.PackEntry, len(entries))
			for _, entry := range entries {
				written[entry.Asked] = entry
			}
			pack.served, pack.stamps = written, stamps
			continue
		}
		pack.lock.Lock()
		pack.changed = true
		pack.lock.Unlock()
		if pack.system {
			session.systemCacheFailed(writeError)
			continue
		}
		session.problem(fmt.Sprintf("writing the build's pack: %v", writeError))
	}
	counts := packs.counts
	counts.RefusedBy = maps.Clone(packs.counts.RefusedBy)
	counts.RecordsServed, counts.RecordsStale = int(packs.served.Load()), int(packs.stale.Load())
	counts.RecordsMissed, counts.ShardReads = int(packs.missed.Load()), int(packs.shardReads.Load())
	session.counts.Packs = &counts
}
