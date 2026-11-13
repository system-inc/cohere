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
// importers' hits stand, since their keys did not move. A hit by an alias, a second key's, finds no record under the
// first key that ran, and stays without one, counted, since a record under the alias's target was made against other
// dependencies (engine's ruling on #6a3kjvz, until piece 4 proves it). Only the host writes a record, after its
// entry's write held, with no slot of the run's pool held, and a failed write is counted with the entries' failed
// writes.

// DependenceRecordVersion is the format of a dependence record as the store keeps it, hashed into every record's key
// (DependenceRecordKey), so a record of another format is never read: it changes whenever the record's encoding does.
const DependenceRecordVersion = "dependence-record-1"

// DependenceRecordKey is where the store keeps the dependence record of the package whose first key is key: apart from
// every entry, export data and alias, and derived from the entry's key alone, so a record is read only by a run that
// keyed its package exactly as the one that wrote it.
func DependenceRecordKey(key cache.Key) cache.Key {
	return sha256.Sum256(append([]byte("dependence record "+DependenceRecordVersion+" "), key[:]...))
}

// DependenceRecordCounts are what the session did with dependence records over its last load: the records read, each
// found under its package's key; written, after their entries; backfilled, the first-key hits with no record made a
// miss so their records are written; alias skipped, the hits by an alias that hold, once holdAliasHits has dropped
// those that do not, which stay without one; and the records that did not decode, each a problem, whose packages are
// checked from source so their records are written again, a hit among them counted backfilled too.
//
// DroppedAfterBackfill counts the hits dropShortHits made misses in a load that backfilled any, which asks the facts
// of everything below a backfilled package: a backfill that cascaded would show here.
type DependenceRecordCounts struct {
	Read                 int
	Written              int
	Backfilled           int
	AliasSkipped         int
	Unreadable           int
	DroppedAfterBackfill int
}

// dependenceRecords is a session's dependence records over its last load: decode, the host graph's decoding of a
// record's bytes; found, each record the store holds of a package the load keyed that decodes, by package, decoded;
// recordOf, the host graph's record of a package its syntax built, for the write, nil until the graph is built; and
// the counts.
type dependenceRecords struct {
	decode   func(encoded []byte) (*build_dependence.Record, error)
	found    map[*packages.Package]*build_dependence.Record
	recordOf func(*packages.Package) ([]byte, bool)
	counts   DependenceRecordCounts
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
// one that decodes.
func (session *CacheSession) DependenceRecord(keyed *packages.Package) (*build_dependence.Record, bool) {
	if session.dependence == nil {
		return nil, false
	}
	record, isFound := session.dependence.found[keyed]
	return record, isFound
}

// readDependenceRecords looks up the record of every package among reachable the session keyed, hit or miss, and
// decodes each, as many at once as readHits reads entries, then makes a miss of each hit by its first key that has
// none (backfill) and counts each hit by an alias that has none. A record that does not decode is a problem, said,
// and its package has none: a hit by its first key is backfilled, so its record is written again over the one that
// did not decode, and a miss's is written from its syntax. A hit from the binary's table is never the store's to
// backfill.
func (session *CacheSession) readDependenceRecords(reachable []*packages.Package) {
	records := session.dependence
	if records == nil {
		return
	}
	records.found = map[*packages.Package]*build_dependence.Record{}
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
		if record, isStored := session.caching.Store.Get(DependenceRecordKey(session.keys[keyed[index]])); isStored {
			read[index], unreadable[index] = records.decode(record)
		}
	})
	for index, each := range keyed {
		switch {
		case unreadable[index] != nil:
			records.counts.Unreadable++
			session.problem(fmt.Sprintf("reading the dependence record of %s: %v", each.ID, unreadable[index]))
		case read[index] != nil:
			records.found[each] = read[index]
			records.counts.Read++
		}
	}
	for _, each := range keyed {
		_, hit := session.hits[each]
		_, isFound := records.found[each]
		if !hit || isFound || session.fromBinary[each] {
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
}

// dependenceRecordToWrite is the record to write beside saved's entry, and whether there is one: the host graph's,
// when the session keeps records, the graph holds one of saved and the store does not hold it already.
func (session *CacheSession) dependenceRecordToWrite(saved *packages.Package) ([]byte, bool) {
	records := session.dependence
	if records == nil || records.recordOf == nil {
		return nil, false
	}
	if _, isFound := records.found[saved]; isFound {
		return nil, false
	}
	return records.recordOf(saved)
}
