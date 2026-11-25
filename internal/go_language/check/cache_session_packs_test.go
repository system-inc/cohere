package check

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rule"
)

// laterClock is a run's clock an hour ahead, so every shard stamp a test's store has is far enough behind its reading
// to be trusted (cache.ShardStamps), as a run's would be had the store been written a while before.
func laterClock() time.Time {
	return time.Now().Add(time.Hour)
}

// runStore is the project's store at root as a new run opens it, holding shard stamps of its own judged by clock.
func runStore(root string, clock func() time.Time) *cache.Store {
	return cache.Open(root, cache.NewShardStamps(clock))
}

// packedSession is an extra build's session over store, a run's, with its packs opened under the source loader, as
// the build's first Restore opens them before it reads its hits.
func packedSession(store *cache.Store) *CacheSession {
	session := NewCacheSession(&Caching{Store: store}, nil)
	session.KeepBuildPacks("project")
	session.openPacks("source")
	return session
}

// packedOnce is a run over store, a run's, that reads key through its pack, finding want, and writes its pack at its
// save, returning the pack's place.
func packedOnce(test *testing.T, store *cache.Store, key cache.Key, want string) cache.PackPlace {
	test.Helper()
	session := packedSession(store)
	if contents, _, found := session.entryAt(store, key); !found || string(contents) != want {
		test.Fatalf("the first run read %q (found %v), want %q", contents, found, want)
	}
	session.writePacks()
	if counts := session.counts.Packs; counts == nil || counts.Written != 1 {
		test.Fatalf("want the first run's pack written, got %+v", counts)
	}
	return session.packs.byStore[0].place
}

// A key the build's pack holds is served from it, and a key it does not hold is read from the store, never answered
// by another of the pack's entries: the store's entry under it, or a miss when the store holds none (#b2t9xmf). Only
// what was found is packed: a key missing from both adds nothing.
func TestAKeyThePackDoesNotHoldIsReadFromTheStore(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := cache.Open(root, nil)
	// packed shares stored's shard, which the store holds, so the shard has a stamp the pack can hold.
	packed, stored, absent := cache.Key{0x02, 0x01}, cache.Key{0x02}, cache.Key{0x03}
	if putError := store.Put(stored, []byte("the store's record")); putError != nil {
		test.Fatal(putError)
	}
	// The pack's place is the session's own, as the first session opened it and found none, which is no refusal.
	opening := packedSession(runStore(root, laterClock))
	if opening.packs.counts.Missing != 1 || len(opening.packs.counts.RefusedBy) != 0 {
		test.Fatalf("want the store's pack missing and nothing refused before one is written, got %+v",
			opening.packs.counts)
	}
	place := opening.packs.byStore[0].place
	// The pack holds packed, which the store does not, so what is served of it can only be the pack's, with the stamp
	// of its shard as it is.
	packedReading, _ := cache.NewShardStamps(nil).Of(store, cache.ShardOf(packed))
	if !packedReading.Known {
		test.Fatal("the store has no shard of the packed key")
	}
	if writeError := store.WritePack(place, []cache.PackEntry{
		{Asked: packed, Stored: packed, Entry: []byte("the pack's record")},
	}, map[byte]int64{cache.ShardOf(packed): packedReading.Stamp}); writeError != nil {
		test.Fatal(writeError)
	}

	session := packedSession(runStore(root, laterClock))
	for _, testCase := range []struct {
		key   cache.Key
		want  string
		found bool
	}{
		{key: packed, want: "the pack's record", found: true},
		{key: stored, want: "the store's record", found: true},
		{key: absent},
	} {
		contents, storedKey, found := session.entryAt(session.caching.Store, testCase.key)
		if found != testCase.found || !bytes.Equal(contents, []byte(testCase.want)) ||
			found && storedKey != testCase.key {
			test.Errorf("%x: want %q stored under itself (found %v), got %q under %x (found %v)", testCase.key[:1],
				testCase.want, testCase.found, contents, storedKey[:1], found)
		}
	}
	session.writePacks()
	counts := session.counts.Packs
	if counts == nil || counts.Read != 1 || counts.RecordsServed != 1 || counts.RecordsMissed != 2 ||
		counts.Written != 1 {
		test.Fatalf("want one pack read, one record served, two read from the store and the pack written again, "+
			"got %+v", counts)
	}
	// The pack written holds what the run found, the store's record beside the pack's, and nothing of absent.
	read, refused := store.ReadPack(place)
	if refused != "" || len(read.Entries) != 2 || string(read.Entries[stored].Entry) != "the store's record" ||
		string(read.Entries[packed].Entry) != "the pack's record" {
		test.Errorf("want the pack written with the two records found, got %d entries (refused %q)",
			len(read.Entries), refused)
	}
}

// A key the pack holds that another run has since written again, other bytes under the same key, is read from the
// store, exactly as a run with no pack reads it: the write moved its shard's stamp past the one the pack holds.
func TestAKeyWrittenAgainSinceThePackIsReadFromTheStore(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := cache.Open(root, nil)
	key := cache.Key{0x07}
	if putError := store.Put(key, []byte("the record the pack was made of")); putError != nil {
		test.Fatal(putError)
	}
	packedOnce(test, runStore(root, laterClock), key, "the record the pack was made of")
	if putError := store.Put(key, []byte("the record another run wrote")); putError != nil {
		test.Fatal(putError)
	}
	session := packedSession(runStore(root, laterClock))
	if contents, _, found := session.entryAt(session.caching.Store, key); !found ||
		string(contents) != "the record another run wrote" {
		test.Errorf("want the record the store holds now, got %q (found %v)", contents, found)
	}
	if served, stale := session.packs.served.Load(), session.packs.stale.Load(); served != 0 || stale != 1 {
		test.Errorf("want the record read from the store as stale, got %d served and %d stale", served, stale)
	}
}

// A key the pack holds that the system cache's trim has since deleted reads as a miss, exactly as with no pack: the
// trim's removal moved its shard's stamp.
func TestAKeyTrimmedSinceThePackReadsAsAMiss(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	directory := filepath.Join(root, "go", "store")
	store := cache.OpenAt(directory, nil)
	key := cache.Key{0x09}
	if putError := store.Put(key, []byte("a record no run read for days")); putError != nil {
		test.Fatal(putError)
	}
	packedOnce(test, cache.OpenAt(directory, cache.NewShardStamps(laterClock)), key, "a record no run read for days")
	aged := time.Now().Add(-6 * 24 * time.Hour)
	recordPath := filepath.Join(directory, key.String()[:2], key.String())
	if timesError := os.Chtimes(recordPath, aged, aged); timesError != nil {
		test.Fatal(timesError)
	}
	if notKept := cache.TrimSystemCache(root, directory, cache.SystemCacheSizeCap); notKept != nil {
		test.Fatalf("the trim failed: %s", notKept)
	}
	if _, statError := os.Stat(recordPath); !os.IsNotExist(statError) {
		test.Fatalf("the trim kept the record unread for six days: %v", statError)
	}
	session := packedSession(cache.OpenAt(directory, cache.NewShardStamps(laterClock)))
	if contents, _, found := session.entryAt(session.caching.Store, key); found {
		test.Errorf("want the trimmed record a miss, got %q", contents)
	}
}

// A shard whose stamp is within two seconds of the clock as the run reads it is never served from the pack, however
// long after the pack is written: a file system whose times are coarse leaves the shard's time in the second it was
// read across a write later in that second, which the stamp alone would then hide (#b2t9xmf). The run reads the stamp
// half a second into its tick, a write lands in the same tick after it, as such a file system holds it, and the pack is
// written three seconds on; the next run reads the key from the store. The clock is the test's.
func TestARacyShardIsNeverServed(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := cache.Open(root, nil)
	key := cache.Key{0x0B}
	if putError := store.Put(key, []byte("the record the pack was made of")); putError != nil {
		test.Fatal(putError)
	}
	tick := time.Unix(1_800_000_000, 0)
	shardDirectory := filepath.Join(root, ".cache", "cohere", "go", key.String()[:2])
	holdTick := func() {
		test.Helper()
		if timesError := os.Chtimes(shardDirectory, tick, tick); timesError != nil {
			test.Fatal(timesError)
		}
	}
	holdTick()
	// The stamp is read half a second into the tick, and every later reading of the clock is three seconds on, as a
	// pack written then would judge it were the guard keyed on the write.
	clockReadings := 0
	clock := func() time.Time {
		clockReadings++
		if clockReadings == 1 {
			return tick.Add(500 * time.Millisecond)
		}
		return tick.Add(3 * time.Second)
	}
	session := packedSession(runStore(root, clock))
	if contents, _, found := session.entryAt(session.caching.Store, key); !found ||
		string(contents) != "the record the pack was made of" {
		test.Fatalf("the first run read %q (found %v)", contents, found)
	}
	// Written again within the tick the stamp was read in, after the reading, by another run: the shard's time is where
	// it was, and no store of this run marks it moved.
	if putError := store.Put(key, []byte("the record written in the same tick")); putError != nil {
		test.Fatal(putError)
	}
	holdTick()
	session.writePacks()
	place := session.packs.byStore[0].place
	if read, _ := store.ReadPack(place); len(read.Stamps) != 0 {
		test.Errorf("want the stamp read half a second into its tick recorded as none, got %v", read.Stamps)
	}
	next := packedSession(runStore(root, laterClock))
	if contents, _, found := next.entryAt(next.caching.Store, key); !found ||
		string(contents) != "the record written in the same tick" {
		test.Errorf("want the record the store holds now, got %q (found %v)", contents, found)
	}
}

// runPackedSession is runWithSession with the session an extra build's, keeping its packs, over a cache in
// storeDirectory as cachingAt keys it, its store a new run's, with shard stamps of its own judged by laterClock.
func runPackedSession(test *testing.T, directory string, storeDirectory string, rules []rule.Rule) sessionRun {
	test.Helper()
	caching := cachingAt(storeDirectory, rules)
	caching.Store = runStore(storeDirectory, laterClock)
	session := NewCacheSession(caching, rules)
	session.KeepBuildPacks("project")
	loaded, loadError := load.PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session})
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld})
	if runError != nil {
		test.Fatalf("running %d rules: %v", len(rules), runError)
	}
	return sessionRun{loaded: loaded, result: result, session: session}
}

// A record the pack serves that falls short of what the run asks of it, as base stored without facts once user
// imports it, is dropped as a miss (dropShortHits), checked from source, and written again, and the pack written at
// the save holds the new record, the store's, with the run's findings those of a run with no cache (#b2t9xmf).
func TestAShortRecordThePackServedIsCheckedAndPackedAgain(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, unimportedFiles)
	rules := []rule.Rule{houseRule(markedAnalyzer)}
	storeDirectory := test.TempDir()
	store := cache.Open(storeDirectory, nil)
	// The first run packs what it wrote, the second what it read, by stamps taken before its reads.
	runPackedSession(test, root, storeDirectory, rules)
	settled := runPackedSession(test, root, storeDirectory, rules)
	if counts := settled.result.Cache.Packs; counts == nil || counts.Read != 1 {
		test.Fatalf("want the second run to read the first's pack, got %+v", counts)
	}
	base := func(session *CacheSession) (*packages.Package, cache.Key) {
		for keyed, key := range session.keys {
			if keyed.ID == "example.com/unimported/base" {
				return keyed, key
			}
		}
		test.Fatal("the load reached no plain base")
		return nil, cache.Key{}
	}
	_, baseKey := base(settled.session)
	place := settled.session.packs.byStore[0].place
	before, _ := store.ReadPack(place)
	packedBefore, held := before.Entries[baseKey]
	if !held {
		test.Fatal("the pack holds no record of base")
	}

	writeFiles(test, root, map[string]string{"user/user.go": userFile})
	imported := runPackedSession(test, root, storeDirectory, rules)
	if counts := imported.result.Cache.Packs; counts == nil || counts.RecordsServed == 0 || counts.Written != 1 {
		test.Fatalf("want the pack to serve records, base's among them, and be written again, got %+v", counts)
	}
	if baseImported, _ := base(imported.session); imported.session.restored(baseImported) {
		test.Error("want base, served without facts, a miss once user imports it, got a hit")
	}
	after, _ := store.ReadPack(place)
	stored, found := store.Get(baseKey)
	if !found || bytes.Equal(packedBefore.Entry, stored) || !bytes.Equal(after.Entries[baseKey].Entry, stored) {
		test.Errorf("want the pack written with base's new record, the store's (found %v)", found)
	}
	uncachedLoad, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading %s again: %v", root, loadError)
	}
	uncached, runError := Run(uncachedLoad.Packages, rules, Options{})
	if runError != nil {
		test.Fatalf("running without the cache: %v", runError)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(imported.result), withoutTimesOrCache(uncached)) {
		test.Fatalf("cached\n%+v\nwithout the cache\n%+v", withoutTimesOrCache(imported.result),
			withoutTimesOrCache(uncached))
	}
}

// keysApartFrom are count keys, each in a shard of its own and none in shard's, so a write to one moves no other's
// shard.
func keysApartFrom(shard byte, count int) []cache.Key {
	keys := make([]cache.Key, 0, count)
	for first := byte(0x21); len(keys) < count; first++ {
		if first != shard {
			keys = append(keys, cache.Key{first})
		}
	}
	return keys
}

// aliasedStore is a store at root holding an entry under target and, in the alias table of packedSession's context,
// alias standing for it, with second a key of a shard of its own the store holds nothing under yet. Each key is in a
// shard of its own and none in the table's, so a write to one moves no other's, and the alias's shard holds a file too,
// so each of the three shards an entry read through the alias is read from has a stamp a pack can hold.
func aliasedStore(test *testing.T, root string) (store *cache.Store, alias cache.Key, target cache.Key,
	second cache.Key) {
	test.Helper()
	store = cache.Open(root, nil)
	// The session's alias table is the one of its context, which packedSession leaves empty.
	keys := keysApartFrom(cache.ShardOf(newAliasTable(store, cache.Context{}).key), 3)
	alias, target, second = keys[0], keys[1], keys[2]
	besideTheAlias := alias
	besideTheAlias[1] = 0x01
	for key, contents := range map[cache.Key]string{
		target:         "the entry the alias stands for",
		besideTheAlias: "another entry of the alias's shard",
	} {
		if putError := store.Put(key, []byte(contents)); putError != nil {
			test.Fatal(putError)
		}
	}
	if addError := newAliasTable(store, cache.Context{}).add(map[cache.Key]cache.Key{alias: target},
		aliasTableFloor); addError != nil {
		test.Fatal(addError)
	}
	return store, alias, target, second
}

// An entry read through an alias was read from the alias table's shard too, so a write to that shard alone, the table
// rewritten with the alias standing for another key, moves what the pack was made of: the next run reads the alias
// from the store, as a run with no pack does, and finds the entry it stands for now (#b2t9xmf).
func TestAnEntryReadThroughAnAliasIsReadAgainOnceItsTableIsRewritten(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store, alias, _, second := aliasedStore(test, root)
	if putError := store.Put(second, []byte("the entry it stands for now")); putError != nil {
		test.Fatal(putError)
	}
	packedOnce(test, runStore(root, laterClock), alias, "the entry the alias stands for")
	// Only the table's shard is written.
	if addError := newAliasTable(store, cache.Context{}).add(map[cache.Key]cache.Key{alias: second},
		aliasTableFloor); addError != nil {
		test.Fatal(addError)
	}
	session := packedSession(runStore(root, laterClock))
	contents, storedKey, found := session.entryAt(session.caching.Store, alias)
	if !found || string(contents) != "the entry it stands for now" || storedKey != second {
		test.Errorf("want the entry the alias stands for now, got %q under %x (found %v)", contents, storedKey[:1],
			found)
	}
	if served := session.packs.served.Load(); served != 0 {
		test.Errorf("want nothing served from the pack, got %d", served)
	}
}

// An entry the pack serves through an alias is served under the key it is stored under, its target's, as the store
// reads it: a hit stored under its own key is trusted with no look at its reads (holdAliasHits), and its dependence
// record is beside the key it is stored under (recordKeyOf), so the pack's hit is judged as the store's would be.
func TestAnEntryThePackServesThroughAnAliasIsStoredUnderItsTarget(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	_, alias, target, _ := aliasedStore(test, root)
	packedOnce(test, runStore(root, laterClock), alias, "the entry the alias stands for")
	session := packedSession(runStore(root, laterClock))
	contents, storedKey, found := session.entryAt(session.caching.Store, alias)
	if !found || string(contents) != "the entry the alias stands for" || storedKey != target {
		test.Errorf("want the entry stored under the alias's target, got %q under %x (found %v)", contents,
			storedKey[:1], found)
	}
	if served := session.packs.served.Load(); served != 1 {
		test.Errorf("want the entry served from the pack, got %d served", served)
	}
}

// An entry read through an alias was read from its target's shard too, so another run's write of the target alone,
// other bytes under the key the alias stands for, moves what the pack was made of: the next run reads the alias from
// the store, as a run with no pack does, and finds the target as the store holds it now (#b2t9xmf).
func TestAnEntryReadThroughAnAliasIsReadAgainOnceItsTargetIsWrittenAgain(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store, alias, target, _ := aliasedStore(test, root)
	packedOnce(test, runStore(root, laterClock), alias, "the entry the alias stands for")
	// Only the target's shard is written.
	if putError := store.Put(target, []byte("the target another run wrote again")); putError != nil {
		test.Fatal(putError)
	}
	session := packedSession(runStore(root, laterClock))
	contents, storedKey, found := session.entryAt(session.caching.Store, alias)
	if !found || string(contents) != "the target another run wrote again" || storedKey != target {
		test.Errorf("want the target as the store holds it now, got %q under %x (found %v)", contents,
			storedKey[:1], found)
	}
	if served := session.packs.served.Load(); served != 0 {
		test.Errorf("want nothing served from the pack, got %d", served)
	}
}

// An entry read through an alias was read from the alias's own shard too, where the store looks first, so another
// run's record under the alias itself, which a run with no pack reads before it looks at the alias table, moves what
// the pack was made of: the next run reads the record under the alias, stored under the alias (#b2t9xmf).
func TestAnEntryReadThroughAnAliasIsReadAgainOnceARecordIsWrittenUnderTheAlias(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store, alias, _, _ := aliasedStore(test, root)
	packedOnce(test, runStore(root, laterClock), alias, "the entry the alias stands for")
	// Only the alias's shard is written.
	if putError := store.Put(alias, []byte("a record another run wrote under the alias")); putError != nil {
		test.Fatal(putError)
	}
	session := packedSession(runStore(root, laterClock))
	contents, storedKey, found := session.entryAt(session.caching.Store, alias)
	if !found || string(contents) != "a record another run wrote under the alias" || storedKey != alias {
		test.Errorf("want the record under the alias, stored under it, got %q under %x (found %v)", contents,
			storedKey[:1], found)
	}
	if served := session.packs.served.Load(); served != 0 {
		test.Errorf("want nothing served from the pack, got %d", served)
	}
}

// A shard is stamped before the session reads a key of it from the store, so a write there between the read and the
// save, another run's, which no store of this run marks moved, is after the stamp the pack records, and the next run
// reads the key from the store rather than serve what this one read (#b2t9xmf).
func TestAWriteBetweenAReadAndTheSaveIsReadFromTheStoreNextRun(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := cache.Open(root, nil)
	key := cache.Key{0x0D}
	if putError := store.Put(key, []byte("the record the run read")); putError != nil {
		test.Fatal(putError)
	}
	session := packedSession(runStore(root, laterClock))
	if contents, _, found := session.entryAt(session.caching.Store, key); !found ||
		string(contents) != "the record the run read" {
		test.Fatalf("the run read %q (found %v)", contents, found)
	}
	if putError := store.Put(key, []byte("the record another run wrote meanwhile")); putError != nil {
		test.Fatal(putError)
	}
	session.writePacks()
	next := packedSession(runStore(root, laterClock))
	if contents, _, found := next.entryAt(next.caching.Store, key); !found ||
		string(contents) != "the record another run wrote meanwhile" {
		test.Errorf("want the record the store holds now, got %q (found %v)", contents, found)
	}
}

// An alias's target's shard is stamped before the session reads the target through it, so another run's write of the
// target between the read and the save is after the stamp the pack records, and the next run reads the alias from the
// store rather than serve the target as this one read it (#b2t9xmf).
func TestATargetWrittenBetweenAReadThroughItsAliasAndTheSaveIsReadFromTheStoreNextRun(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store, alias, target, _ := aliasedStore(test, root)
	session := packedSession(runStore(root, laterClock))
	if contents, _, found := session.entryAt(session.caching.Store, alias); !found ||
		string(contents) != "the entry the alias stands for" {
		test.Fatalf("the run read %q (found %v)", contents, found)
	}
	if putError := store.Put(target, []byte("the target another run wrote meanwhile")); putError != nil {
		test.Fatal(putError)
	}
	session.writePacks()
	next := packedSession(runStore(root, laterClock))
	if contents, _, found := next.entryAt(next.caching.Store, alias); !found ||
		string(contents) != "the target another run wrote meanwhile" {
		test.Errorf("want the target as the store holds it now, got %q (found %v)", contents, found)
	}
}

// The alias table's shard is stamped before the session resolves an alias by it, so another run's rewrite of the
// table between the resolution and the save, the alias standing for another key, is after the stamp the pack records,
// and the next run resolves the alias again and finds the entry it stands for now (#b2t9xmf).
func TestATableRewrittenBetweenAResolutionAndTheSaveIsReadFromTheStoreNextRun(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store, alias, _, second := aliasedStore(test, root)
	if putError := store.Put(second, []byte("the entry it stands for now")); putError != nil {
		test.Fatal(putError)
	}
	session := packedSession(runStore(root, laterClock))
	if contents, _, found := session.entryAt(session.caching.Store, alias); !found ||
		string(contents) != "the entry the alias stands for" {
		test.Fatalf("the run read %q (found %v)", contents, found)
	}
	if addError := newAliasTable(store, cache.Context{}).add(map[cache.Key]cache.Key{alias: second},
		aliasTableFloor); addError != nil {
		test.Fatal(addError)
	}
	session.writePacks()
	next := packedSession(runStore(root, laterClock))
	if contents, storedKey, found := next.entryAt(next.caching.Store, alias); !found ||
		string(contents) != "the entry it stands for now" || storedKey != second {
		test.Errorf("want the entry the alias stands for now, got %q under %x (found %v)", contents, storedKey[:1],
			found)
	}
}

// A key the run writes without asking it first has its shard stamped before the write (putRecord), so the write marks
// it moved and the pack records no stamp of it: a stamp taken after the write would hold whatever landed there since,
// another run's write of the same key here, and the next run would serve the record this run wrote over the store's
// (#b2t9xmf).
func TestAKeyTheRunWritesIsNeverStampedAfterItsWrite(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := cache.Open(root, nil)
	key := cache.Key{0x0E}
	session := packedSession(runStore(root, laterClock))
	if putError := session.putRecord(session.caching.Store, key,
		encodedEntry{record: entryRecord{Version: "the record this run wrote"}}); putError != nil {
		test.Fatal(putError)
	}
	if putError := store.Put(key, []byte("the record another run wrote")); putError != nil {
		test.Fatal(putError)
	}
	session.writePacks()
	if read, _ := store.ReadPack(session.packs.byStore[0].place); len(read.Stamps) != 0 {
		test.Errorf("want no stamp of the shard the run wrote, got %v", read.Stamps)
	}
	next := packedSession(runStore(root, laterClock))
	if contents, _, found := next.entryAt(next.caching.Store, key); !found ||
		string(contents) != "the record another run wrote" {
		test.Errorf("want the record the store holds now, got %q (found %v)", contents, found)
	}
}

// A shard the run writes after reading its stamp, one the store already held, is recorded as none too: the write
// marked it moved, and the stamp read before it is of what the store held then, not of what the pack holds. Were it
// recorded, a shard's time that comes back to it, as a clock stepped back leaves one, would have the next run serve
// the record this run wrote over another run's write since (#b2t9xmf). The time is set back by the test.
func TestAShardTheRunWroteAfterReadingItsStampIsNeverRecorded(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := cache.Open(root, nil)
	key := cache.Key{0x11}
	if putError := store.Put(key, []byte("the record the run read")); putError != nil {
		test.Fatal(putError)
	}
	session := packedSession(runStore(root, laterClock))
	if contents, _, found := session.entryAt(session.caching.Store, key); !found ||
		string(contents) != "the record the run read" {
		test.Fatalf("the run read %q (found %v)", contents, found)
	}
	reading := session.packs.stamp(session.caching.Store, cache.ShardOf(key))
	if !reading.Trusted {
		test.Fatalf("want the shard's stamp trusted as the run read it, got %+v", reading)
	}
	if putError := session.putRecord(session.caching.Store, key,
		encodedEntry{record: entryRecord{Version: "the record this run wrote"}}); putError != nil {
		test.Fatal(putError)
	}
	if putError := store.Put(key, []byte("the record another run wrote")); putError != nil {
		test.Fatal(putError)
	}
	session.writePacks()
	if read, _ := store.ReadPack(session.packs.byStore[0].place); len(read.Stamps) != 0 {
		test.Errorf("want no stamp of the shard the run wrote, got %v", read.Stamps)
	}
	stamped := time.Unix(0, reading.Stamp)
	shardDirectory := filepath.Join(root, ".cache", "cohere", "go", key.String()[:2])
	if timesError := os.Chtimes(shardDirectory, stamped, stamped); timesError != nil {
		test.Fatal(timesError)
	}
	next := packedSession(runStore(root, laterClock))
	if contents, _, found := next.entryAt(next.caching.Store, key); !found ||
		string(contents) != "the record another run wrote" {
		test.Errorf("want the record the store holds now, got %q (found %v)", contents, found)
	}
}

// A record the pack served that the run then writes anew under the same key, as one dropShortHits drops, checked from
// source and saved again, is never served from the pack again in the run: the write marks its shard moved, so a
// reload's Restore asking the key reads the store's new record, here after a save whose pack write failed, which
// leaves the pack as the run read it (#b2t9xmf).
func TestARecordTheRunWroteAgainIsNeverServedOldOnAReload(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := cache.Open(root, nil)
	key := cache.Key{0x0F}
	if putError := store.Put(key, []byte("the record the pack was made of")); putError != nil {
		test.Fatal(putError)
	}
	// The first run packs the record, and the second reads its stamps' shards before they are written.
	packedOnce(test, runStore(root, laterClock), key, "the record the pack was made of")
	session := packedSession(runStore(root, laterClock))
	if contents, _, found := session.entryAt(session.caching.Store, key); !found ||
		string(contents) != "the record the pack was made of" || session.packs.served.Load() != 1 {
		test.Fatalf("want the record served from the pack, got %q (found %v, %d served)", contents, found,
			session.packs.served.Load())
	}
	if putError := session.putRecord(session.caching.Store, key,
		encodedEntry{record: entryRecord{Version: "the record checked from source"}}); putError != nil {
		test.Fatal(putError)
	}
	// A directory where the pack goes fails its rename, so the save writes no pack.
	place := session.packs.byStore[0].place
	packPath := filepath.Join(root, ".cache", "cohere", "go", "packs", place.Identity.String(), place.Name.String())
	if removeError := os.Remove(packPath); removeError != nil {
		test.Fatal(removeError)
	}
	if makeError := os.MkdirAll(filepath.Join(packPath, "held"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	session.writePacks()
	if counts := session.counts.Packs; counts == nil || counts.Written != 0 {
		test.Fatalf("want the save's pack write to fail, got %+v", counts)
	}
	stored, _ := store.Get(key)
	if contents, _, found := session.entryAt(session.caching.Store, key); !found || !bytes.Equal(contents, stored) {
		test.Errorf("want the reload to read the record the run wrote, got %q (found %v)", contents, found)
	}
}

// A save that writes the pack has a reload's Restore serve what it wrote, the entries with their stamps together: a
// key the pack held stale when the run opened it, written again by another run since, which the run read from the
// store, is served on a reload as the store holds it, never as the pack held it when opened, whose stamp the save
// replaced with the one the run read (#b2t9xmf).
func TestAKeyThePackHeldStaleIsServedOnAReloadAsTheRunReadIt(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := cache.Open(root, nil)
	key := cache.Key{0x12}
	if putError := store.Put(key, []byte("the record the pack was made of")); putError != nil {
		test.Fatal(putError)
	}
	packedOnce(test, runStore(root, laterClock), key, "the record the pack was made of")
	if putError := store.Put(key, []byte("the record another run wrote")); putError != nil {
		test.Fatal(putError)
	}
	session := packedSession(runStore(root, laterClock))
	if contents, _, found := session.entryAt(session.caching.Store, key); !found ||
		string(contents) != "the record another run wrote" || session.packs.stale.Load() != 1 {
		test.Fatalf("want the record read from the store as stale, got %q (found %v, %d stale)", contents, found,
			session.packs.stale.Load())
	}
	session.writePacks()
	if counts := session.counts.Packs; counts == nil || counts.Written != 1 {
		test.Fatalf("want the save's pack written, got %+v", counts)
	}
	if contents, _, found := session.entryAt(session.caching.Store, key); !found ||
		string(contents) != "the record another run wrote" {
		test.Errorf("want the reload to read the record the run read, got %q (found %v)", contents, found)
	}
	if served := session.packs.served.Load(); served != 1 {
		test.Errorf("want the reload served from the pack the save wrote, got %d served", served)
	}
}

// The project's first pack write of a run removes every pack of another executable identity, file by file and then its
// directory, a pack's temporary file left behind among them, and keeps every pack of its own executable, another
// build's included: each new executable would otherwise leave every pack of the one before where no run reads it
// (#b2t9xmf).
func TestAPackOfAnotherIdentityIsRemovedAtTheSave(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := cache.Open(root, nil)
	key := cache.Key{0x10}
	if putError := store.Put(key, []byte("a record")); putError != nil {
		test.Fatal(putError)
	}
	// The session's executable identity is its context's, as a session opening its pack finds it.
	identity := packedSession(runStore(root, laterClock)).packs.byStore[0].place.Identity
	other := cache.PackPlace{
		Identity: cache.PackIdentity("another executable"),
		Name:     cache.PackName([]string{"other"}),
	}
	ours := cache.PackPlace{Identity: identity, Name: cache.PackName([]string{"another build"})}
	for _, place := range []cache.PackPlace{other, ours} {
		if writeError := store.WritePack(place, nil, nil); writeError != nil {
			test.Fatal(writeError)
		}
	}
	packs := filepath.Join(root, ".cache", "cohere", "go", "packs")
	otherDirectory := filepath.Join(packs, other.Identity.String())
	leftBehind := filepath.Join(otherDirectory, other.Name.String()+".123.temporary")
	if writeError := os.WriteFile(leftBehind, []byte("half a pack"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	written := packedOnce(test, runStore(root, laterClock), key, "a record")
	if _, statError := os.Stat(otherDirectory); !os.IsNotExist(statError) {
		test.Errorf("want another executable's packs and their directory removed, got %v", statError)
	}
	for _, place := range []cache.PackPlace{ours, written} {
		if _, refused := store.ReadPack(place); refused != "" {
			test.Errorf("want cohere-go's pack %x kept, got it %s", place.Name[:2], refused)
		}
	}
}

// The system cache's pack is named by every field of the standard library's context, so a build under another context
// never reads a pack of entries keyed under this one: each field changed names another pack, and so does another
// project. A new field without a case here fails, since the fields are found by reflection.
func TestEveryFieldOfTheSystemContextNamesItsPackApart(test *testing.T) {
	test.Parallel()

	context := cache.StandardLibraryContext{
		FormatVersion: "1", ExecutableIdentity: "executable", Build: []string{"GOOS=linux"}, Facts: []string{"nilness"},
		Loader: load.LoaderSource, CgoToolchain: []string{"CC=clang"}, Goroot: "/go",
	}
	place := systemPackPlace(context, "project")
	changes := map[string]func(*cache.StandardLibraryContext){
		"FormatVersion":      func(changed *cache.StandardLibraryContext) { changed.FormatVersion += "+" },
		"ExecutableIdentity": func(changed *cache.StandardLibraryContext) { changed.ExecutableIdentity += "+" },
		"Build":              func(changed *cache.StandardLibraryContext) { changed.Build = []string{"GOOS=darwin"} },
		"Facts":              func(changed *cache.StandardLibraryContext) { changed.Facts = []string{"printf"} },
		"Loader":             func(changed *cache.StandardLibraryContext) { changed.Loader = load.LoaderCgoTranslated },
		"CgoToolchain":       func(changed *cache.StandardLibraryContext) { changed.CgoToolchain = []string{"CC=gcc"} },
		"Goroot":             func(changed *cache.StandardLibraryContext) { changed.Goroot = "/other/go" },
	}
	fields := reflect.TypeFor[cache.StandardLibraryContext]()
	for index := range fields.NumField() {
		name := fields.Field(index).Name
		change, known := changes[name]
		if !known {
			test.Errorf("StandardLibraryContext.%s has no case here: say whether it names the system pack", name)
			continue
		}
		changed := context
		change(&changed)
		if systemPackPlace(changed, "project").Name == place.Name {
			test.Errorf("another %s names the same system pack", name)
		}
	}
	if systemPackPlace(context, "another project").Name == place.Name {
		test.Error("another project names the same system pack")
	}
}
