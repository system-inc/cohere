package go_engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// The dependence-records fixture's packages (#6a3kjvz): platform, whose Make returns a Plain alike in every build,
// and whose Locked holds a Lock windows declares its own way, and reader, which compares two values Make returns.
const (
	recordsPlatform = "example.com/records/platform"
	recordsReader   = "example.com/records/reader"
)

// recordsSwitches are a run judging every build's dependence, the findings cache on in the fixture's own store. Every
// run here is read raw (runOverRaw), since normalize zeroes the cache's counts.
var recordsSwitches = []string{"--other-builds", "--build-dependence"}

// graphVerdicts are every unit of every build's judgment of the host's graph, by build, package, file and start,
// each file relative to root, and whether the build makes it dependent, so two runs over two copies of a fixture
// compare.
func graphVerdicts(test *testing.T, engine *pipeline, root string) map[string]bool {
	test.Helper()
	verdicts := map[string]bool{}
	for _, run := range engine.buildRuns {
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
// record cut short, with the store's sum written again so the store reads it whole, leaves a warm run that counts it
// unreadable, names it among the cache's problems, and backfills reader, checking it from source and writing its
// record again, so its graph is the cold run's; the run after reads both records whole.
func TestAnUnreadableRecordIsAProblem(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "dependence-records"))
	cold, _ := runOverRaw(test, root, exitClean, recordsSwitches...)
	file := recordFileOf(test, root, recordsReader)
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatal(readError)
	}
	body := contents[sha256.Size : len(contents)-3]
	sum := sha256.Sum256(body)
	if writeError := os.WriteFile(file, append(sum[:], body...), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	healed, healedStream, noted := runOverNoted(test, root, exitClean, nil, recordsSwitches...)
	if counted := recordCounts(test, healedStream); counted != (contract.DependenceRecords{Read: 2, Written: 1,
		Backfilled: 1, Unreadable: 1}) {
		test.Errorf("the run counts %+v, want base's and platform's records read, reader's unreadable, backfilled "+
			"and written", counted)
	}
	if !strings.Contains(noted, "reading the dependence record of "+recordsReader) {
		test.Errorf("the run's notes say nothing of reader's record:\n%s", noted)
	}
	expectSameVerdicts(test, "healed against cold", graphVerdicts(test, cold, root), graphVerdicts(test, healed, root))
	_, afterStream := runOverRaw(test, root, exitClean, recordsSwitches...)
	if after := recordCounts(test, afterStream); after != (contract.DependenceRecords{Read: 3}) {
		test.Errorf("the run after counts %+v, want every record read and nothing else", after)
	}
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

// A plain run never opens a record key (engine's condition on #6a3kjvz): over a store a run under
// `--build-dependence` filled, with records beside every entry, a warm run with neither switch reads no key that is a
// record's, every one derived from a key the store holds, while a warm run under the switch reads them.
//
// Not parallel: cache.ObserveReads is the process's, and one observer at a time can be set.
func TestAPlainRunReadsNoRecord(test *testing.T) {
	root := copyFixture(test, filepath.Join("testdata", "dependence-records"))
	runOverRaw(test, root, exitClean, recordsSwitches...)
	recordKeys := map[cache.Key]bool{}
	walkError := filepath.WalkDir(storeDirectory(root), func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		decoded, decodeError := hex.DecodeString(entry.Name())
		if decodeError != nil || len(decoded) != len(cache.Key{}) {
			return nil
		}
		recordKeys[check.DependenceRecordKey(cache.Key(decoded))] = true
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	readsOf := func(switches ...string) int {
		var reading sync.Mutex
		var read []cache.Key
		stop := cache.ObserveReads(func(key cache.Key) {
			reading.Lock()
			defer reading.Unlock()
			read = append(read, key)
		})
		runOverRaw(test, root, exitClean, switches...)
		stop()
		return len(slices.DeleteFunc(read, func(key cache.Key) bool { return !recordKeys[key] }))
	}
	if plain := readsOf("--other-builds"); plain != 0 {
		test.Errorf("a plain run read %d record keys, want none", plain)
	}
	if switched := readsOf(recordsSwitches...); switched == 0 {
		test.Error("a run under --build-dependence read no record key, so the plain run's none proves nothing")
	}
}
