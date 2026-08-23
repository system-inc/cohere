package program_test

import (
	"errors"
	"testing"

	"github.com/system-inc/verify/internal/program"
)

func sampleLintCache() *program.LintCache {
	return &program.LintCache{
		RuleSetHash: program.HashRuleSet([]string{"no-debugger", "no-empty"}),
		Entries: []program.LintCacheEntry{
			{
				Path:        "/project/source/dirty.ts",
				ContentHash: program.HashContent("debugger;\n"),
				Findings: []program.LintCacheFinding{
					{RuleName: "no-debugger", Start: 0, End: 9, MessageId: "unexpectedDebugger", FixCount: 1},
					{RuleName: "no-empty", Start: 10, End: 12, MessageId: "emptyBlock", SuggestionCount: 2},
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
