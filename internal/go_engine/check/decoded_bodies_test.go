package check

import (
	"bytes"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// warmBodies is the write-through fixture run twice over one store, the host and windows each time: the second host
// hit every package in its store, so the run's memo of decoded bodies holds each host body, and windows's records,
// written through by the first run, name those bodies.
type warmBodies struct {
	root    string
	store   string
	host    *HostRun
	hostRun *CacheSession
	cold    *Result
	warm    *Result
	windows *CacheSession
}

// warmBodiesOf runs the write-through fixture cold and then warm over one store.
func warmBodiesOf(test *testing.T) warmBodies {
	test.Helper()
	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	store := filepath.Join(test.TempDir(), "shared")
	cold, _ := throughBuild(test, root, store, throughHost(test, root, store))
	host, hostSession := throughHostSession(test, root, store)
	warm, windows := throughBuild(test, root, store, host)
	return warmBodies{root: root, store: store, host: host, hostRun: hostSession, cold: cold, warm: warm,
		windows: windows}
}

// readUser is user's hit as a fresh session of windows, restoring from fixture's host, reads it from the store under
// windows's key of user, with the run's memo of decoded bodies as the session would read it.
func (fixture warmBodies) readUser(test *testing.T, known func(*cacheAnalyzers) *cacheAnalyzers) storeHit {
	test.Helper()
	session := NewCacheSession(windowsCachingAt(fixture.store, throughRules()), throughRules())
	session.RestoreFromHost(fixture.host)
	if known != nil {
		session.known = known(session.known)
	}
	user := throughPackage(test, fixture.windows, throughUserPath)
	return session.readHit(user, fixture.windows.keys[user], session.sharedBodies())
}

// windowsRecordOfUser is the record windows's store keeps of user, under windows's key of it, with that key.
func (fixture warmBodies) windowsRecordOfUser(test *testing.T) (entryRecord, cache.Key) {
	test.Helper()
	key := fixture.windows.keys[throughPackage(test, fixture.windows, throughUserPath)]
	return storedRecordAt(test, fixture.windows.caching.Store, key), key
}

// hostRecordOfUser is the record the host's store keeps of user, under the host's key of it.
func (fixture warmBodies) hostRecordOfUser(test *testing.T) entryRecord {
	test.Helper()
	key := fixture.hostRun.keys[throughPackage(test, fixture.hostRun, throughUserPath)]
	return storedRecordAt(test, fixture.hostRun.caching.Store, key)
}

// A warm build whose records name the bodies the host's session read from its store takes each from the run's memo of
// decoded bodies (#qqyq61s), and finds what its cold run found. A cold run's build, whose host read nothing from a
// store, and a run under `--no-cache`, which reads no store at all, take none.
func TestAWarmBuildTakesTheBodiesItsRecordsNameFromTheRunsDecodes(test *testing.T) {
	test.Parallel()

	fixture := warmBodiesOf(test)
	if fixture.warm.Cache.SharedBodyHits == 0 {
		test.Fatalf("windows's warm run took no body from the run's memo: %+v", fixture.warm.Cache)
	}
	if fixture.cold.Cache.SharedBodyHits != 0 {
		test.Errorf("windows's cold run took %d bodies from the run's memo, want none: its host hit nothing",
			fixture.cold.Cache.SharedBodyHits)
	}
	if !reflect.DeepEqual(fixture.warm.Findings, fixture.cold.Findings) {
		test.Errorf("windows warm found\n%+v\ncold\n%+v", fixture.warm.Findings, fixture.cold.Findings)
	}
	// The host's session keeps the memo it handed the run, so a host analyzing again after a reload keeps what it
	// decoded, and the run's Release empties it for both.
	memo := fixture.hostRun.bodies
	if memo == nil || memo != fixture.host.bodies || len(memo.byDigest) == 0 {
		test.Fatalf("the host's session holds memo %p, the run %p, want one memo holding bodies", memo,
			fixture.host.bodies)
	}
	fixture.host.Release()
	if len(memo.byDigest) != 0 {
		test.Errorf("the run's Release left %d bodies in the memo the host's session holds", len(memo.byDigest))
	}

	uncached := func() *Caching {
		caching := windowsCachingAt(test.TempDir(), throughRules())
		caching.Store = nil
		return caching
	}
	for range 2 {
		hostCaching := cachingAt(test.TempDir(), throughRules())
		hostCaching.Store = nil
		host, _ := hostSessionWith(test, fixture.root, hostCaching, throughRules(), nil)
		result, _ := buildWith(test, fixture.root, uncached(), host.Host, throughRules())
		if result.Cache.SharedBodyHits != 0 {
			test.Errorf("windows under --no-cache took %d bodies from the run's memo, want none",
				result.Cache.SharedBodyHits)
		}
	}
}

// A hit taken from the run's memo reads what its own record says it read (#qqyq61s): windows's reads name each owner by
// windows's key, never the host's, whose record of the same body names them by the host's.
func TestASharedBodysHitReadsItsOwnRecordsReads(test *testing.T) {
	test.Parallel()

	fixture := warmBodiesOf(test)
	read := fixture.readUser(test, nil)
	if read.restored == nil || !read.shared {
		test.Fatalf("windows's user was not taken from the run's memo: hit %t, shared %t", read.restored != nil,
			read.shared)
	}
	own, _ := fixture.windowsRecordOfUser(test)
	host := fixture.hostRecordOfUser(test)
	if !bytes.Equal(own.Body, host.Body) || len(own.Reads) == 0 || reflect.DeepEqual(own.Reads, host.Reads) {
		test.Fatalf("want windows's record of user naming the host's body with reads of its own, got body %x for "+
			"the host's %x, reads %+v for the host's %+v", own.Body, host.Body, own.Reads, host.Reads)
	}
	if !reflect.DeepEqual(read.restored.reads, own.Reads) || read.restored.readsIncomplete != own.ReadsIncomplete {
		test.Errorf("user's hit reads\n%+v (incomplete %t)\nits record\n%+v (incomplete %t)", read.restored.reads,
			read.restored.readsIncomplete, own.Reads, own.ReadsIncomplete)
	}
}

// A hit taken from the run's memo reads the export data its own record names (#qqyq61s), the types of its own target,
// never those of the record that decoded the body: windows's record of user is rewritten to name export data the
// host's does not, as a build whose word size differs names its own, and its body stays the host's.
func TestASharedBodysHitReadsItsOwnRecordsExportData(test *testing.T) {
	test.Parallel()

	fixture := warmBodiesOf(test)
	record, key := fixture.windowsRecordOfUser(test)
	exportData := []byte("windows's own export data of user")
	digest := exportDataDigest(exportData)
	store := fixture.windows.caching.Store
	if putError := store.Put(exportDataKey(digest), exportData); putError != nil {
		test.Fatal(putError)
	}
	record.ExportData = digest
	encoded, encodeError := encodeRecord(record)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if putError := store.Put(key, encoded); putError != nil {
		test.Fatal(putError)
	}

	read := fixture.readUser(test, nil)
	if read.restored == nil || !read.shared {
		test.Fatalf("windows's user was not taken from the run's memo: hit %t, shared %t", read.restored != nil,
			read.shared)
	}
	if !bytes.Equal(read.restored.exportDigest, digest) {
		test.Errorf("user's hit names export data %x, its record %x, the host's %x", read.restored.exportDigest,
			digest, fixture.hostRecordOfUser(test).ExportData)
	}
	got, readError := read.restored.types.ReadExportData()
	if readError != nil || !bytes.Equal(got, exportData) {
		test.Errorf("user's hit read export data %q (%v), want its record's %q", got, readError, exportData)
	}
}

// Two builds whose hits take one body from the run's memo each hold types of their own (#qqyq61s), since a build's
// restore writes their Holds, Declined and Untouched, and neither holds the memo's.
func TestTwoBuildsTakingOneSharedBodyHoldTypesOfTheirOwn(test *testing.T) {
	test.Parallel()

	fixture := warmBodiesOf(test)
	first := fixture.readUser(test, nil)
	second := fixture.readUser(test, nil)
	if first.restored == nil || second.restored == nil || !first.shared || !second.shared {
		test.Fatal("windows's user was not taken from the run's memo by both builds")
	}
	shared, held := fixture.host.bodies.of(first.restored.bodyDigest)
	if !held {
		test.Fatal("the run's memo holds no body of user")
	}
	if first.restored.types == second.restored.types || first.restored.types == shared.types ||
		second.restored.types == shared.types {
		test.Errorf("want three types of their own, got the first build's %p, the second's %p, the memo's %p",
			first.restored.types, second.restored.types, shared.types)
	}
}

// A build whose analyzers decode a body unlike the host's never takes a body from the run's memo (#qqyq61s): it reads
// and decodes each from disk itself, so a fact can never land under an analyzer another slot names.
func TestABuildWhoseAnalyzersDifferTakesNoSharedBody(test *testing.T) {
	test.Parallel()

	fixture := warmBodiesOf(test)
	// The one difference: a report analyzer the host's lack, which decodesAlike reads as unlike, and which decodes the
	// entries cleanly, since none holds its slot, so the build still hits.
	read := fixture.readUser(test, func(known *cacheAnalyzers) *cacheAnalyzers {
		unlike := *known
		unlike.reports = maps.Clone(known.reports)
		unlike.reports[&analysis.Analyzer{Name: "report_the_host_lacks"}] = true
		return &unlike
	})
	if read.restored == nil {
		test.Fatal("windows's user did not decode from disk")
	}
	if read.shared {
		test.Error("a build whose analyzers differ took user's body from the run's memo, want its own decode")
	}
}

// SharedBodyHits counts the hits a load kept whose body was the run's memo's, never one dropShortHits made a miss
// after readHit took its body (#qqyq61s).
func TestSharedBodyHitsCountsOnlyTheHitsKept(test *testing.T) {
	test.Parallel()

	session := NewCacheSession(&Caching{}, throughRules())
	kept, dropped := &packages.Package{ID: "kept"}, &packages.Package{ID: "dropped"}
	session.hits = map[*packages.Package]*restoredPackage{kept: {}}
	session.sharedBodyHits = map[*packages.Package]bool{kept: true, dropped: true}
	session.save(nil, nil, nil)
	if session.counts.SharedBodyHits != 1 {
		test.Errorf("counted %d hits from the memo, want the 1 kept", session.counts.SharedBodyHits)
	}
}

// A record whose body the run's memo holds hits from it though the host's record at its BodyAt has moved on to another
// body since (#qqyq61s), on purpose: the record's own digest names the bytes it judged, which is what the holder's
// compare proves on disk. Read from disk, with no memo, the same record is a miss, as it always was.
func TestASharedBodyHitsThoughItsHostRecordMovedOn(test *testing.T) {
	test.Parallel()

	fixture := warmBodiesOf(test)
	own, key := fixture.windowsRecordOfUser(test)
	hostKey := fixture.hostRun.keys[throughPackage(test, fixture.hostRun, throughUserPath)]
	if !bytes.Equal(own.BodyAt, hostKey[:]) {
		test.Fatalf("windows's record of user names %x, want the host's record at %x", own.BodyAt, hostKey[:])
	}
	rewriteStoredEntry(test, fixture.hostRun.caching.Store, hostKey, func(entry *cacheEntry) {
		entry.Files = append(entry.Files, storedFile{Name: "moved on"})
	})
	if moved := storedRecordAt(test, fixture.hostRun.caching.Store, hostKey); bytes.Equal(moved.Body, own.Body) {
		test.Fatal("the host's record of user still holds the body windows's names, so nothing moved")
	}

	read := fixture.readUser(test, nil)
	if read.restored == nil || !read.shared || !bytes.Equal(read.restored.bodyDigest, own.Body) {
		test.Errorf("windows's user from the memo: hit %t, shared %t, want a hit of the body its record names",
			read.restored != nil, read.shared)
	}
	session := NewCacheSession(windowsCachingAt(fixture.store, throughRules()), throughRules())
	user := throughPackage(test, fixture.windows, throughUserPath)
	if fromDisk := session.readHit(user, key, nil); fromDisk.restored != nil {
		test.Error("windows's user read from disk hit, though its host record holds another body")
	}
}

// A record of a shape no writer makes, naming its body by a key of the wrong length and holding none, is a miss from
// the run's memo as from disk (#qqyq61s), though the memo holds the body its digest names.
func TestAMalformedRecordNeverHitsFromTheMemo(test *testing.T) {
	test.Parallel()

	fixture := warmBodiesOf(test)
	record, key := fixture.windowsRecordOfUser(test)
	if _, held := fixture.host.bodies.of(record.Body); record.Inline != nil || !held {
		test.Fatalf("want windows's record of user naming a body the memo holds, got %d bytes inline, held %t",
			len(record.Inline), held)
	}
	record.BodyAt = record.BodyAt[:len(record.BodyAt)/2]
	encoded, encodeError := encodeRecord(record)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if putError := fixture.windows.caching.Store.Put(key, encoded); putError != nil {
		test.Fatal(putError)
	}
	if read := fixture.readUser(test, nil); read.restored != nil || read.shared {
		test.Errorf("a record naming its body by %d bytes hit, shared %t, want a miss", len(record.BodyAt),
			read.shared)
	}
}

// The host's own analysis over hits whose decodes the run's memo shares, and a build's after it, write nothing of them
// (#qqyq61s): restore hands their facts to the checker's actions, importers inherit and read them, and findings,
// reports and answers are read, but each decode the memo holds is, after both runs, what a fresh decode of the same
// body is. user's comment is edited after a cold run, so the host hits base, whose printf fact about Logf user's
// analysis reads to find its planted finding.
func TestTheHostsAnalysisWritesNothingOfASharedDecode(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	store := filepath.Join(test.TempDir(), "shared")
	throughBuild(test, root, store, throughHost(test, root, store))
	editFile(test, root, "user/user.go", replaced(test, "// Use hands Logf", "// Use hands base.Logf"))

	// Each body the load hit, decoded afresh from the store before anything is analyzed, as the memo holds it.
	fresh := map[cache.Key]*restoredPackage{}
	result, session := hostSessionWith(test, root, cachingAt(store, throughRules()), throughRules(),
		func(session *CacheSession) {
			for hit, restored := range session.hits {
				if session.fromBinary[hit] {
					continue
				}
				decoded, decodeError := decodedStoredEntry(test, session.caching.Store, session.known, hit,
					session.stored[hit])
				if decodeError != nil || !bytes.Equal(decoded.bodyDigest, restored.bodyDigest) {
					test.Fatalf("%s: decoded afresh to body %x (%v), the hit's %x", hit.ID, decoded.bodyDigest,
						decodeError, restored.bodyDigest)
				}
				expected := newDecodedBodies(session.known)
				expected.add(decoded)
				fresh[cache.Key(decoded.bodyDigest)] = expected.byDigest[cache.Key(decoded.bodyDigest)]
			}
		})
	base, user := throughPackage(test, session, throughBasePath), throughPackage(test, session, throughUserPath)
	if !session.restored(base) || session.restored(user) {
		test.Fatalf("want base hit and user analyzed, got base hit %t, user hit %t", session.restored(base),
			session.restored(user))
	}
	if !slices.ContainsFunc(result.Findings, func(finding Finding) bool {
		return strings.HasSuffix(finding.File, "user.go") && finding.Rule == throughRules()[0].Name
	}) {
		test.Fatalf("the host found no printf finding in user.go, which only base's restored fact gives: %+v",
			result.Findings)
	}
	throughBuild(test, root, store, result.Host)
	for digest, expected := range fresh {
		held, kept := result.Host.bodies.of(digest[:])
		if !kept {
			test.Errorf("the run's memo lost body %x", digest[:])
			continue
		}
		if !reflect.DeepEqual(held, expected) {
			test.Errorf("body %x: the memo's decode after the runs\n%+v\na fresh decode\n%+v", digest[:], held,
				expected)
		}
	}
	if len(fresh) == 0 {
		test.Fatal("the host hit nothing, so the memo shared nothing to write")
	}
}

// The run's memo holds the bodies the host's session read from its store, never a decode from the binary's table
// (#qqyq61s), whose Withheld is renamed to the run's `GOROOT` after it: warm, the module's one package hits the store
// and every standard-library package the table, and the memo holds the module's body alone.
func TestTheRunsMemoHoldsNoDecodeFromTheBinarysTable(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	rules := throughRules()
	collecting := testCaching(test, rules)
	collected := map[cache.Key]StandardLibraryRunEntry{}
	collecting.StandardLibrary = &StandardLibraryCaching{
		Context:   standardLibraryContext("check tests", goRoot),
		Collected: collected,
	}
	runWithSession(test, directory, collecting, rules)
	table, tableError := StandardLibraryTableOf(collected, goRoot, nil)
	if tableError != nil {
		test.Fatalf("writing the table: %v", tableError)
	}
	caching := testCaching(test, rules)
	caching.StandardLibrary = &StandardLibraryCaching{
		Table:   cache.NewStandardLibraryTable(table),
		Context: standardLibraryContext("check tests", goRoot),
	}
	hostSessionWith(test, directory, caching, rules, nil)
	warm, session := hostSessionWith(test, directory, caching, rules, nil)
	host := warm.Host
	if len(session.fromBinary) == 0 || len(session.hits) == len(session.fromBinary) {
		test.Fatalf("want hits from the table and the store both, got %d hits, %d from the table", len(session.hits),
			len(session.fromBinary))
	}
	fromStore := map[cache.Key]bool{}
	for hit, restored := range session.hits {
		if !session.fromBinary[hit] {
			fromStore[cache.Key(restored.bodyDigest)] = true
		}
	}
	held := map[cache.Key]bool{}
	for digest := range host.bodies.byDigest {
		held[digest] = true
	}
	if !maps.Equal(held, fromStore) {
		test.Errorf("the run's memo holds %d bodies, want the %d of the store's hits alone", len(held),
			len(fromStore))
	}
}

// A record whose body the run's memo lacks is read from disk at BodyAt, and is a miss when the host's record there
// holds another body (#qqyq61s, #fq7dxvx): windows's next run beside the host's first, whose memo holds nothing of
// base, restores base from the host again rather than hit the rewritten body.
func TestARecordWhoseBodyTheMemoLacksMissesWhenItsHostRecordMoved(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	store := filepath.Join(test.TempDir(), "shared")
	host, hostSession := throughHostSession(test, root, store)
	first, _ := throughBuild(test, root, store, host)
	hostBase := hostSession.keys[throughPackage(test, hostSession, throughBasePath)]
	rewriteStoredEntry(test, hostSession.caching.Store, hostBase, func(entry *cacheEntry) { entry.Answers = nil })

	again, againSession := throughBuild(test, root, store, host)
	base := throughPackage(test, againSession, throughBasePath)
	if _, hit := againSession.stored[base]; hit {
		test.Errorf("windows hit base in its own store at %s, whose host record holds another body",
			againSession.stored[base])
	}
	if !slices.Contains(again.FromHost.Restored, throughBasePath) &&
		!slices.Contains(again.FromHost.Untouched, throughBasePath) {
		test.Errorf("windows restored %q and left %q untouched, want base among them", again.FromHost.Restored,
			again.FromHost.Untouched)
	}
	if again.Cache.SharedBodyHits != 0 {
		test.Errorf("windows took %d bodies from a memo its host's cold run left empty", again.Cache.SharedBodyHits)
	}
	if !reflect.DeepEqual(again.Findings, first.Findings) {
		test.Errorf("the run after\n%+v\nthe first run\n%+v", again.Findings, first.Findings)
	}
}

// hostOnlyFiles are the write-through fixture with a file only the host compiles beside base, whose printf finding the
// host's body of base holds and windows's never does.
var hostOnlyFiles = func() map[string]string {
	files := maps.Clone(throughFiles)
	files["base/base_host.go"] = "//go:build !windows\n\npackage base\n\nimport \"fmt\"\n\n" +
		"// Host formats on the host alone.\nfunc Host() string {\n\treturn fmt.Sprintf(\"%d\", \"host\")\n}\n"
	return files
}()

// The run's memo is keyed by a body's digest, never by its package (#qqyq61s): base has a file only the host
// compiles, so the host's body of base and windows's differ. Once that file is edited, the host's body of base is new,
// and on the next warm run the memo holds it, but windows's record names its own body, so windows finds what a run
// that restores nothing from the host finds, never the host's finding in the host's file.
func TestTheRunsMemoNeverHandsABuildAnotherBodyOfItsPackage(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, hostOnlyFiles)
	store := filepath.Join(test.TempDir(), "shared")
	throughBuild(test, root, store, throughHost(test, root, store))
	editFile(test, root, "base/base_host.go", replaced(test, `"host"`, `"edited"`))
	edited, _ := throughBuild(test, root, store, throughHost(test, root, store))
	host, hostSession := throughHostSession(test, root, store)
	warm, _ := throughBuild(test, root, store, host)
	reference, _ := throughBuild(test, root, filepath.Join(test.TempDir(), "reference"), nil)

	for label, result := range map[string]*Result{"after the edit": edited, "warm": warm} {
		if !reflect.DeepEqual(result.Findings, reference.Findings) {
			test.Errorf("%s windows found\n%+v\nand a run restoring nothing from the host\n%+v", label,
				result.Findings, reference.Findings)
		}
	}
	// What makes the comparison tell: the host's warm run hit base, its finding in base_host.go in the body the memo
	// holds.
	hostBase := throughPackage(test, hostSession, throughBasePath)
	hostHit, hostHeld := hostSession.hits[hostBase]
	if !hostHeld || !slices.ContainsFunc(hostHit.analyses[throughRules()[0].Analyzer].findings,
		func(finding Finding) bool { return strings.HasSuffix(finding.File, "base_host.go") }) {
		test.Fatal("the host's warm run did not hit base with its finding in base_host.go, so the memo holds " +
			"nothing windows could be handed")
	}
	if _, held := host.bodies.of(hostHit.bodyDigest); !held {
		test.Error("the run's memo holds no body of the host's base")
	}
}
