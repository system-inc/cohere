package check

import (
	"fmt"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// The standard library's dependence records in the project's own store (#6a3kjvz piece 3, engine's ruling on its key
// and writes). A standard-library package the host restores from the system cache's store has no entry in the
// project's store, and so no record beside one: the switch loads it
// from source beside the host's lint for its graph (the side load). When it does, the host's session writes each such
// package's record, built from the side load's live packages, into the store under a key derived from the package's
// own standard-library key (StandardLibraryDependenceRecordKey), and a later run with a store reads it back in place
// of the side load. Any doubt is a miss: a record of another format, release, build, file, engine or toolchain is under
// another key, and one that does not decode is a problem, said, and loaded again, so its record is written over. A
// record names its files as the side load did, by their paths below this `GOROOT`, which its key holds through the
// toolchain, so a record is never read under another installation.

// StandardLibraryDependenceRecordVersion is the format of a standard-library dependence record as the store keeps it,
// hashed into its key apart from the host's records' version, so either changes alone. It moves with
// DependenceRecordVersion when what the collector records changes, as it did at 2, at 3, at 4, where the name bit of a
// type defined from another type's name reads that type in full, and at 5, where each unit carries whether it hangs
// on the C side, one in a cgo tool's output or in any file of a package checked as written that has a cgo file.
const StandardLibraryDependenceRecordVersion = "standard-library-dependence-record-5"

// StandardLibraryDependenceRecordKey is where the store keeps the dependence record of the standard-library package
// whose standard-library key is standardLibraryKey (cache.StandardLibraryKeys), written by the engine whose identity is
// engineIdentity under toolchain, the Go version and `GOROOT` go env reports. The standard-library key holds the
// cache's format, the release stamp, the build and every file's bytes by its path below `GOROOT`; the engine and the
// toolchain are hashed in beside it, so a record another engine, another toolchain or another installation wrote is
// never read.
func StandardLibraryDependenceRecordKey(
	standardLibraryKey cache.Key, engineIdentity string, toolchain string,
) cache.Key {
	return cache.DigestOf([]byte("standard library dependence record "+StandardLibraryDependenceRecordVersion),
		[]byte(engineIdentity), []byte(toolchain), standardLibraryKey[:])
}

// standardLibraryRecordKey is keyed's record key, and whether it has one: a standard-library package the session
// restored from the system cache's store, keyed by its standard-library key, in a session with a store.
func (session *CacheSession) standardLibraryRecordKey(keyed *packages.Package) (cache.Key, bool) {
	standardLibrary := session.caching.StandardLibrary
	key, isKeyed := session.standardLibraryKeys[keyed]
	if !isKeyed || standardLibrary == nil || !session.restoredFromSystemCache(keyed) {
		return cache.Key{}, false
	}
	return StandardLibraryDependenceRecordKey(key, session.caching.Context.EngineIdentity, standardLibrary.Toolchain),
		true
}

// readStandardLibraryRecords looks up, beside readDependenceRecords, the record of every standard-library package the
// session restored from the system cache's store, and decodes each, as many at once as readHits reads entries. A record
// that does not decode is a problem, said, and its package has none, so the side load loads it and its record is
// written again.
func (session *CacheSession) readStandardLibraryRecords(reachable []*packages.Package) {
	records := session.dependence
	records.standardLibrary = map[*packages.Package]*build_dependence.Record{}
	// A run that keys no standard library apart restores no standard-library package from the system cache, and has
	// none of these records to read.
	if session.caching.StandardLibrary == nil {
		return
	}
	var restored []*packages.Package
	var keys []cache.Key
	for _, each := range reachable {
		if key, isKeyed := session.standardLibraryRecordKey(each); isKeyed {
			restored = append(restored, each)
			keys = append(keys, key)
		}
	}
	read := make([]*build_dependence.Record, len(restored))
	unreadable := make([]error, len(restored))
	session.fanOut(len(restored), func(index int) {
		if record, isStored := session.caching.Store.Get(keys[index]); isStored {
			read[index], unreadable[index] = build_dependence.DecodeRecord(record)
		}
	})
	for index, each := range restored {
		switch {
		case unreadable[index] != nil:
			records.counts.StandardLibraryUnreadable++
			session.problem(fmt.Sprintf("reading the dependence record of %s: %v", each.ID, unreadable[index]))
		case read[index] != nil:
			records.standardLibrary[each] = read[index]
			records.counts.StandardLibraryRead++
		}
	}
}

// StandardLibraryRecord is the record the store holds of restored, a standard-library package the last load restored
// from the system cache's store, decoded, and whether it holds one that decodes.
func (session *CacheSession) StandardLibraryRecord(restored *packages.Package) (*build_dependence.Record, bool) {
	if session.dependence == nil {
		return nil, false
	}
	record, isFound := session.dependence.standardLibrary[restored]
	return record, isFound
}

// WriteStandardLibraryRecords writes into the store the record of every standard-library package the last load
// restored from the system cache's store with none, as recordOf gives it by the package's path: the host's side load's
// graph, once it has ended, which gives only the records it built from live packages. Each record is encoded on the
// run's pool and written with no slot held (fanOutWriting), as saveMisses writes the host's entries, since about 260
// writes one after another took some 40 ms at the run's tail. It is called after the session's counts were handed on,
// so it returns how many writes failed and the first one's reason, for the run to count among the cache's failed
// writes and say.
func (session *CacheSession) WriteStandardLibraryRecords(
	recordOf func(path string) ([]byte, bool),
) (int, error) {
	records := session.dependence
	if records == nil {
		return 0, nil
	}
	var writing []*packages.Package
	var keys []cache.Key
	for each := range session.hits {
		key, isKeyed := session.standardLibraryRecordKey(each)
		if _, isFound := records.standardLibrary[each]; isKeyed && !isFound {
			writing = append(writing, each)
			keys = append(keys, key)
		}
	}
	failed := 0
	var firstFailure error
	var countsLock sync.Mutex
	session.fanOutWriting(len(writing), func(index int) func() {
		record, isHeld := recordOf(writing[index].PkgPath)
		if !isHeld {
			return nil
		}
		// The write and its counting run with no slot held: the counting is a counter's, under a lock.
		return func() {
			putError := session.caching.Store.Put(keys[index], record)
			countsLock.Lock()
			defer countsLock.Unlock()
			if putError != nil {
				if failed == 0 {
					firstFailure = fmt.Errorf("writing the dependence record of %s: %w", writing[index].ID, putError)
				}
				failed++
				return
			}
			records.counts.StandardLibraryWritten++
		}
	})
	return failed, firstFailure
}
