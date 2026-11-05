package program

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The readiness half of the findings cache (#drbrp8c). A run that measures Adamic readiness must never be
// answered by an entry from a run that did not: that file was never measured, and replaying it reads as measured
// and clean. A run that does not measure may use either kind, since measure-only findings never enter Findings.

func adamicFixtureReuse(test *testing.T, entry LintCacheEntry) (*FindingsReuse, cacheKeys) {
	test.Helper()
	key := HashRuleSet([]string{"fixture"})
	keys := cacheKeys{contentHash: entry.ContentHash, pure: entry.Rules, typed: entry.TypedRules, typeFingerprint: entry.TypeFingerprint}
	previous := &LintCache{Version: lintCacheVersion, Key: key, Entries: []LintCacheEntry{entry}}
	return NewFindingsReuse(key, previous, PathAnchor{}), keys
}

func adamicFixtureEntry(record *AdamicRecord) LintCacheEntry {
	return LintCacheEntry{
		Path:            "/project/a.ts",
		ContentHash:     HashContent("const a = 1;\n"),
		Rules:           []string{"adamic/single-spread", "no-var"},
		TypedRules:      []string{"adamic/invariant-mutable"},
		TypeFingerprint: sha256.Sum256([]byte("types")),
		Adamic:          record,
	}
}

func TestAReadinessRunMissesAnEntryThatWasNeverMeasured(test *testing.T) {
	test.Parallel()
	reuse, keys := adamicFixtureReuse(test, adamicFixtureEntry(nil))
	reuse.MeasureReadiness()
	if _, hits := reuse.lookup("/project/a.ts", keys, ""); hits.pure {
		test.Fatal("a readiness run replayed an entry recorded without readiness, so an unmeasured file would read as ready")
	}
	if reuse.Replayed() != 0 {
		test.Fatalf("a miss counted as %d replayed files", reuse.Replayed())
	}
}

func TestAReadinessRunReplaysAMeasuredEntry(test *testing.T) {
	test.Parallel()
	record := &AdamicRecord{Counts: []AdamicCount{{Rule: "adamic/invariant-mutable", Findings: 2}, {Rule: "adamic/single-spread"}}}
	reuse, keys := adamicFixtureReuse(test, adamicFixtureEntry(record))
	reuse.MeasureReadiness()
	entry, hits := reuse.lookup("/project/a.ts", keys, "")
	if !hits.pure || !hits.typed {
		test.Fatalf("a measured entry under unchanged keys missed: %+v", hits)
	}
	if got := replayedAdamic(entry, hits); !reflect.DeepEqual(got, record) {
		test.Fatalf("replayed %+v, recorded %+v", got, record)
	}
}

// A run that does not measure needs nothing from the record, so either kind of entry answers it.
func TestARunThatDoesNotMeasureReplaysEitherEntry(test *testing.T) {
	test.Parallel()
	for _, record := range []*AdamicRecord{nil, {Counts: []AdamicCount{{Rule: "adamic/single-spread", Findings: 1}}}} {
		reuse, keys := adamicFixtureReuse(test, adamicFixtureEntry(record))
		if _, hits := reuse.lookup("/project/a.ts", keys, ""); !hits.pure {
			test.Fatalf("a run without readiness missed an entry with record %+v", record)
		}
	}
}

// When an imported file's types change, only the type-aware rules run again, and the record is merged rule by
// rule: the pure rules' counts stay, the type-aware rules' counts are this walk's.
func TestARefreshMergesTheReadinessRecordRuleByRule(test *testing.T) {
	test.Parallel()
	old := adamicFixtureEntry(&AdamicRecord{
		Counts:  []AdamicCount{{Rule: "adamic/invariant-mutable", Findings: 3}, {Rule: "adamic/single-spread", Findings: 1}},
		Skipped: []string{"adamic/invariant-mutable"},
	})
	keys := cacheKeys{contentHash: old.ContentHash, pure: old.Rules, typed: old.TypedRules, typeFingerprint: sha256.Sum256([]byte("moved"))}
	hits := classHits{pure: true, shaped: true, design: true}
	walked := &AdamicRecord{Counts: []AdamicCount{{Rule: "adamic/invariant-mutable"}, {Rule: "adamic/uncacheable", Findings: 4}}}

	refreshed, eligible := refreshClasses(old, keys, hits, nil, nil, nil, walked, suppressionTally{})
	if !eligible {
		test.Fatal("a refresh with no fixes was refused")
	}
	want := &AdamicRecord{Counts: []AdamicCount{{Rule: "adamic/invariant-mutable"}, {Rule: "adamic/single-spread", Findings: 1}}}
	if !reflect.DeepEqual(refreshed.Adamic, want) {
		test.Fatalf("refreshed record %+v, want %+v: the stale type-aware count or skip survived, or the uncacheable rule was stored",
			refreshed.Adamic, want)
	}

	if unmeasured, _ := refreshClasses(old, keys, hits, nil, nil, nil, nil, suppressionTally{}); unmeasured.Adamic != nil {
		test.Fatalf("a refresh whose walk measured nothing kept a record, %+v, half of it stale", unmeasured.Adamic)
	}
}

// Only the cacheable rules' part of what the walk measured is recorded: an uncacheable rule runs on every walk,
// so a stored count for it would be counted twice on replay.
func TestARecordedEntryKeepsOnlyTheCacheableRulesReadiness(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	if writeError := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions": {"noEmit": true}}`), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if writeError := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a = 1;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	graph, buildError := Build(Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
	if buildError != nil {
		test.Fatal(buildError)
	}
	sourceFile := graph.ProjectFiles()[0]
	keys := cacheKeys{contentHash: HashContent(sourceFile.Text()), pure: []string{"adamic/single-spread"}}
	walked := &AdamicRecord{
		Counts:  []AdamicCount{{Rule: "adamic/single-spread", Findings: 1}, {Rule: "adamic/uncacheable", Findings: 4}},
		Skipped: []string{"adamic/uncacheable"},
	}

	entry, eligible := recordableEntry(sourceFile, keys, nil, nil, nil, walked, 0, suppressionTally{})
	if !eligible {
		test.Fatal("a clean file was refused")
	}
	want := &AdamicRecord{Counts: []AdamicCount{{Rule: "adamic/single-spread", Findings: 1}}}
	if !reflect.DeepEqual(entry.Adamic, want) {
		test.Fatalf("recorded %+v, want %+v", entry.Adamic, want)
	}

	if unmeasured, _ := recordableEntry(sourceFile, keys, nil, nil, nil, nil, 0, suppressionTally{}); unmeasured.Adamic != nil {
		test.Fatalf("a walk that measured nothing recorded %+v", unmeasured.Adamic)
	}
}
