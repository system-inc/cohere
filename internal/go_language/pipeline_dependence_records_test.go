package go_language

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/rules"
)

// The dependence-records fixture's packages (#6a3kjvz): platform, whose Make returns a Plain alike in every build,
// and whose Locked holds a Lock windows declares its own way, and reader, which compares two values Make returns.
const (
	recordsPlatform = "example.com/records/platform"
	recordsReader   = "example.com/records/reader"
)

// recordsSwitches are a run judging every build's dependence, the findings cache on in the fixture's own store. Every
// run here is read raw (runOverRaw), since normalize zeroes the cache's counts.
var recordsSwitches = []string{"--build-dependence"}

// graphVerdicts are every unit of every build's judgment of the host's graph, by build, package, file and start,
// each file relative to root, and whether the build makes it dependent, so two runs over two copies of a fixture
// compare.
func graphVerdicts(test *testing.T, pipeline *pipeline, root string) map[string]bool {
	test.Helper()
	verdicts := map[string]bool{}
	for _, run := range pipeline.buildRuns {
		if run.judged == nil || run.judged.dependence == nil {
			continue
		}
		for _, unit := range run.judged.dependence.Units() {
			file := strings.TrimPrefix(unit.File, root)
			verdicts[run.build.String()+" "+unit.PackageId+" "+file+":"+strconv.Itoa(unit.Start)] = unit.Dependent
		}
	}
	if len(verdicts) == 0 {
		test.Fatal("no build was judged on the host's graph")
	}
	return verdicts
}

// expectSameVerdicts fails for every unit one of want and got judges that the other does not judge alike.
func expectSameVerdicts(test *testing.T, label string, want map[string]bool, got map[string]bool) {
	test.Helper()
	for unit, dependent := range want {
		if judged, isJudged := got[unit]; !isJudged || judged != dependent {
			test.Errorf("%s: %q is judged %t, want %t (judged: %t)", label, unit, judged, dependent, isJudged)
		}
	}
	for unit := range got {
		if _, isWanted := want[unit]; !isWanted {
			test.Errorf("%s: %q is judged, and the other run holds no such unit", label, unit)
		}
	}
}

// readerDependent is whether some build calls a unit of reader dependent.
func readerDependent(verdicts map[string]bool) bool {
	for unit, dependent := range verdicts {
		if dependent && strings.Contains(unit, " "+recordsReader+" ") {
			return true
		}
	}
	return false
}

// recordCounts are the stream's dependence record counts, failing when it has none.
func recordCounts(test *testing.T, stream string) contract.DependenceRecords {
	test.Helper()
	counted := lintRecordOf(test, stream).Cache.DependenceRecords
	if counted == nil {
		test.Fatal("the cache record counts no dependence records")
		return contract.DependenceRecords{}
	}
	return *counted
}

// storeDirectory is the fixture's findings store, as cache.Open places it.
func storeDirectory(root string) string {
	return filepath.Join(root, ".cache", "cohere", "go")
}

// recordFileOf is the file the store keeps the dependence record of identifier in, found by the record's opening:
// the store's sum, the record's format and its package's ID.
func recordFileOf(test *testing.T, root string, identifier string) string {
	test.Helper()
	opening := append([]byte("cohere dependence record 1"), 0)
	opening = binary.AppendUvarint(opening, uint64(len(identifier)))
	opening = append(opening, identifier...)
	found := ""
	walkError := filepath.WalkDir(storeDirectory(root), func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		if len(contents) > sha256.Size && bytes.HasPrefix(contents[sha256.Size:], opening) {
			found = path
		}
		return nil
	})
	if walkError != nil || found == "" {
		test.Fatalf("the store holds no dependence record of %s: %v", identifier, walkError)
	}
	return found
}

// makeLocked edits root's platform so Make returns a Locked, whose layout windows changes: reader's own files stay
// as they are, and its key moves with platform's.
func makeLocked(test *testing.T, root string) {
	test.Helper()
	file := filepath.Join(root, "platform", "platform.go")
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatal(readError)
	}
	edited := strings.Replace(string(contents), "func Make() Plain { return Plain{} }",
		"func Make() Locked { return Locked{} }", 1)
	if edited == string(contents) {
		test.Fatal("platform's Make is not the fixture's")
	}
	if writeError := os.WriteFile(file, []byte(edited), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}

// A warm run's graph is a cold run's (#6a3kjvz): every package the second run restores hands the graph its record
// from the store, and every build judges every unit as the first run did. The cold run writes a record beside each
// entry it writes and reads none; the warm run reads one of each package and writes, backfills and skips none.
func TestAWarmGraphIsTheColdGraph(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "dependence-records"))
	cold, coldStream := runOverRaw(test, root, exitClean, recordsSwitches...)
	warm, warmStream := runOverRaw(test, root, exitClean, recordsSwitches...)
	coldVerdicts := graphVerdicts(test, cold, root)
	expectSameVerdicts(test, "warm against cold", coldVerdicts, graphVerdicts(test, warm, root))
	if readerDependent(coldVerdicts) {
		test.Error("a build calls a unit of reader dependent, which compares two Plains alike in every build")
	}
	coldCounts, warmCounts := recordCounts(test, coldStream), recordCounts(test, warmStream)
	if coldCounts.Written == 0 || coldCounts.Read != 0 {
		test.Errorf("the cold run wrote %d records and read %d, want some written and none read", coldCounts.Written,
			coldCounts.Read)
	}
	if warmCounts != (contract.DependenceRecords{Read: coldCounts.Written}) {
		test.Errorf("the warm run counts %+v, want the %d records the cold run wrote read, and nothing else",
			warmCounts, coldCounts.Written)
	}
}

// A record is never read stale (#6a3kjvz): platform's Make, edited to return a Locked, moves platform's key and so
// reader's, though reader's own files did not change, so the warm run over the edit finds no record of reader under
// its key and builds it from reader's syntax: its graph is a cold run's of the edited tree, where reader, comparing two
// Lockeds, is dependent on windows. A record keyed by the package's own digest alone would hand the warm run the
// record of reader that compared two Plains.
func TestAStaleRecordIsNeverRead(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "dependence-records"))
	runOverRaw(test, root, exitClean, recordsSwitches...)
	makeLocked(test, root)
	warm, _ := runOverRaw(test, root, exitClean, recordsSwitches...)
	edited := copyFixture(test, filepath.Join("testdata", "dependence-records"))
	makeLocked(test, edited)
	cold, _ := runOverRaw(test, edited, exitClean, recordsSwitches...)
	coldVerdicts := graphVerdicts(test, cold, edited)
	if !readerDependent(coldVerdicts) {
		test.Fatal("no build calls reader dependent in the edited tree, so the case proves nothing")
	}
	expectSameVerdicts(test, "warm over the edit against cold", coldVerdicts, graphVerdicts(test, warm, root))
}

// A record that does not decode under a valid opening is the run's problem, said, and it heals (#6a3kjvz): reader's
// record cut short, or its first unit placed past 2 GiB, where no unit column reaches (#gf9qrzf), with the store's sum
// written again so the store reads it whole, leaves a warm run that counts it unreadable, names it among the cache's
// problems, and backfills reader, checking it from source and writing its record again, so its graph is the cold
// run's; the run after reads every record whole.
func TestAnUnreadableRecordIsAProblem(test *testing.T) {
	test.Parallel()

	for _, damaged := range []struct {
		name   string
		damage func(test *testing.T, body []byte) []byte
	}{
		{"cut short", func(test *testing.T, body []byte) []byte { return body[:len(body)-3] }},
		{"a unit past 2 GiB", placeFirstUnitPastTwoGibibytes},
	} {
		test.Run(damaged.name, func(test *testing.T) {
			test.Parallel()

			root := copyFixture(test, filepath.Join("testdata", "dependence-records"))
			cold, _ := runOverRaw(test, root, exitClean, recordsSwitches...)
			file := recordFileOf(test, root, recordsReader)
			contents, readError := os.ReadFile(file)
			if readError != nil {
				test.Fatal(readError)
			}
			body := damaged.damage(test, contents[sha256.Size:])
			sum := sha256.Sum256(body)
			if writeError := os.WriteFile(file, append(sum[:], body...), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			healed, healedStream, noted := runOverNoted(test, root, exitClean, nil, recordsSwitches...)
			if counted := recordCounts(test, healedStream); counted != (contract.DependenceRecords{Read: 2, Written: 1,
				Backfilled: 1, Unreadable: 1}) {
				test.Errorf("the run counts %+v, want base's and platform's records read, reader's unreadable, "+
					"backfilled and written", counted)
			}
			if !strings.Contains(noted, "reading the dependence record of "+recordsReader) {
				test.Errorf("the run's notes say nothing of reader's record:\n%s", noted)
			}
			expectSameVerdicts(test, "healed against cold", graphVerdicts(test, cold, root),
				graphVerdicts(test, healed, root))
			_, afterStream := runOverRaw(test, root, exitClean, recordsSwitches...)
			if after := recordCounts(test, afterStream); after != (contract.DependenceRecords{Read: 3}) {
				test.Errorf("the run after counts %+v, want every record read and nothing else", after)
			}
		})
	}
}

// placeFirstUnitPastTwoGibibytes is a record's bytes with its first unit's start moved 2 GiB later, past what a unit
// column holds, and nothing else changed: it reads the record as build_dependence's decodeRecord does, up to that
// number (record_writer.go names the format), failing when the record holds no unit.
func placeFirstUnitPastTwoGibibytes(test *testing.T, body []byte) []byte {
	test.Helper()
	at := len("cohere dependence record 1") + 1
	number := func() uint64 {
		value, length := binary.Uvarint(body[at:])
		if length <= 0 {
			test.Fatalf("the record ends early at byte %d", at)
		}
		at += length
		return value
	}
	skip := func(numbers uint64) {
		for range numbers {
			number()
		}
	}
	raw := func() {
		at += int(number())
	}
	raw()
	raw()
	skip(1)
	for range number() {
		raw()
	}
	skip(2 * number())
	skip(number())
	skip(2 * number())
	if units := number(); units == 0 {
		test.Fatal("reader's record holds no unit, so the case proves nothing")
	}
	skip(1)
	startAt := at
	start := number()
	placed := append([]byte{}, body[:startAt]...)
	placed = binary.AppendUvarint(placed, start+1<<31)
	return append(placed, body[at:]...)
}

// A hit by its first key with no record is made a miss once, and no package above or below it misses with it
// (#6a3kjvz): platform's record taken from the store, the warm run checks platform from source and writes its
// record, while reader, which imports it, and base, which it imports and whose facts its analysis asks, stay hits,
// none dropped, and the graph is the cold run's; the run after backfills nothing.
func TestABackfillMissesOnceAndNeverCascades(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "dependence-records"))
	cold, _ := runOverRaw(test, root, exitClean, recordsSwitches...)
	if removeError := os.Remove(recordFileOf(test, root, recordsPlatform)); removeError != nil {
		test.Fatal(removeError)
	}
	backfilled, backfilledStream := runOverRaw(test, root, exitClean, recordsSwitches...)
	counted := recordCounts(test, backfilledStream)
	if counted != (contract.DependenceRecords{Read: 2, Written: 1, Backfilled: 1}) {
		test.Errorf("the run counts %+v, want base's and reader's records read, platform backfilled and its record "+
			"written, and no hit dropped", counted)
	}
	if missed := lintRecordOf(test, backfilledStream).Cache.Missed; missed != 1 {
		test.Errorf("the run missed %d packages, want platform alone", missed)
	}
	expectSameVerdicts(test, "backfilled against cold", graphVerdicts(test, cold, root),
		graphVerdicts(test, backfilled, root))
	_, afterStream := runOverRaw(test, root, exitClean, recordsSwitches...)
	if after := recordCounts(test, afterStream); after.Backfilled != 0 || after.Written != 0 {
		test.Errorf("the run after counts %+v, want nothing backfilled or written", after)
	}
}

// storedRecordKeys are the keys of the records root's store holds, of each kind: the host's, each under the key
// check.DependenceRecordKey derives from a key the store holds, and the standard library's, every other file whose
// body opens as a record does (check.StandardLibraryDependenceRecordKey, which no test can derive, since its key holds
// cohere-go's identity).
func storedRecordKeys(test *testing.T, root string) (map[cache.Key]bool, map[cache.Key]bool) {
	test.Helper()
	stored := map[cache.Key][]byte{}
	walkError := filepath.WalkDir(storeDirectory(root), func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		decoded, decodeError := hex.DecodeString(entry.Name())
		if decodeError != nil || len(decoded) != len(cache.Key{}) {
			return nil
		}
		contents, readError := os.ReadFile(path)
		stored[cache.Key(decoded)] = contents
		return readError
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	host, standardLibrary := map[cache.Key]bool{}, map[cache.Key]bool{}
	for key := range stored {
		if _, isStored := stored[check.DependenceRecordKey(key)]; isStored {
			host[check.DependenceRecordKey(key)] = true
		}
	}
	opening := append([]byte("cohere dependence record 1"), 0)
	for key, contents := range stored {
		if !host[key] && len(contents) > sha256.Size && bytes.HasPrefix(contents[sha256.Size:], opening) {
			standardLibrary[key] = true
		}
	}
	return host, standardLibrary
}

// A plain run never opens a record key of either kind, the host's or the standard library's (engine's condition on
// #6a3kjvz, pieces 2 and 3): over cohere-go tests' table, a cold run with neither switch writes no record; over the
// store a run under `--build-dependence` then filled, with a record beside every entry and one of each
// standard-library package it side-loaded, a warm run with neither switch reads no key of either kind and writes none,
// while a warm run under the switch reads every one.
//
// Not parallel: cache.ObserveReads is the process's, and one observer at a time can be set.
func TestAPlainRunReadsNoRecord(test *testing.T) {
	filled, _ := filledSystemCache(test, rules.All(), testsStamp, nil)
	root := copyFixture(test, standardLibraryRecordsFixture)
	reading := readingSystemCache(filled, "")
	runOverNoted(test, root, exitClean, reading)
	if host, standardLibrary := storedRecordKeys(test, root); len(host) != 0 || len(standardLibrary) != 0 {
		test.Errorf("a plain cold run wrote %d records of the host's and %d of the standard library's, want none",
			len(host), len(standardLibrary))
	}
	runOverNoted(test, root, exitClean, reading, recordsSwitches...)
	host, standardLibrary := storedRecordKeys(test, root)
	if len(host) == 0 || len(standardLibrary) == 0 {
		test.Fatalf("the store holds %d records of the host's and %d of the standard library's, want some of each",
			len(host), len(standardLibrary))
	}
	readsOf := func(switches ...string) (int, int) {
		// The template's fill reads keys of its own, so it is filled before the observer is set (templateFilled).
		templateFilled(test)
		var counting sync.Mutex
		hostReads, standardLibraryReads := 0, 0
		stop := cache.ObserveReads(func(key cache.Key) {
			counting.Lock()
			defer counting.Unlock()
			switch {
			case host[key]:
				hostReads++
			case standardLibrary[key]:
				standardLibraryReads++
			}
		})
		runOverNoted(test, root, exitClean, reading, switches...)
		stop()
		return hostReads, standardLibraryReads
	}
	if hostReads, standardLibraryReads := readsOf(); hostReads != 0 || standardLibraryReads != 0 {
		test.Errorf("a plain run read %d record keys of the host's and %d of the standard library's, want none",
			hostReads, standardLibraryReads)
	}
	if after, afterStandardLibrary := storedRecordKeys(test, root); len(after) != len(host) ||
		len(afterStandardLibrary) != len(standardLibrary) {
		test.Errorf("a plain warm run left %d and %d records, want the %d and %d before it", len(after),
			len(afterStandardLibrary), len(host), len(standardLibrary))
	}
	if hostReads, standardLibraryReads := readsOf(recordsSwitches...); hostReads < len(host) ||
		standardLibraryReads < len(standardLibrary) {
		test.Errorf("a run under --build-dependence read %d of the host's %d record keys and %d of the standard "+
			"library's %d", hostReads, len(host), standardLibraryReads, len(standardLibrary))
	}
}

// storedDeclarationsRecords are the first keys of the packages whose declarations-only record root's store holds, each
// a key the store holds whose check.DeclarationsRecordKey it holds too.
func storedDeclarationsRecords(test *testing.T, root string) []cache.Key {
	test.Helper()
	stored := map[cache.Key]bool{}
	walkError := filepath.WalkDir(storeDirectory(root), func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		// A file named for no key, as the store's packs are, is not an entry.
		decoded, decodeError := hex.DecodeString(entry.Name())
		if decodeError == nil && len(decoded) == len(cache.Key{}) {
			stored[cache.Key(decoded)] = true
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	var keys []cache.Key
	for key := range stored {
		if stored[check.DeclarationsRecordKey(key)] {
			keys = append(keys, key)
		}
	}
	return keys
}

// A run under `--build-dependence` refuses a declarations-only record found under a whole record's key as a record that
// does not decode, a miss (#anfjtr3). Over one copy of the build-dependence-embedded fixture, every package keyed into
// the project's store, a plain census run writes each package's declarations-only record, and each is then planted
// under its package's whole record key, as a collision of the two keys or a damaged store would leave it. The run
// under the switch counts every planted record unreadable, rather than hand it to the whole graph, which panics on a
// declarations-only record.
func TestABuildDependenceRunRefusesADeclarationsRecord(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "build-dependence-embedded"))
	runOverConfigured(test, root, exitClean, func(pipeline *pipeline) { pipeline.mustCheckCensus = test.TempDir() },
		"--no-system-cache")
	planted := storedDeclarationsRecords(test, root)
	if len(planted) == 0 {
		test.Fatal("the census run wrote no declarations record")
	}
	store := cache.Open(root, nil)
	for _, key := range planted {
		record, isStored := store.Get(check.DeclarationsRecordKey(key))
		if !isStored {
			test.Fatalf("the declarations record beside %x does not read", key)
		}
		if putError := store.Put(check.DependenceRecordKey(key), record); putError != nil {
			test.Fatal(putError)
		}
	}
	_, stream := runOverRaw(test, root, exitClean, "--no-system-cache", "--build-dependence")
	if counted := recordCounts(test, stream); counted.Unreadable != len(planted) || counted.Read != 0 {
		test.Errorf("the run counts %+v, want each of the %d planted records unreadable and none read", counted,
			len(planted))
	}
}
