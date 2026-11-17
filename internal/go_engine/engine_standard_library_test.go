package go_engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// engineTestsStamp is the stamp the engine's tests write their tables under, in the engine's identity's place.
const engineTestsStamp = "engine tests"

// standardLibraryFixture is the fixture whose standard-library packages the tests write tables of: it reaches
// errors, fmt, log/slog, strings and unicode/utf8, and its two findings read fmt's facts and slog's positions.
var standardLibraryFixture = filepath.Join("testdata", "standard-library")

// standardLibraryTableOf is the table of every standard-library package a cached run over a fresh copy of the fixture
// saves, under stamp, or the executable's identity when it is empty, with registered as the rules and prepare, when it
// is not nil, run over the copy first, the entries it was written from, and what every standard-library key of the run
// shared, by which a run under the same identity, build and fact analyzers reads it from the system cache
// (readingTable).
func standardLibraryTableOf(
	test *testing.T,
	registered []rule.Rule,
	stamp string,
	prepare func(root string),
) ([]byte, map[cache.Key]check.StandardLibraryRunEntry, cache.StandardLibraryContext) {
	test.Helper()
	root := copyFixture(test, standardLibraryFixture)
	if prepare != nil {
		prepare(root)
	}
	collected := map[cache.Key]check.StandardLibraryRunEntry{}
	// Without the system cache, as the release's generator collects, so every standard-library package is computed
	// and collected whatever the test's system cache already holds.
	engine, _, _ := runOverRegistered(test, root, exitFindings, registered, func(engine *pipeline) {
		engine.request.standardLibrary = standardLibrarySource{stamp: stamp, collected: collected}
	}, "--no-fix", "--no-system-cache")
	if len(collected) == 0 {
		test.Fatal("want the cached run to collect the standard library's entries")
	}
	context, keyed := engine.session.StandardLibraryContext()
	if !keyed {
		test.Fatal("want the cached run to key the standard library")
	}
	table, tableError := check.StandardLibraryTableOf(collected, goEnvironmentIn(test, root, nil)["GOROOT"], nil)
	if tableError != nil {
		test.Fatalf("writing the table: %v", tableError)
	}
	return table, collected, context
}

// readingTable is a run's standard library read from a system cache of the test's own that holds table as the table
// of context, under context's identity, the stamp it was written under.
func readingTable(test *testing.T, table []byte, context cache.StandardLibraryContext) standardLibrarySource {
	test.Helper()
	root := test.TempDir()
	if putError := cache.OpenAt(standardLibraryTablesDirectory(root)).Put(cache.StandardLibraryTableKey(context),
		table); putError != nil {
		test.Fatalf("putting the table in the system cache: %v", putError)
	}
	return standardLibrarySource{stamp: context.EngineIdentity, systemCacheRoot: root}
}

// onlyFromSystemCache is counts with only what a run under `--no-cache` may count kept: how many packages came from
// the system cache, and the facts restored from it whose object a package's export data lacks, which are skipped and
// counted.
func onlyFromSystemCache(counts contract.LintCache) contract.LintCache {
	return contract.LintCache{FromSystemCache: counts.FromSystemCache, FactsSkipped: counts.FactsSkipped}
}

// A run under `--no-cache` restores every standard-library package from the system cache's table: the count says how
// many, no store count moves, no store is made, and every record is the plain `--no-cache` run's, printf's finding
// from fmt's restored facts and structtag's, which names a line of log/slog's from its export data, among them.
func TestTheStandardLibraryComesFromTheSystemCacheUnderNoCache(test *testing.T) {
	test.Parallel()

	table, collected, context := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache")
	fromSystemCache := runEngineWith(test, root, readingTable(test, table, context), rules.All(), "--no-cache")
	if fromSystemCache.counts != onlyFromSystemCache(fromSystemCache.counts) ||
		fromSystemCache.counts.FromSystemCache != len(collected) {
		test.Fatalf("want all %d standard-library packages from the system cache and nothing else counted, got %+v",
			len(collected), fromSystemCache.counts)
	}
	if fromSystemCache.withoutCounts != plain.withoutCounts {
		test.Fatalf("from the system cache\n%s\nplain\n%s", fromSystemCache.withoutCounts, plain.withoutCounts)
	}
	for _, expected := range []string{`"rule":"cohere-go/printf"`, `"rule":"cohere-go/structtag"`, "also at"} {
		if !strings.Contains(fromSystemCache.stream, expected) {
			test.Errorf("want %s in\n%s", expected, fromSystemCache.stream)
		}
	}
	if _, statError := os.Stat(filepath.Join(root, ".cache")); !os.IsNotExist(statError) {
		test.Errorf("want no store made under --no-cache, got %v", statError)
	}
}

// A cached run restores the standard library from the system cache and the rest from its store: no standard-library
// package from the system cache is written to the store, so the run misses and writes only what a run without the
// table misses beyond it, and every record, warm or cold, is the plain `--no-cache` run's.
func TestTheSystemCachesEntriesChangeNoRecordInACachedRun(test *testing.T) {
	test.Parallel()

	table, collected, context := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	withoutTable := runCachedEngine(test, copyFixture(test, standardLibraryFixture))
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache")
	reading := readingTable(test, table, context)
	cold := runEngineWith(test, root, reading, rules.All())
	if !cold.counts.On || cold.counts.Hit != 0 || cold.counts.FromSystemCache != len(collected) ||
		cold.counts.Missed != withoutTable.counts.Missed-len(collected) || cold.counts.Written == 0 ||
		cold.counts.Written+cold.counts.Unstorable > cold.counts.Missed {
		test.Fatalf("want the %d standard-library packages from the system cache, and only the rest missed and "+
			"written, without the table %+v, with it %+v", len(collected), withoutTable.counts, cold.counts)
	}
	if cold.withoutCounts != plain.withoutCounts {
		test.Fatalf("cold with the table\n%s\nplain\n%s", cold.withoutCounts, plain.withoutCounts)
	}
	warm := runEngineWith(test, root, reading, rules.All())
	if warm.counts.Hit != cold.counts.Written || warm.counts.FromSystemCache != len(collected) ||
		warm.counts.Missed != cold.counts.Missed-cold.counts.Written {
		test.Fatalf("want every package written cold hit warm, beside the same from the system cache, cold %+v, "+
			"warm %+v", cold.counts, warm.counts)
	}
	if warm.withoutCounts != plain.withoutCounts {
		test.Fatalf("warm with the table\n%s\nplain\n%s", warm.withoutCounts, plain.withoutCounts)
	}
}

// plantedRecord is the findings cache's record of an entry as gob reads it, field for field by name, holding the
// entry's body (Inline) when it was written from source, so a test can change what a stored entry holds and write it
// back whole.
type plantedRecord struct {
	Version         string
	Body            []byte
	Inline          []byte
	BodyAt          []byte
	ExportData      []byte
	Reads           []plantedRead
	ReadsIncomplete bool
}

// plantedRead is one read a plantedRecord holds.
type plantedRead struct {
	Kind     string
	Owner    string
	OwnerKey []byte
	Path     string
	Value    []byte
}

// plantedEntry is the findings cache's entry as gob reads it, field for field by name, so a test can change what a
// stored entry's body holds and write it back.
type plantedEntry struct {
	Version   string
	Analyzers map[string]plantedAnalysis
	Imports   []string
	Withheld  load.PackageWithheld
	Files     []plantedFile
	Output    []byte
}

// plantedAnalysis is one analyzer's part of a plantedEntry.
type plantedAnalysis struct {
	Findings     []check.Finding
	ObjectFacts  []plantedObjectFact
	PackageFacts []plantedFact
}

// plantedObjectFact is a fact about one object, by its objectpath.
type plantedObjectFact struct {
	Path string
	Fact plantedFact
}

// plantedFact is one fact as its type's name and its gob bytes.
type plantedFact struct {
	Type    string
	Encoded []byte
}

// plantedFile is one file's walk.
type plantedFile struct {
	Name      string
	Nodes     int
	Generated bool
	Copied    bool
}

// Under `--no-cache` the system cache's entries are read and nothing else. A store a cached run wrote holds log/slog's
// entry under the key a cached run reads it by, planted with a withheld error the types record would name, and the
// table holds every standard-library package but log/slog. The run under `--no-cache` restores the rest from the table
// and computes log/slog, never reading the planted body, and leaves the store as it was; the cached run after it reads
// the planted body, so the entry was whole and under the key that matches. log/slog is the one package the table leaves
// out because no other standard-library package the fixture reaches imports it: export data is shallow, so a package
// left out of the table leaves every package that reaches it unreadable from the table too.
func TestOnlyTheSystemCachesEntriesAreReadUnderNoCache(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, standardLibraryFixture)
	collected := map[cache.Key]check.StandardLibraryRunEntry{}
	engine, _, _ := runOverNoted(test, root, exitFindings, func(engine *pipeline) {
		engine.request.standardLibrary = standardLibrarySource{stamp: engineTestsStamp, collected: collected}
	}, "--no-fix", "--no-system-cache")
	context, keyed := engine.session.StandardLibraryContext()
	if !keyed {
		test.Fatal("want the cached run to key the standard library")
	}
	var slogEntry plantedEntry
	found := false
	for key, entry := range collected {
		var decoded plantedEntry
		if decodeError := gob.NewDecoder(bytes.NewReader(entry.Entry)).Decode(&decoded); decodeError != nil {
			test.Fatalf("reading %s's collected entry: %v", entry.PackagePath, decodeError)
		}
		if slices.Contains(decoded.Imports, "log/slog") {
			test.Fatalf("want no standard-library package to import log/slog, got %s", entry.PackagePath)
		}
		if entry.PackagePath == "log/slog" {
			slogEntry = decoded
			found = true
			delete(collected, key)
		}
	}
	if !found {
		test.Fatal("want log/slog among the collected entries")
	}
	table, tableError := check.StandardLibraryTableOf(collected, goEnvironmentIn(test, root, nil)["GOROOT"], nil)
	if tableError != nil {
		test.Fatalf("writing the table: %v", tableError)
	}

	store := cache.Open(root)
	var stored []cache.Key
	var record plantedRecord
	walkError := filepath.WalkDir(filepath.Join(root, ".cache", "cohere", "go"),
		func(path string, entry fs.DirEntry, pathError error) error {
			if pathError != nil || entry.IsDir() {
				return pathError
			}
			decoded, decodeError := hex.DecodeString(entry.Name())
			if decodeError != nil || len(decoded) != len(cache.Key{}) {
				return nil
			}
			contents, found := store.Get(cache.Key(decoded))
			var read plantedRecord
			if !found || gob.NewDecoder(bytes.NewReader(contents)).Decode(&read) != nil || read.Inline == nil {
				return nil
			}
			var body plantedEntry
			if gob.NewDecoder(bytes.NewReader(read.Inline)).Decode(&body) == nil && reflect.DeepEqual(body,
				slogEntry) {
				stored, record = append(stored, cache.Key(decoded)), read
			}
			return nil
		})
	if walkError != nil || len(stored) != 1 {
		test.Fatalf("want log/slog's entry in the store once, found it under %v (%v)", stored, walkError)
	}
	planted := slogEntry
	planted.Withheld.Errors = append(planted.Withheld.Errors, load.WithheldError{
		File: filepath.Join(root, "planted_by_the_test.go"), Line: 1, Column: 1, Message: "planted by the test",
	})
	var body bytes.Buffer
	if encodeError := gob.NewEncoder(&body).Encode(planted); encodeError != nil {
		test.Fatalf("writing the planted body: %v", encodeError)
	}
	digest := sha256.Sum256(body.Bytes())
	record.Inline, record.Body = body.Bytes(), digest[:]
	var encoded bytes.Buffer
	if encodeError := gob.NewEncoder(&encoded).Encode(record); encodeError != nil {
		test.Fatalf("writing the planted record: %v", encodeError)
	}
	if putError := store.Put(stored[0], encoded.Bytes()); putError != nil {
		test.Fatalf("planting the entry: %v", putError)
	}

	before := entriesIn(test, root)
	uncached := runEngineWith(test, root, readingTable(test, table, context), rules.All(), "--no-cache")
	if strings.Contains(uncached.stream, "planted_by_the_test") {
		test.Fatalf("want the planted body never read under --no-cache, got\n%s", uncached.stream)
	}
	if uncached.counts != onlyFromSystemCache(uncached.counts) || uncached.counts.FromSystemCache != len(collected) {
		test.Fatalf("want the %d packages in the table from the system cache and nothing from the store, got %+v",
			len(collected), uncached.counts)
	}
	if after := entriesIn(test, root); len(after) != len(before) || !equalEntries(before, after) {
		test.Fatalf("want the store untouched under --no-cache, before %v, after %v", before, after)
	}
	plain := runCachedEngine(test, root, "--no-cache")
	if uncached.withoutCounts != plain.withoutCounts {
		test.Fatalf("from the system cache\n%s\nplain\n%s", uncached.withoutCounts, plain.withoutCounts)
	}
	// Without the system cache, which the runs above warmed with log/slog's entry, so the store is what is read.
	if cached := runCachedEngine(test, root, "--no-system-cache"); !strings.Contains(cached.stream,
		"planted_by_the_test") {
		test.Fatalf("want a cached run to read the planted entry, so it is whole and under log/slog's key, got\n%s",
			cached.stream)
	}
}

// equalEntries reports whether two listings of a store name the same files with the same modification times.
func equalEntries(before map[string]string, after map[string]string) bool {
	for path, modified := range before {
		if after[path] != modified {
			return false
		}
	}
	return len(before) == len(after)
}

// A table written under one engine identity restores nothing under another: every standard-library package computes,
// and the records are the plain run's.
func TestATableUnderAnotherIdentityRestoresNothing(test *testing.T) {
	test.Parallel()

	table, _, context := standardLibraryTableOf(test, rules.All(), "release one", nil)
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache")
	reading := readingTable(test, table, context)
	reading.stamp = "release two"
	other := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if other.counts != (contract.LintCache{}) || other.withoutCounts != plain.withoutCounts {
		test.Fatalf("want nothing from the system cache and the plain run's records, got %+v\n%s\nplain\n%s",
			other.counts, other.withoutCounts, plain.withoutCounts)
	}
}

// probeFact is the fact probeFactsRule's analyzer learns and never exports.
type probeFact struct {
	Probed bool
}

// AFact marks probeFact a fact.
func (*probeFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// probeFactsRule is a rule of one test's own whose analyzer learns facts and has one option, mode, which changes
// nothing it does but its flags. Its analyzer is new for each call, so a test that sets the option sets it on no other
// test's.
func probeFactsRule() rule.Rule {
	analyzer := &analysis.Analyzer{
		Name:      "probefacts",
		Doc:       "probefacts learns a fact type and does nothing",
		FactTypes: []analysis.Fact{new(probeFact)},
		Run:       func(*analysis.Pass) (any, error) { return nil, nil },
	}
	analyzer.Flags.String("mode", "", "changes nothing it does")
	return rule.Rule{Name: rule.Namespace + "correctness-probe-facts", Origin: rule.OriginHouse, Analyzer: analyzer}
}

// A table written while a fact analyzer had one option value restores nothing in a run where it has another, since
// every standard-library key holds the fact analyzers' options, and restores everything under the same value.
func TestATableUnderAnotherFactOptionRestoresNothing(test *testing.T) {
	test.Parallel()

	registered := append(rules.All(), probeFactsRule())
	settingMode := func(mode string) func(root string) {
		return func(root string) {
			settings := `{"go": {"rules": {"cohere-go/correctness-probe-facts": ["error", {"mode": "` + mode + `"}]}}}`
			if writeError := os.WriteFile(filepath.Join(root, "CohereSettings.json"), []byte(settings),
				0o644); writeError != nil {
				test.Fatalf("writing the settings: %v", writeError)
			}
		}
	}
	table, collected, context := standardLibraryTableOf(test, registered, engineTestsStamp, settingMode("first"))
	for _, reading := range []struct {
		mode            string
		fromSystemCache int
	}{{mode: "first", fromSystemCache: len(collected)}, {mode: "second"}} {
		root := copyFixture(test, standardLibraryFixture)
		settingMode(reading.mode)(root)
		plain := runEngineWith(test, root, standardLibrarySource{}, registered, "--no-cache")
		read := runEngineWith(test, root, readingTable(test, table, context), registered, "--no-cache")
		if read.counts.FromSystemCache != reading.fromSystemCache || read.withoutCounts != plain.withoutCounts {
			test.Errorf("mode %s: want %d from the system cache and the plain run's records, got %+v\n%s\nplain\n%s",
				reading.mode, reading.fromSystemCache, read.counts, read.withoutCounts, plain.withoutCounts)
		}
	}
}

// A table written under one build restores nothing under another: a run whose go flags name a build tag keys every
// standard-library package apart, though no file the tag could choose is among them.
//
// Not parallel: it sets the process's own `GOFLAGS`, which every go command the engine runs reads.
func TestATableUnderAnotherBuildRestoresNothing(test *testing.T) {
	table, _, context := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	reading := readingTable(test, table, context)
	test.Setenv("GOFLAGS", "-tags=probe")
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache")
	other := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if other.counts != (contract.LintCache{}) || other.withoutCounts != plain.withoutCounts {
		test.Fatalf("want nothing from the system cache and the plain run's records, got %+v\n%s\nplain\n%s",
			other.counts, other.withoutCounts, plain.withoutCounts)
	}
}

// mirroredGoRoot is a Go installation at a directory of the test's own that is the local one entry for entry, each a
// symbolic link to the local entry, but along the path to changed, a file below it given as a slash-separated path
// whose directories are made and whose own bytes are the local file's with suffix after them.
func mirroredGoRoot(test *testing.T, goRoot string, changed string, suffix string) string {
	test.Helper()
	mirror, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	mirror = filepath.Join(mirror, "goroot")
	elements := strings.Split(changed, "/")
	source, target := goRoot, mirror
	for index, element := range elements {
		if makeError := os.MkdirAll(target, 0o755); makeError != nil {
			test.Fatalf("making %s: %v", target, makeError)
		}
		entries, readError := os.ReadDir(source)
		if readError != nil {
			test.Fatalf("listing %s: %v", source, readError)
		}
		for _, entry := range entries {
			if entry.Name() == element {
				continue
			}
			if linkError := os.Symlink(filepath.Join(source, entry.Name()),
				filepath.Join(target, entry.Name())); linkError != nil {
				test.Fatalf("linking %s: %v", entry.Name(), linkError)
			}
		}
		source, target = filepath.Join(source, element), filepath.Join(target, element)
		if index == len(elements)-1 {
			contents, fileError := os.ReadFile(source)
			if fileError != nil {
				test.Fatalf("reading %s: %v", source, fileError)
			}
			if writeError := os.WriteFile(target, append(contents, suffix...), 0o644); writeError != nil {
				test.Fatalf("writing %s: %v", target, writeError)
			}
		}
	}
	return mirror
}

// A standard-library file whose bytes are not the ones the table was written from misses its package, and every
// package that imports it, directly or not: an installation like the local one but for a comment added to
// unicode/utf8's utf8.go restores some packages from the system cache and computes the rest, and the records are the
// plain run's under that installation.
//
// Not parallel: it sets the process's own `GOROOT`, which every go command the engine runs reads.
func TestAStandardLibraryFileWithOtherBytesMisses(test *testing.T) {
	table, collected, context := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryFixture)
	goRoot := goEnvironmentIn(test, root, nil)["GOROOT"]
	test.Setenv("GOROOT", mirroredGoRoot(test, goRoot, "src/unicode/utf8/utf8.go", "\n// Changed by the test.\n"))
	plain := runCachedEngine(test, root, "--no-cache")
	changed := runEngineWith(test, root, readingTable(test, table, context), rules.All(), "--no-cache")
	if changed.counts.FromSystemCache == 0 || changed.counts.FromSystemCache >= len(collected) {
		test.Fatalf("want some of the %d packages from the system cache and unicode/utf8's importers computed, got %+v",
			len(collected), changed.counts)
	}
	if changed.withoutCounts != plain.withoutCounts {
		test.Fatalf("with utf8.go changed\n%s\nplain\n%s", changed.withoutCounts, plain.withoutCounts)
	}
}

// The same Go installation reached through a symbolic link at another path restores every package the table holds,
// its export data renamed to the path the run names it by: structtag's finding names log/slog's line by that path, as
// the plain run's does, so the records are the plain run's.
//
// Not parallel: it sets the process's own `GOROOT`, which every go command the engine runs reads.
func TestTheSystemCachesEntriesReadThroughASymlinkedGoroot(test *testing.T) {
	table, collected, context := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryFixture)
	goRoot := goEnvironmentIn(test, root, nil)["GOROOT"]
	linked, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	linked = filepath.Join(linked, "linked-goroot")
	if linkError := os.Symlink(goRoot, linked); linkError != nil {
		test.Fatalf("linking %s: %v", linked, linkError)
	}
	test.Setenv("GOROOT", linked)
	if reported := goEnvironmentIn(test, root, nil)["GOROOT"]; reported != linked {
		test.Fatalf("want go to report GOROOT as the link %s, got %s", linked, reported)
	}
	plain := runCachedEngine(test, root, "--no-cache")
	read := runEngineWith(test, root, readingTable(test, table, context), rules.All(), "--no-cache")
	if read.counts.FromSystemCache != len(collected) {
		test.Fatalf("want all %d packages from the system cache through the link, got %+v", len(collected), read.counts)
	}
	if !strings.Contains(plain.stream, "linked-goroot") {
		test.Fatalf("want the plain run's structtag finding to name slog's file through the link, got\n%s",
			plain.stream)
	}
	if read.withoutCounts != plain.withoutCounts {
		test.Fatalf("through the link\n%s\nplain\n%s", read.withoutCounts, plain.withoutCounts)
	}
}
