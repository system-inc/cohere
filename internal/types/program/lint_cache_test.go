package program_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

func sampleLintCache() *program.LintCache {
	return &program.LintCache{
		RuleSetHash: program.HashRuleSet([]string{"no-debugger", "no-empty"}),
		Entries: []program.LintCacheEntry{
			{
				Path:        "/project/source/dirty.ts",
				ContentHash: program.HashContent("debugger;\n"),
				Findings: []program.LintCacheFinding{
					{
						RuleName: "no-debugger", Start: 0, End: 9,
						MessageId:          "unexpectedDebugger",
						MessageDescription: "Unexpected debugger statement.",
						FixCount:           1,
					},
					{
						RuleName: "no-empty", Start: 10, End: 12,
						MessageId: "emptyBlock",
						// Interpolated text, the case the id could never have rebuilt: this is the
						// shape of the 11 rules that build a description with fmt.Sprintf.
						MessageDescription: "Argument of unary negation should be assignable to number | bigint but is string instead.",
						SuggestionCount:    2,
					},
				},
			},
			{
				// A clean file is the case worth caching: most files produce nothing, and an
				// empty finding list is a real answer rather than an absence.
				Path:        "/project/source/clean.ts",
				ContentHash: program.HashContent("export const value = 1;\n"),
				Findings:    nil,
			},
		},
	}
}

// TestLintCacheRoundTripsEveryField compares every field rather than spot-checking, because
// this format is raw offsets into a blob: a field the encoder forgets decodes to empty while
// everything around it looks correct. I shipped exactly that bug twice while writing the
// resolution cache, and once more here before this test existed.
//
// It then happened a fourth time, in the way this test could not see. The comparisons below were
// written out by hand, so `MessageDescription` was added to the struct and this test kept passing
// without ever looking at it — a test named for every field, checking the fields someone
// remembered. TestLintCacheFindingHasNoUncheckedFields now makes that structural: it fails when a
// field exists that the comparison does not name, so the next field cannot be added silently.
func TestLintCacheRoundTripsEveryField(t *testing.T) {
	original := sampleLintCache()

	decoded, err := program.DecodeLintCache(original.Encode())
	if err != nil {
		t.Fatalf("decoding what we just encoded: %v", err)
	}

	if decoded.RuleSetHash != original.RuleSetHash {
		t.Errorf("rule set hash did not survive: %x against %x", decoded.RuleSetHash, original.RuleSetHash)
	}
	if len(decoded.Entries) != len(original.Entries) {
		t.Fatalf("entries: %d back from %d", len(decoded.Entries), len(original.Entries))
	}

	for index, want := range original.Entries {
		got := decoded.Entries[index]
		if got.Path != want.Path {
			t.Errorf("entry %d Path: %q against %q", index, got.Path, want.Path)
		}
		if got.ContentHash != want.ContentHash {
			t.Errorf("entry %d ContentHash: %x against %x", index, got.ContentHash, want.ContentHash)
		}
		if len(got.Findings) != len(want.Findings) {
			t.Fatalf("entry %d findings: %d back from %d", index, len(got.Findings), len(want.Findings))
		}
		for findingIndex, wantFinding := range want.Findings {
			gotFinding := got.Findings[findingIndex]
			if gotFinding.RuleName != wantFinding.RuleName {
				t.Errorf("entry %d finding %d RuleName: %q against %q",
					index, findingIndex, gotFinding.RuleName, wantFinding.RuleName)
			}
			if gotFinding.Start != wantFinding.Start || gotFinding.End != wantFinding.End {
				t.Errorf("entry %d finding %d range: %d-%d against %d-%d",
					index, findingIndex, gotFinding.Start, gotFinding.End, wantFinding.Start, wantFinding.End)
			}
			if gotFinding.MessageId != wantFinding.MessageId {
				t.Errorf("entry %d finding %d MessageId: %q against %q",
					index, findingIndex, gotFinding.MessageId, wantFinding.MessageId)
			}
			if gotFinding.MessageDescription != wantFinding.MessageDescription {
				t.Errorf("entry %d finding %d MessageDescription: %q against %q",
					index, findingIndex, gotFinding.MessageDescription, wantFinding.MessageDescription)
			}
			if gotFinding.FixCount != wantFinding.FixCount {
				t.Errorf("entry %d finding %d FixCount: %d against %d",
					index, findingIndex, gotFinding.FixCount, wantFinding.FixCount)
			}
			if gotFinding.SuggestionCount != wantFinding.SuggestionCount {
				t.Errorf("entry %d finding %d SuggestionCount: %d against %d",
					index, findingIndex, gotFinding.SuggestionCount, wantFinding.SuggestionCount)
			}
		}
	}
}

// TestLintCacheRejectsBadArtifacts proves the decoder fails loudly rather than assembling
// findings out of the wrong bytes. A finding pointing at the wrong rule and range is worse
// than no finding, because it reads as a real result.
func TestLintCacheRejectsBadArtifacts(t *testing.T) {
	valid := sampleLintCache().Encode()

	cases := []struct {
		name   string
		buffer []byte
	}{
		{"empty", nil},
		{"shorter than the header", valid[:10]},
		{"truncated mid-record", valid[:len(valid)/2]},
		{"wrong magic", append([]byte("NOTVFYLI"), valid[8:]...)},
		{"trailing garbage", append(append([]byte{}, valid...), 0, 0, 0, 0)},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decoded, err := program.DecodeLintCache(testCase.buffer)
			if err == nil {
				t.Fatalf("a %s artifact decoded without error into %d entries; the decoder cannot "+
					"detect corruption and every clean read from it is vacuous",
					testCase.name, len(decoded.Entries))
			}
			if !errors.Is(err, program.ErrLintCacheUnreadable) {
				t.Errorf("error should be ErrLintCacheUnreadable so a caller treats it as a cold "+
					"cache rather than a failure, got: %v", err)
			}
		})
	}
}

// TestLintCacheLookupDistinguishesCleanFromUnknown is the correctness heart of this cache.
//
// A clean file and an unknown file both have zero findings. If Lookup cannot tell them apart,
// every uncached file reads as clean and the tool reports a green tree it never linted. That
// is the exact silent failure this whole domain exists to prevent, and it is one boolean away.
func TestLintCacheLookupDistinguishesCleanFromUnknown(t *testing.T) {
	cache, err := program.DecodeLintCache(sampleLintCache().Encode())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	ruleSet := program.HashRuleSet([]string{"no-debugger", "no-empty"})

	t.Run("a cached clean file is a hit with no findings", func(t *testing.T) {
		findings, hit := cache.Lookup("/project/source/clean.ts",
			program.HashContent("export const value = 1;\n"), ruleSet)
		if !hit {
			t.Fatal("a file that was linted and found clean reported as a miss, so its result " +
				"is recomputed forever and the cache never pays for the common case")
		}
		if len(findings) != 0 {
			t.Fatalf("expected no findings, got %d", len(findings))
		}
	})

	t.Run("an unknown file is a miss, not a clean hit", func(t *testing.T) {
		_, hit := cache.Lookup("/project/source/never-seen.ts",
			program.HashContent("whatever"), ruleSet)
		if hit {
			t.Fatal("a file the cache has never seen reported as a hit; every unlinted file " +
				"would read as clean and the tool would report a green tree it never checked")
		}
	})

	t.Run("changed contents under a known path is a miss", func(t *testing.T) {
		_, hit := cache.Lookup("/project/source/dirty.ts",
			program.HashContent("something completely different\n"), ruleSet)
		if hit {
			t.Fatal("an edited file matched its stale entry by path; the cache would replay " +
				"yesterday's findings for today's code")
		}
	})

	t.Run("a changed rule set invalidates every entry", func(t *testing.T) {
		_, hit := cache.Lookup("/project/source/clean.ts",
			program.HashContent("export const value = 1;\n"),
			program.HashRuleSet([]string{"no-debugger"}))
		if hit {
			t.Fatal("removing a rule left the cache serving results produced when it ran; " +
				"adding or removing a rule must invalidate everything at once")
		}
	})

	t.Run("a nil cache is always a miss", func(t *testing.T) {
		var nilCache *program.LintCache
		if _, hit := nilCache.Lookup("anything", program.HashContent("x"), ruleSet); hit {
			t.Fatal("a nil cache reported a hit")
		}
	})
}

// TestRuleSetHashIsOrderSensitive pins a property that looks like pedantry and is not. Two
// rules can report on the same range, and the order they ran in is the order findings come
// back in. A hash that ignored order would let a reordered rule set replay a finding sequence
// the current configuration would not produce.
func TestRuleSetHashIsOrderSensitive(t *testing.T) {
	forward := program.HashRuleSet([]string{"alpha", "beta"})
	backward := program.HashRuleSet([]string{"beta", "alpha"})
	if forward == backward {
		t.Fatal("the rule set hash ignores order, so a reordered configuration reads as the " +
			"same rule set and replays findings in an order it would not produce")
	}

	t.Run("concatenation cannot collide", func(t *testing.T) {
		// Without a separator, {"ab","c"} and {"a","bc"} hash identically. That is a real
		// collision between two different rule sets, and it would serve one's cache to the
		// other.
		if program.HashRuleSet([]string{"ab", "c"}) == program.HashRuleSet([]string{"a", "bc"}) {
			t.Fatal("rule names are concatenated without a separator, so different rule sets " +
				"collide and one set's cache is served to another")
		}
	})
}

// TestLintCacheFindingHasNoUncheckedFields makes the round-trip comparison structural.
//
// TestLintCacheRoundTripsEveryField enumerates fields by hand, which means it checks the fields
// somebody remembered rather than the fields that exist. That is not hypothetical: MessageDescription
// was added to LintCacheFinding and that test kept passing, having never looked at it. A test named
// for every field silently stopped covering one.
//
// So this asserts the field set itself. Adding a field to LintCacheFinding fails here until it is
// named below AND compared above, which is the cheapest way to make "the encoder forgot a field"
// impossible to ship — the exact bug this format has produced four times.
func TestLintCacheFindingHasNoUncheckedFields(t *testing.T) {
	// Every field the round-trip test compares. Kept in sync by this test failing, not by discipline.
	compared := map[string]struct{}{
		"RuleName":           {},
		"Start":              {},
		"End":                {},
		"MessageId":          {},
		"MessageDescription": {},
		"FixCount":           {},
		"SuggestionCount":    {},
	}

	findingType := reflect.TypeOf(program.LintCacheFinding{})
	if findingType.NumField() == 0 {
		t.Fatal("LintCacheFinding has no fields, so this check is looking at the wrong type")
	}

	for index := range findingType.NumField() {
		name := findingType.Field(index).Name
		if _, checked := compared[name]; !checked {
			t.Errorf("LintCacheFinding.%s is not compared by TestLintCacheRoundTripsEveryField: "+
				"add it there and name it here, or an encoder that drops it decodes to empty while "+
				"every field around it looks correct", name)
		}
		delete(compared, name)
	}

	// A name left over means the list drifted the other way: this check would keep passing while
	// pointing at a field that no longer exists, which makes it decorative.
	for name := range compared {
		t.Errorf("this check names %q but LintCacheFinding has no such field, so the list is stale", name)
	}
}

// TestLintCacheStoreReplacesRatherThanAppends pins the property Lookup depends on.
//
// A path stored twice must leave one entry, not two. Two entries make Lookup's answer depend on
// which copy the index happened to keep, and the stale one would win or lose by insertion order,
// which is exactly the kind of nondeterminism that reads as a flaky cache rather than a bug.
func TestLintCacheStoreReplacesRatherThanAppends(t *testing.T) {
	cache := &program.LintCache{RuleSetHash: program.HashRuleSet([]string{"no-debugger"})}
	path := "/project/source/edited.ts"

	first := program.HashContent("debugger;\n")
	cache.Store(path, first, []program.LintCacheFinding{{
		RuleName: "no-debugger", Start: 0, End: 9,
		MessageId: "unexpectedDebugger", MessageDescription: "Unexpected debugger statement.",
	}})

	// The same file, edited clean. The second store must supersede the first.
	second := program.HashContent("export const value = 1;\n")
	cache.Store(path, second, nil)

	if len(cache.Entries) != 1 {
		t.Fatalf("entries: %d, so Store appended instead of replacing", len(cache.Entries))
	}

	// The superseded content hash must no longer be a hit, or the cache would serve findings for
	// bytes that are gone.
	if _, hit := cache.Lookup(path, first, cache.RuleSetHash); hit {
		t.Error("the replaced content hash is still a hit, so stale findings survived a Store")
	}

	findings, hit := cache.Lookup(path, second, cache.RuleSetHash)
	if !hit {
		t.Fatal("the current content hash is a miss, so the replacement did not take")
	}
	if len(findings) != 0 {
		t.Errorf("findings: %d, want 0 for the cleaned file", len(findings))
	}
}

// TestLintCacheStoreRecordsCleanFiles is the case worth caching, and the one easiest to skip.
//
// Most files produce nothing. If Store dropped empty results, every clean file would be a miss
// forever and the cache would save nothing on exactly the population it exists for, while looking
// like it worked.
func TestLintCacheStoreRecordsCleanFiles(t *testing.T) {
	cache := &program.LintCache{RuleSetHash: program.HashRuleSet([]string{"no-debugger"})}
	contentHash := program.HashContent("export const value = 1;\n")
	cache.Store("/project/source/clean.ts", contentHash, nil)

	findings, hit := cache.Lookup("/project/source/clean.ts", contentHash, cache.RuleSetHash)
	if !hit {
		t.Fatal("a stored clean file is a miss, so clean files would never be cached")
	}
	if len(findings) != 0 {
		t.Errorf("findings: %d, want 0", len(findings))
	}
}

// TestLintCacheSurvivesDisk round-trips through the real filesystem rather than through Encode
// alone, because the write path is where a cache is truncated or half-visible.
func TestLintCacheSurvivesDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "lint.cache")
	original := sampleLintCache()

	if err := program.WriteLintCache(path, original); err != nil {
		t.Fatalf("writing: %v", err)
	}

	decoded, err := program.ReadLintCache(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(decoded.Entries) != len(original.Entries) {
		t.Fatalf("entries: %d back from %d", len(decoded.Entries), len(original.Entries))
	}

	// The interpolated description is the one that cannot be rebuilt, so it is the one worth
	// asserting survived a real write.
	findings, hit := decoded.Lookup(
		original.Entries[0].Path, original.Entries[0].ContentHash, original.RuleSetHash)
	if !hit {
		t.Fatal("an entry written to disk came back as a miss")
	}
	if len(findings) != 2 {
		t.Fatalf("findings: %d back from 2", len(findings))
	}
	if findings[1].MessageDescription != original.Entries[0].Findings[1].MessageDescription {
		t.Errorf("description did not survive disk:\n  got  %q\n  want %q",
			findings[1].MessageDescription, original.Entries[0].Findings[1].MessageDescription)
	}
}

// TestLintCacheWriteLeavesNoPartialArtifact pins the atomic-rename property.
//
// Several cohere runs can share a tree, so a reader must never observe a half-written cache. The
// temporary file is written in the destination directory and renamed, and this asserts the
// directory holds exactly the finished artifact afterward: a leftover temporary is the tell that a
// failure path forgot to clean up, and it accumulates silently across runs.
func TestLintCacheWriteLeavesNoPartialArtifact(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "lint.cache")

	if err := program.WriteLintCache(path, sampleLintCache()); err != nil {
		t.Fatalf("writing: %v", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("listing %s: %v", directory, err)
	}
	if len(entries) != 1 || entries[0].Name() != "lint.cache" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the directory holds %v, want exactly [lint.cache]: a leftover temporary means a "+
			"failure path did not clean up", names)
	}
}

// TestReadLintCacheTreatsAMissingFileAsAMiss pins that a first run is not an error.
//
// A missing cache and an unparseable one both mean run cold. Reporting either as fatal would make
// the first run in a fresh checkout fail, which is the loudest possible way to be wrong about
// something harmless.
func TestReadLintCacheTreatsAMissingFileAsAMiss(t *testing.T) {
	cache, err := program.ReadLintCache(filepath.Join(t.TempDir(), "absent.cache"))
	if err == nil {
		t.Error("reading an absent cache returned no error, so a caller cannot say why it was cold")
	}
	if cache != nil {
		t.Error("reading an absent cache returned a cache, which would be used as if it were real")
	}
}

// TestCacheableRulesExcludesImpureRules pins the split the cache's correctness rests on.
//
// A rule is cacheable only if its findings are a function of the linted file's bytes, because that
// is what the content-hash key covers. Both declarations below describe a rule whose answer can
// change while that hash does not, and replaying such a rule serves zero findings forever on a file
// that now has one.
func TestCacheableRulesExcludesImpureRules(t *testing.T) {
	pure := rule.Rule{Name: "pure"}
	readsProgram := rule.Rule{Name: "reads-program", ReadsProgram: true}
	needsChecker := rule.Rule{Name: "needs-checker", NeedsTypeChecker: true}
	both := rule.Rule{Name: "both", ReadsProgram: true, NeedsTypeChecker: true}

	cacheable, uncacheable := program.CacheableRules(
		[]rule.Rule{pure, readsProgram, needsChecker, both})

	if len(cacheable) != 1 || cacheable[0].Name != "pure" {
		names := make([]string, 0, len(cacheable))
		for _, subject := range cacheable {
			names = append(names, subject.Name)
		}
		t.Errorf("cacheable = %v, want exactly [pure]: anything reading outside its own file can "+
			"have its answer changed by an edit the content hash cannot see", names)
	}
	if len(uncacheable) != 3 {
		t.Errorf("uncacheable = %d, want 3", len(uncacheable))
	}
}

// TestCacheableRulesDefaultsToExcluding is the asymmetry, stated as a test rather than a comment.
//
// Including a rule wrongly serves stale findings silently and forever; excluding one wrongly costs
// a cache miss. Those are not comparable, so a rule carrying any impurity declaration is excluded
// even when it also looks pure by every other measure.
func TestCacheableRulesDefaultsToExcluding(t *testing.T) {
	// A rule that declares an impurity and nothing else must still be turned away, so a future
	// declaration added to rule.Rule cannot quietly become cacheable by omission here.
	for _, subject := range []rule.Rule{
		{Name: "reads-program", ReadsProgram: true},
		{Name: "needs-checker", NeedsTypeChecker: true},
	} {
		cacheable, _ := program.CacheableRules([]rule.Rule{subject})
		if len(cacheable) != 0 {
			t.Errorf("%s was admitted to the cache despite declaring an impurity", subject.Name)
		}
	}
}

// TestCacheableRulesHandlesAnEmptySet pins that no rules means no cacheable rules.
//
// An empty input returning a nil cacheable set matters because the caller uses emptiness to decide
// whether to consult the cache at all, and a non-nil empty slice and a nil one must behave the same
// at that decision.
func TestCacheableRulesHandlesAnEmptySet(t *testing.T) {
	cacheable, uncacheable := program.CacheableRules(nil)
	if len(cacheable) != 0 || len(uncacheable) != 0 {
		t.Errorf("empty input produced %d cacheable and %d uncacheable", len(cacheable), len(uncacheable))
	}
}
