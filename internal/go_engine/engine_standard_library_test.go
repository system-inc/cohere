package go_engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// engineTestsStamp is the stamp the engine's tests write their tables under, in the engine's identity's place.
const engineTestsStamp = "engine tests"

// standardLibraryFixture is the fixture whose standard-library packages the tests write tables of: it reaches
// errors, fmt, log/slog, strings and unicode/utf8, and its two findings read fmt's facts and slog's positions.
var standardLibraryFixture = filepath.Join("testdata", "standard-library")

// filledSystemCache is a run's standard library from a system cache of the test's own, filled by a run under
// `--no-cache` over a fresh copy of the fixture, under stamp, or the executable's identity when it is empty, with
// registered as the rules and prepare, when it is not nil, run over the copy first, and how many standard-library
// entries that run wrote there: a later run under the same identity, build, fact analyzers and `GOROOT` restores
// them, and one under any other keys every package apart.
func filledSystemCache(
	test *testing.T,
	registered []rule.Rule,
	stamp string,
	prepare func(root string),
) (standardLibrarySource, int) {
	test.Helper()
	root := copyFixture(test, standardLibraryFixture)
	if prepare != nil {
		prepare(root)
	}
	source := standardLibrarySource{stamp: stamp, systemCacheRoot: test.TempDir()}
	filling := runEngineWith(test, root, source, registered, "--no-cache")
	if filling.counts.SystemCacheWritten == 0 {
		test.Fatalf("want the filling run to write the standard library to the system cache, got %+v", filling.counts)
	}
	return source, filling.counts.SystemCacheWritten
}

// onlyFromSystemCache is counts with only what a run under `--no-cache` may count kept: how many packages came from
// the system cache and how many entries it wrote there, and the facts restored from it whose object a package's export
// data lacks, which are skipped and counted.
func onlyFromSystemCache(counts contract.LintCache) contract.LintCache {
	return contract.LintCache{FromSystemCache: counts.FromSystemCache, SystemCacheWritten: counts.SystemCacheWritten,
		FactsSkipped: counts.FactsSkipped}
}

// A run under `--no-cache` restores every standard-library package from the system cache's table: the count says how
// many, no store count moves, no store is made, and every record is the plain `--no-cache` run's, printf's finding
// from fmt's restored facts and structtag's, which names a line of log/slog's from its export data, among them.
func TestTheStandardLibraryComesFromTheSystemCacheUnderNoCache(test *testing.T) {
	test.Parallel()

	reading, written := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache")
	fromSystemCache := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if fromSystemCache.counts != onlyFromSystemCache(fromSystemCache.counts) ||
		fromSystemCache.counts.FromSystemCache != written || fromSystemCache.counts.SystemCacheWritten != 0 {
		test.Fatalf("want all %d standard-library packages from the system cache, none written and nothing else "+
			"counted, got %+v", written, fromSystemCache.counts)
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

	reading, written := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	withoutTable := runCachedEngine(test, copyFixture(test, standardLibraryFixture), "--no-system-cache")
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache")
	cold := runEngineWith(test, root, reading, rules.All())
	if !cold.counts.On || cold.counts.Hit != 0 || cold.counts.FromSystemCache != written ||
		cold.counts.Missed != withoutTable.counts.Missed-written || cold.counts.Written == 0 ||
		cold.counts.Written+cold.counts.Unstorable > cold.counts.Missed {
		test.Fatalf("want the %d standard-library packages from the system cache, and only the rest missed and "+
			"written, without the table %+v, with it %+v", written, withoutTable.counts, cold.counts)
	}
	if cold.withoutCounts != plain.withoutCounts {
		test.Fatalf("cold with the table\n%s\nplain\n%s", cold.withoutCounts, plain.withoutCounts)
	}
	warm := runEngineWith(test, root, reading, rules.All())
	if warm.counts.Hit != cold.counts.Written || warm.counts.FromSystemCache != written ||
		warm.counts.Missed != cold.counts.Missed-cold.counts.Written {
		test.Fatalf("want every package written cold hit warm, beside the same from the system cache, cold %+v, "+
			"warm %+v", cold.counts, warm.counts)
	}
	if warm.withoutCounts != plain.withoutCounts {
		test.Fatalf("warm with the table\n%s\nplain\n%s", warm.withoutCounts, plain.withoutCounts)
	}
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

// equalEntries reports whether two listings of a store name the same files with the same modification times.
func equalEntries(before map[string]string, after map[string]string) bool {
	for path, modified := range before {
		if after[path] != modified {
			return false
		}
	}
	return len(before) == len(after)
}

// The system cache's entries written under one engine identity restore nothing under another: every standard-library
// package computes, and the records are the plain run's.
func TestASystemCacheUnderAnotherIdentityRestoresNothing(test *testing.T) {
	test.Parallel()

	reading, _ := filledSystemCache(test, rules.All(), "release one", nil)
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache")
	reading.stamp = "release two"
	other := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if other.counts.FromSystemCache != 0 || other.withoutCounts != plain.withoutCounts {
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

// The system cache's entries written while a fact analyzer had one option value restore nothing in a run where it has
// another, since every standard-library key holds the fact analyzers' options, and restore everything under the same
// value.
func TestASystemCacheUnderAnotherFactOptionRestoresNothing(test *testing.T) {
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
	filled, written := filledSystemCache(test, registered, engineTestsStamp, settingMode("first"))
	for _, reading := range []struct {
		mode            string
		fromSystemCache int
	}{{mode: "first", fromSystemCache: written}, {mode: "second"}} {
		root := copyFixture(test, standardLibraryFixture)
		settingMode(reading.mode)(root)
		plain := runEngineWith(test, root, standardLibrarySource{}, registered, "--no-cache")
		read := runEngineWith(test, root, filled, registered, "--no-cache")
		if read.counts.FromSystemCache != reading.fromSystemCache || read.withoutCounts != plain.withoutCounts {
			test.Errorf("mode %s: want %d from the system cache and the plain run's records, got %+v\n%s\nplain\n%s",
				reading.mode, reading.fromSystemCache, read.counts, read.withoutCounts, plain.withoutCounts)
		}
	}
}

// The system cache's entries written under one build restore nothing under another: a run whose go flags name a build
// tag keys every standard-library package apart, though no file the tag could choose is among them.
//
// Not parallel: it sets the process's own `GOFLAGS`, which every go command the engine runs reads.
func TestASystemCacheUnderAnotherBuildRestoresNothing(test *testing.T) {
	reading, _ := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	test.Setenv("GOFLAGS", "-tags=probe")
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache")
	other := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if other.counts.FromSystemCache != 0 || other.withoutCounts != plain.withoutCounts {
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

// A standard-library file edited in place, its installation where it was, misses its package and every package that
// imports it, directly or not: over an installation like the local one but for its own copy of unicode/utf8's
// utf8.go, the system cache filled there restores some packages and computes the rest once a comment is added to that
// copy, and the records are the plain run's under the edited installation.
//
// Not parallel: it sets the process's own `GOROOT`, which every go command the engine runs reads.
func TestAStandardLibraryFileEditedInPlaceMisses(test *testing.T) {
	root := copyFixture(test, standardLibraryFixture)
	goRoot := goEnvironmentIn(test, root, nil)["GOROOT"]
	mirror := mirroredGoRoot(test, goRoot, "src/unicode/utf8/utf8.go", "")
	test.Setenv("GOROOT", mirror)
	reading, written := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	edited, openError := os.OpenFile(filepath.Join(mirror, "src", "unicode", "utf8", "utf8.go"),
		os.O_APPEND|os.O_WRONLY, 0)
	if openError != nil {
		test.Fatal(openError)
	}
	_, appendError := edited.WriteString("\n// Changed by the test.\n")
	if closeError := edited.Close(); appendError != nil || closeError != nil {
		test.Fatalf("editing utf8.go: %v %v", appendError, closeError)
	}
	plain := runCachedEngine(test, root, "--no-cache")
	changed := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if changed.counts.FromSystemCache == 0 || changed.counts.FromSystemCache >= written {
		test.Fatalf("want some of the %d packages from the system cache and unicode/utf8's importers computed, got %+v",
			written, changed.counts)
	}
	if changed.withoutCounts != plain.withoutCounts {
		test.Fatalf("with utf8.go changed\n%s\nplain\n%s", changed.withoutCounts, plain.withoutCounts)
	}
}

// The same Go installation reached through a symbolic link at another path is another `GOROOT`, which every
// standard-library key holds, since an entry holds its installation's paths: the system cache filled through the
// real path restores nothing through the link, and the records are the plain run's there, structtag's finding naming
// log/slog's line by the link.
//
// Not parallel: it sets the process's own `GOROOT`, which every go command the engine runs reads.
func TestTheSystemCacheKeysAGorootReachedThroughALinkApart(test *testing.T) {
	reading, _ := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
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
	read := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if read.counts.FromSystemCache != 0 {
		test.Fatalf("want nothing from the system cache through the link, got %+v", read.counts)
	}
	if !strings.Contains(plain.stream, "linked-goroot") {
		test.Fatalf("want the plain run's structtag finding to name slog's file through the link, got\n%s",
			plain.stream)
	}
	if read.withoutCounts != plain.withoutCounts {
		test.Fatalf("through the link\n%s\nplain\n%s", read.withoutCounts, plain.withoutCounts)
	}
}

// The system cache's entries written under one `CGO_ENABLED` restore nothing under the other, since every
// standard-library key holds it, and a package checked with cgo is another package than one checked without.
//
// Not parallel: it sets the process's own `CGO_ENABLED`, which every go command the engine runs reads.
func TestASystemCacheUnderAnotherCgoSettingRestoresNothing(test *testing.T) {
	reading, _ := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryFixture)
	other := map[string]string{"1": "0", "0": "1"}[goEnvironmentIn(test, root, nil)["CGO_ENABLED"]]
	test.Setenv("CGO_ENABLED", other)
	plain := runCachedEngine(test, root, "--no-cache", "--no-system-cache")
	read := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if read.counts.FromSystemCache != 0 || read.withoutCounts != plain.withoutCounts {
		test.Fatalf("CGO_ENABLED=%s: want nothing from the system cache and the plain run's records, got %+v\n%s\n"+
			"plain\n%s", other, read.counts, read.withoutCounts, plain.withoutCounts)
	}
}
