package standing_facts_test

import (
	"crypto/sha256"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
	"github.com/system-inc/cohere/internal/go_engine/rules/unused_code"
)

// recordsHost is fakeHost with the host's U1000 records of its package beside it, answered as the engine's side
// answers them (unused_code.BodyRecordHost, unused_code.FileDigestHost): for identifier alone, and only where held. A
// test changes what it answers by withheld, the bodies it answers no record of, ownDigests, a body's own file answered
// with another digest, and fileDigests, a file answered with another digest wherever a record names it.
type recordsHost struct {
	*fakeHost
	identifier  string
	held        bool
	recorded    unused_code.BodyRecords
	withheld    map[fact_reach.Place]bool
	ownDigests  map[fact_reach.Place][sha256.Size]byte
	fileDigests map[string][sha256.Size]byte
}

// BodyRecord is the host's record keyed at filename and offset, and its own file's digest.
func (host *recordsHost) BodyRecord(
	packageId string,
	filename string,
	offset int,
) (unused_code.RecordedBody, [sha256.Size]byte, bool) {
	host.count("BodyRecord")
	place := fact_reach.Place{File: filename, Offset: offset}
	if packageId != host.identifier || host.withheld[place] {
		return unused_code.RecordedBody{}, [sha256.Size]byte{}, false
	}
	for _, body := range host.recorded.Bodies {
		if bodyFile, bodyOffset := body.Key(); bodyFile != filename || bodyOffset != offset {
			continue
		}
		digest, isDigested := host.recorded.Digests[body.ParsedFile]
		if own, isChanged := host.ownDigests[place]; isChanged {
			digest = own
		}
		return body, digest, isDigested
	}
	return unused_code.RecordedBody{}, [sha256.Size]byte{}, false
}

// RecordsHeld is whether packageId is the package whose records the side holds, and the test has it hold them.
func (host *recordsHost) RecordsHeld(packageId string) bool {
	host.count("RecordsHeld")
	return host.held && packageId == host.identifier
}

// FileDigest is the digest of the file parsed under filename, as the test changed it, or as the host's run read it.
func (host *recordsHost) FileDigest(packageId string, filename string) ([sha256.Size]byte, bool) {
	if packageId != host.identifier {
		return [sha256.Size]byte{}, false
	}
	if digest, isChanged := host.fileDigests[filename]; isChanged {
		return digest, true
	}
	digest, isDigested := host.recorded.Digests[filename]
	return digest, isDigested
}

// recordsOf is what the host's U1000 recorded of the records fixture, run on a load of its own as a host's run asked to
// record its bodies runs it.
func recordsOf(test *testing.T) unused_code.BodyRecords {
	test.Helper()
	found := loadVariant(test, "records", "records")
	options := check.DispatchOptions()
	options.RecordBodies = true
	action, _ := analyze(test, found, unused_code.Analyzer, options)
	recorded, isRecorded := unused_code.RecordedOf(action.Result)
	if !isRecorded {
		test.Fatal("the host's U1000 handed no records on")
	}
	return recorded
}

// recordsSettled is the records fixture settled with each of its bodies standing, and the host's side holding the
// records U1000 made of it where held, Missing's withheld, Stale's own file and Answer's types.go answered with
// another digest, and the fixture as loaded.
func recordsSettled(test *testing.T, held bool) (*standing_facts.Result, fixture) {
	test.Helper()
	recorded := recordsOf(test)
	found := loadVariant(test, "records", "records")
	standing := map[fact_reach.Place]bool{}
	var places []fact_reach.Place
	for _, name := range []string{"Address", "Answer", "Missing", "Stale", "Plain"} {
		standing[found.place(test, name)] = true
		places = append(places, found.place(test, name))
	}
	if !slices.ContainsFunc(recorded.Bodies, func(body unused_code.RecordedBody) bool {
		return !body.Summarizable()
	}) {
		test.Fatal("the host recorded no body flagged, so Address's unsafe.Pointer conversion keeps nothing present")
	}
	other := sha256.Sum256([]byte("package records\n"))
	typesFile := filepath.Join(filepath.Dir(found.file), "types.go")
	host := &recordsHost{
		fakeHost:    &fakeHost{standing: standing, places: places},
		identifier:  found.root.ID,
		held:        held,
		recorded:    recorded,
		withheld:    map[fact_reach.Place]bool{found.place(test, "Missing"): true},
		ownDigests:  map[fact_reach.Place][sha256.Size]byte{found.place(test, "Stale"): other},
		fileDigests: map[string][sha256.Size]byte{typesFile: other},
	}
	result, _ := settle(test, found, host)
	return result, found
}

// A standing body whose record U1000 could not take leaves the standing set before anything is settled (#xeafx79),
// with a reason of its own and counted by why: Address's record is flagged, Answer's names types.go, which the side
// digests otherwise, Missing has none, and Stale's own file is digested otherwise. Plain's record is usable, and it
// stands. Nothing of it is a presence row's or a fact's.
func TestAStandingBodyWithAnUnusableRecordLeaves(test *testing.T) {
	test.Parallel()
	result, found := recordsSettled(test, true)
	want := []standing_facts.RecordLeaving{
		{Place: found.place(test, "Address"), Use: unused_code.RecordFlagged,
			Reason: standing_facts.RecordReason(unused_code.RecordFlagged)},
		{Place: found.place(test, "Answer"), Use: unused_code.RecordFilesDiffer,
			Reason: standing_facts.RecordReason(unused_code.RecordFilesDiffer)},
		{Place: found.place(test, "Missing"), Use: unused_code.RecordUnrecorded,
			Reason: standing_facts.RecordReason(unused_code.RecordUnrecorded)},
		{Place: found.place(test, "Stale"), Use: unused_code.RecordOwnFileDiffers,
			Reason: standing_facts.RecordReason(unused_code.RecordOwnFileDiffers)},
	}
	if !slices.Equal(result.RecordLeft, want) {
		test.Errorf("left for their records %+v, want %+v", result.RecordLeft, want)
	}
	wantCounts := map[unused_code.RecordUse]int{unused_code.RecordFlagged: 1, unused_code.RecordFilesDiffer: 1,
		unused_code.RecordUnrecorded: 1, unused_code.RecordOwnFileDiffers: 1}
	if !maps.Equal(result.RecordRemoved, wantCounts) {
		test.Errorf("removed for records %v, want %v", result.RecordRemoved, wantCounts)
	}
	for _, left := range want {
		if result.Stands(left.Place.File, left.Place.Offset) {
			test.Errorf("the body at %v stands, though its record is %s", left.Place, left.Use)
		}
	}
	if plain := found.place(test, "Plain"); !result.Stands(plain.File, plain.Offset) {
		test.Error("Plain does not stand, though its record is usable")
	}
	requireRemoved(test, result)
	if len(result.PresenceLeft) != 0 || len(result.PresenceRemoved) != 0 {
		test.Errorf("presence took out %v, counted %v, want nothing", result.PresenceLeft, result.PresenceRemoved)
	}
}

// Where the host's side holds no records of the package, nothing leaves for a record (#xeafx79): records not asked,
// U1000 not run, or a test variant asking by its path, so the standing the confined rules read is untouched.
func TestNoBodyLeavesForARecordWhereTheSideHoldsNone(test *testing.T) {
	test.Parallel()
	result, found := recordsSettled(test, false)
	if len(result.RecordLeft) != 0 || len(result.RecordRemoved) != 0 {
		test.Errorf("left for records %+v, counted %v, with no records held", result.RecordLeft, result.RecordRemoved)
	}
	for _, name := range []string{"Address", "Answer", "Missing", "Stale", "Plain"} {
		if place := found.place(test, name); !result.Stands(place.File, place.Offset) {
			test.Errorf("%s does not stand, though the side holds no records", name)
		}
	}
}
