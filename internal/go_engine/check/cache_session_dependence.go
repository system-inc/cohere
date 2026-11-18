package check

import (
	"crypto/sha256"
	"fmt"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// Block 1's dependence records in the store (#6a3kjvz). A host's run that judges each build's dependence keeps each
// package's record (build_dependence's packageRecord, encoded) beside the package's entry, under a key derived from
// the entry's first key (DependenceRecordKey), never in the entry's body, so a plain run neither writes, reads nor
// knows of them. The first key holds every input a record reads: the package's own files, its import map, every
// dependency's key, the build, the engine and the format, so a record found under it is the package's as the run
// finds it, whether or not its entry hit, and a change anywhere below leaves a miss, never a stale record.
//
// A run with them on looks up the record of every package its host's load keyed, hit or miss, beside the entries in
// one fan-out, and hands each one found to the host's graph in place of the package's syntax (DependenceRecord). A hit
// by its first key with no record is made a miss once, so its record is written from its syntax (backfill); its
// importers' hits stand, since their keys did not move. A hit by an alias, a second key's, has no record under the
// first key that ran: it reads the record beside the entry the alias leads to, its target's (engine's ruling 1 on
// #6a3kjvz, piece 4). That record was made against other dependencies, and it is the package's as this run would
// build it for two reasons, and no broader one. A record names each object of another package it reads, so this run
// judges the object fresh, by the unit that declares it now, whatever the dependency changed. What a record keeps of
// another package's types is the bits a read reached walking through one of its unnamed types, a struct with no name
// of its own, and the second key holds the output of everything below the package, whose export rendering writes
// each such type whole (writtenType, through types.TypeString, unexported fields and their types included), so a
// change there moves the second key and no alias leads to the old record. The alias-records fixture pins that line
// (TestAnUnexportedFieldsTypeIsNeverReadStaleThroughAnAlias): a rendering that leaves out an unexported field of an
// unnamed struct reads a stale record, and its graph differs from a cold run's. A hit by an alias with no record
// beside its target stays without one, counted. Only the host writes a record, after its entry's write held, with no
// slot of the run's pool held, and a failed write is counted with the entries' failed writes.

// DependenceRecordVersion is the format of a dependence record as the store keeps it, hashed into every record's key
// (DependenceRecordKey), so a record of another format is never read: it changes whenever the record's encoding does,
// or what the collector records in it. At 2, an array's length in a spelled type reads what it uses in full, so a
// record of 1 holds a looser bit.
const DependenceRecordVersion = "dependence-record-2"

// DependenceRecordKey is where the store keeps the dependence record of the package whose first key is key: apart from
// every entry, export data and alias, and derived from the entry's key alone, so a record is read only by a run that
// keyed its package exactly as the one that wrote it.
func DependenceRecordKey(key cache.Key) cache.Key {
	return sha256.Sum256(append([]byte("dependence record "+DependenceRecordVersion+" "), key[:]...))
}

// DependenceRecordCounts are what the session did with dependence records over its last load: the records read, each
// found under its package's key; written, after their entries; backfilled, the first-key hits with no record made a
// miss so their records are written; alias read, the hits by an alias that hold, once holdAliasHits has dropped those
// that do not, which found the record beside their alias's target; alias skipped, those that found none there, which
// stay without one; and the records that did not decode, each a problem, whose packages are checked from source so
// their records are written again, a hit by its first key among them counted backfilled too, and one by an alias
// alias skipped, since it stays restored.
//
// DroppedAfterBackfill counts the hits dropShortHits made misses in a load that backfilled any, which asks the facts
// of everything below a backfilled package: a backfill that cascaded would show here.
//
// The standard library's counts are of the packages the host restored from the system cache's store, whose records the
// project's store keeps apart (cache_session_standard_library_records.go): read and unreadable over the last load, and
// written once the side load has ended, whose failed writes the run counts with the entries'
// (WriteStandardLibraryRecords).
type DependenceRecordCounts struct {
	Read                 int
	Written              int
	Backfilled           int
	AliasSkipped         int
	Unreadable           int
	DroppedAfterBackfill int
	AliasRead            int

	StandardLibraryRead       int
	StandardLibraryWritten    int
	StandardLibraryUnreadable int
}

// dependenceRecords is a session's dependence records over its last load: decode, the host graph's decoding of a
// record's bytes; found, each record the store holds of a package the load keyed that decodes, by package, decoded;
// throughAlias, the packages of found whose record is their alias target's; recordOf, the host graph's record of a
// package its syntax built, for the write, nil until the graph is built; and the counts.
type dependenceRecords struct {
	decode       func(encoded []byte) (*build_dependence.Record, error)
	found        map[*packages.Package]*build_dependence.Record
	throughAlias map[*packages.Package]bool
	recordOf     func(*packages.Package) ([]byte, bool)
	counts       DependenceRecordCounts

	// standardLibrary are the records the project's store holds of the standard-library packages the load restored from
	// the system cache's store, decoded (readStandardLibraryRecords).
	standardLibrary map[*packages.Package]*build_dependence.Record
}

// KeepDependenceRecords has the session read and write the dependence records of its host's load: under
// `--build-dependence` alone, on a session with a store. decode is the host graph's decoding of a record's bytes
// (build_dependence.DecodeRecord), which the session runs as it reads each record, so a record that does not decode is
// its own to say and to write again.
func (session *CacheSession) KeepDependenceRecords(decode func(encoded []byte) (*build_dependence.Record, error)) {
	if session.caching.Store != nil {
		session.dependence = &dependenceRecords{decode: decode}
	}
}

// DependenceRecordsFrom is where the session finds the record of a package the host's graph built from its syntax,
// which it writes beside the package's entry when it writes the entry (saveMisses). recordOf answers false for a
// package the graph holds no record of.
func (session *CacheSession) DependenceRecordsFrom(recordOf func(*packages.Package) ([]byte, bool)) {
	if session.dependence != nil {
		session.dependence.recordOf = recordOf
	}
}

// DependenceRecord is the record the store holds of keyed, a package of the last load, decoded, and whether it holds
// one that decodes. A record beside an alias's target is keyed's only while keyed is still restored through that
// alias: a hit the run made a miss after (dropShortHits) is checked from source, and its graph built from its syntax.
func (session *CacheSession) DependenceRecord(keyed *packages.Package) (*build_dependence.Record, bool) {
	if session.dependence == nil || session.dependence.throughAlias[keyed] && !session.restored(keyed) {
		return nil, false
	}
	record, isFound := session.dependence.found[keyed]
	return record, isFound
}

// readDependenceRecords looks up the record of every package among reachable the session keyed, hit or miss, and
// decodes each, as many at once as readHits reads entries, then makes a miss of each hit by its first key that has
// none (backfill) and counts each hit by an alias that has none. A hit by an alias looks up its record beside the
// entry the alias leads to (recordKeyOf). A record that does not decode is a problem, said,
// and its package has none: a hit by its first key is backfilled, so its record is written again over the one that
// did not decode, and a miss's is written from its syntax. A hit from the system cache's store is never backfilled,
// so no project's census writes its entry again in the store every project shares: its record is the standard
// library's records' (readStandardLibraryRecords).
func (session *CacheSession) readDependenceRecords(reachable []*packages.Package) {
	records := session.dependence
	if records == nil {
		return
	}
	records.found = map[*packages.Package]*build_dependence.Record{}
	records.throughAlias = map[*packages.Package]bool{}
	records.counts = DependenceRecordCounts{}
	var keyed []*packages.Package
	for _, each := range reachable {
		if _, hasKey := session.keys[each]; hasKey {
			keyed = append(keyed, each)
		}
	}
	read := make([]*build_dependence.Record, len(keyed))
	unreadable := make([]error, len(keyed))
	session.fanOut(len(keyed), func(index int) {
		recordKey, _ := session.recordKeyOf(keyed[index])
		if record, isStored := session.caching.Store.Get(DependenceRecordKey(recordKey)); isStored {
			read[index], unreadable[index] = records.decode(record)
		}
	})
	for index, each := range keyed {
		_, isAliased := session.recordKeyOf(each)
		switch {
		case unreadable[index] != nil:
			records.counts.Unreadable++
			session.problem(fmt.Sprintf("reading the dependence record of %s: %v", each.ID, unreadable[index]))
		case read[index] != nil && isAliased:
			records.found[each] = read[index]
			records.throughAlias[each] = true
			records.counts.AliasRead++
		case read[index] != nil:
			records.found[each] = read[index]
			records.counts.Read++
		}
	}
	for _, each := range keyed {
		_, hit := session.hits[each]
		_, isFound := records.found[each]
		if !hit || isFound || session.restoredFromSystemCache(each) {
			continue
		}
		if session.stored[each] != session.keys[each] {
			records.counts.AliasSkipped++
			continue
		}
		delete(session.hits, each)
		delete(session.stored, each)
		records.counts.Backfilled++
	}
	session.readStandardLibraryRecords(reachable)
}

// recordKeyOf is the key keyed's record is kept beside, and whether it is an alias's target: a hit by an alias that
// holdAliasHits kept, whose entry is under the key the alias leads to (session.stored), finds its record beside that
// entry, and every other package beside its own first key. A hit from the system cache's store has no record in the
// project's store under either; its record is kept apart (StandardLibraryDependenceRecordKey).
func (session *CacheSession) recordKeyOf(keyed *packages.Package) (cache.Key, bool) {
	stored, isStored := session.stored[keyed]
	if session.restored(keyed) && isStored && !session.restoredFromSystemCache(keyed) && stored != session.keys[keyed] {
		return stored, true
	}
	return session.keys[keyed], false
}

// dependenceRecordToWrite is the record to write beside saved's entry, and whether there is one: the host graph's,
// when the session keeps records, the graph holds one of saved and the store does not hold it already.
func (session *CacheSession) dependenceRecordToWrite(saved *packages.Package) ([]byte, bool) {
	records := session.dependence
	if records == nil || records.recordOf == nil {
		return nil, false
	}
	// A package whose record was its alias target's is written its own once it is checked from source.
	if _, isFound := records.found[saved]; isFound && !records.throughAlias[saved] {
		return nil, false
	}
	return records.recordOf(saved)
}
