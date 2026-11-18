package check

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// laterClock is a run's clock an hour ahead, so every shard stamp a test's store has is far enough behind its reading
// to be trusted (cache.ShardStamps), as a run's would be had the store been written a while before.
func laterClock() time.Time {
	return time.Now().Add(time.Hour)
}

// packedSession is an extra build's session over store with its packs opened under the source loader, as the build's
// first Restore opens them before it reads its hits, its shard stamps its own, as a new run's are, judged by clock.
func packedSession(store *cache.Store, clock func() time.Time) *CacheSession {
	session := NewCacheSession(&Caching{Store: store}, nil)
	session.KeepBuildPacks("project", cache.NewShardStamps(clock))
	session.openPacks("source")
	return session
}

// packedOnce is a run over store that reads key through its pack, finding want, its stamps judged by clock, and writes
// its pack at its save, returning the pack's name.
func packedOnce(test *testing.T, store *cache.Store, key cache.Key, want string, clock func() time.Time) cache.Key {
	test.Helper()
	session := packedSession(store, clock)
	if contents, _, found := session.entryAt(store, key); !found || string(contents) != want {
		test.Fatalf("the first run read %q (found %v), want %q", contents, found, want)
	}
	session.writePacks()
	if counts := session.counts.Packs; counts == nil || counts.Written != 1 {
		test.Fatalf("want the first run's pack written, got %+v", counts)
	}
	return session.packs.byStore[0].name
}

// A key the build's pack holds is served from it, and a key it does not hold is read from the store, never answered
// by another of the pack's entries: the store's entry under it, or a miss when the store holds none (#b2t9xmf). Only
// what was found is packed: a key missing from both adds nothing.
func TestAKeyThePackDoesNotHoldIsReadFromTheStore(test *testing.T) {
	test.Parallel()

	store := cache.Open(test.TempDir())
	// packed shares stored's shard, which the store holds, so the shard has a stamp the pack can hold.
	packed, stored, absent := cache.Key{0x02, 0x01}, cache.Key{0x02}, cache.Key{0x03}
	if putError := store.Put(stored, []byte("the store's record")); putError != nil {
		test.Fatal(putError)
	}
	// The pack's name is the session's own, as the first session opened it and found none, which is no refusal.
	opening := packedSession(store, laterClock)
	if opening.packs.counts.Missing != 1 || len(opening.packs.counts.RefusedBy) != 0 {
		test.Fatalf("want the store's pack missing and nothing refused before one is written, got %+v",
			opening.packs.counts)
	}
	name := opening.packs.byStore[0].name
	// The pack holds packed, which the store does not, so what is served of it can only be the pack's, with the stamp
	// of its shard as it is.
	packedReading, _ := cache.NewShardStamps(nil).Of(store, cache.ShardOf(packed))
	if !packedReading.Known {
		test.Fatal("the store has no shard of the packed key")
	}
	if writeError := store.WritePack(name, []cache.PackEntry{
		{Asked: packed, Stored: packed, Entry: []byte("the pack's record")},
	}, map[byte]int64{cache.ShardOf(packed): packedReading.Stamp}); writeError != nil {
		test.Fatal(writeError)
	}

	session := packedSession(store, laterClock)
	for _, testCase := range []struct {
		key   cache.Key
		want  string
		found bool
	}{
		{key: packed, want: "the pack's record", found: true},
		{key: stored, want: "the store's record", found: true},
		{key: absent},
	} {
		contents, storedKey, found := session.entryAt(store, testCase.key)
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
	read, refused := store.ReadPack(name)
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

	store := cache.Open(test.TempDir())
	key := cache.Key{0x07}
	if putError := store.Put(key, []byte("the record the pack was made of")); putError != nil {
		test.Fatal(putError)
	}
	packedOnce(test, store, key, "the record the pack was made of", laterClock)
	if putError := store.Put(key, []byte("the record another run wrote")); putError != nil {
		test.Fatal(putError)
	}
	session := packedSession(store, laterClock)
	if contents, _, found := session.entryAt(store, key); !found || string(contents) != "the record another run wrote" {
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
	store := cache.OpenAt(directory)
	key := cache.Key{0x09}
	if putError := store.Put(key, []byte("a record no run read for days")); putError != nil {
		test.Fatal(putError)
	}
	packedOnce(test, store, key, "a record no run read for days", laterClock)
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
	session := packedSession(store, laterClock)
	if contents, _, found := session.entryAt(store, key); found {
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
	store := cache.Open(root)
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
	session := packedSession(store, clock)
	if contents, _, found := session.entryAt(store, key); !found ||
		string(contents) != "the record the pack was made of" {
		test.Fatalf("the first run read %q (found %v)", contents, found)
	}
	// Written again within the tick the stamp was read in, after the reading: the shard's time is where it was.
	if putError := store.Put(key, []byte("the record written in the same tick")); putError != nil {
		test.Fatal(putError)
	}
	holdTick()
	session.writePacks()
	name := session.packs.byStore[0].name
	if read, _ := store.ReadPack(name); len(read.Stamps) != 0 {
		test.Errorf("want the stamp read half a second into its tick recorded as none, got %v", read.Stamps)
	}
	next := packedSession(store, laterClock)
	if contents, _, found := next.entryAt(store, key); !found ||
		string(contents) != "the record written in the same tick" {
		test.Errorf("want the record the store holds now, got %q (found %v)", contents, found)
	}
}

// runPackedSession is runWithSession with the session an extra build's, keeping its packs by laterClock.
func runPackedSession(test *testing.T, directory string, caching *Caching, rules []rule.Rule) sessionRun {
	test.Helper()
	session := NewCacheSession(caching, rules)
	session.KeepBuildPacks("project", cache.NewShardStamps(laterClock))
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
	caching := testCaching(test, rules)
	// The first run packs what it wrote, the second what it read, by stamps taken before its reads.
	runPackedSession(test, root, caching, rules)
	settled := runPackedSession(test, root, caching, rules)
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
	name := settled.session.packs.byStore[0].name
	before, _ := caching.Store.ReadPack(name)
	packedBefore, held := before.Entries[baseKey]
	if !held {
		test.Fatal("the pack holds no record of base")
	}

	writeFiles(test, root, map[string]string{"user/user.go": userFile})
	imported := runPackedSession(test, root, caching, rules)
	if counts := imported.result.Cache.Packs; counts == nil || counts.RecordsServed == 0 || counts.Written != 1 {
		test.Fatalf("want the pack to serve records, base's among them, and be written again, got %+v", counts)
	}
	if baseImported, _ := base(imported.session); imported.session.restored(baseImported) {
		test.Error("want base, served without facts, a miss once user imports it, got a hit")
	}
	after, _ := caching.Store.ReadPack(name)
	stored, found := caching.Store.Get(baseKey)
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
