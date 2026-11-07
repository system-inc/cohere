package configuration

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// An ignorePatterns entry says why (#cxeq6jw): a glob, or an object with the glob, its reason, its kind and the
// check that still reads it. The object is read key by key, and inside our tiers, and in every set cohere carries,
// an entry with no reason is refused, as an off with none is.

func TestAnIgnoreEntryReadsAsAGlobOrAnObject(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"CohereSettings.json": `{"ignorePatterns": ["dist/**", {"pattern": "data/**", "reason": "  exports, kept as ` +
			`given  ", "kind": "input", "check": "types"}, {"pattern": "vendor/**", "kind": "thirdParty"}]}`,
	})
	settings := filepath.Join(directory, "CohereSettings.json")
	loaded := loadOrFail(test, settings)
	if want := []string{"dist/**", "data/**", "vendor/**"}; !reflect.DeepEqual(loaded.IgnorePatterns, want) {
		test.Errorf("IgnorePatterns are %v, want each entry's glob, in order: %v", loaded.IgnorePatterns, want)
	}
	want := []IgnoreEntry{
		{Pattern: "dist/**", File: settings},
		{
			Pattern: "data/**",
			Reason:  "exports, kept as given",
			Kind:    IgnoreKindInput,
			Check:   IgnoreCheckTypes,
			File:    settings,
		},
		{Pattern: "vendor/**", Kind: IgnoreKindThirdParty, File: settings},
	}
	if !reflect.DeepEqual(loaded.IgnoreEntries, want) {
		test.Errorf("IgnoreEntries are\n  %+v\nwant\n  %+v", loaded.IgnoreEntries, want)
	}
	entries, ignoreEntriesOfError := IgnoreEntriesOf(settings)
	if ignoreEntriesOfError != nil || !reflect.DeepEqual(entries, want) {
		test.Errorf("IgnoreEntriesOf = %+v (%v), want Load's", entries, ignoreEntriesOfError)
	}
	if patterns, ignorePatternsOfError := IgnorePatternsOf(
		settings,
	); ignorePatternsOfError != nil || !reflect.DeepEqual(patterns, loaded.IgnorePatterns) {
		test.Errorf("IgnorePatternsOf = %v (%v), want Load's %v",
			patterns, ignorePatternsOfError, loaded.IgnorePatterns)
	}
}

func TestAMalformedIgnoreEntryIsRefused(test *testing.T) {
	test.Parallel()
	cases := map[string]struct {
		entry string
		want  string
	}{
		"an unknown key": {
			entry: `{"pattern": "dist/**", "reason": "build output", "excludedFiles": ["dist/keep"]}`,
			want:  `declares "excludedFiles", which this loader does not read`,
		},
		"an unknown kind": {
			entry: `{"pattern": "dist/**", "reason": "build output", "kind": "vendored"}`,
			want:  `names the kind "vendored": a kind is one of "input", "generated", "thirdParty"`,
		},
		"no pattern": {
			entry: `{"reason": "build output"}`,
			want:  `names no "pattern"`,
		},
		"an empty pattern": {
			entry: `{"pattern": "", "reason": "build output"}`,
			want:  `names no "pattern"`,
		},
		"an empty reason": {
			entry: `{"pattern": "dist/**", "reason": "   "}`,
			want:  `the "ignorePatterns" entry for "dist/**" gives an empty "reason"`,
		},
		"an unknown check": {
			entry: `{"pattern": "dist/**", "reason": "build output", "check": "lint"}`,
			want:  `names the check "lint"`,
		},
		"neither a glob nor an object": {
			entry: `["dist/**"]`,
			want:  `an "ignorePatterns" entry is a glob, or an object with "pattern" and "reason"`,
		},
	}
	for name, check := range cases {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			directory := writeConfigs(test, map[string]string{
				"CohereSettings.json": `{"ignorePatterns": [` + check.entry + `]}`,
			})
			settings := filepath.Join(directory, "CohereSettings.json")
			refusedWith(test, settings, check.want)
			// Discovery and the format walk read the list without Load, and are refused alike.
			if _, ignorePatternsOfError := IgnorePatternsOf(settings); ignorePatternsOfError == nil ||
				!strings.Contains(ignorePatternsOfError.Error(), check.want) {
				test.Errorf("IgnorePatternsOf did not refuse it saying %q: %v", check.want, ignorePatternsOfError)
			}
		})
	}
}

// A set cohere carries is ours wherever it is read, so an ignore written in one says why, as an off does there.
// Not parallel: it swaps the package's setFiles for stand-in sets through withSetFiles, which every test that
// reads a set would see.
func TestASetsIgnoreGivesAReason(test *testing.T) {
	withSetFiles(test, map[string]string{
		"bare":     `{"ignorePatterns": ["dist/**"]}`,
		"reasoned": `{"ignorePatterns": [{"pattern": "dist/**", "reason": "Build output.", "kind": "generated"}]}`,
	})
	want := `lint config cohere:bare ignores "dist/**" and gives no reason: an ignore that says nothing reads as ` +
		`an allowance`
	directory := writeConfigs(test, map[string]string{
		"bare/CohereSettings.json":     `{"extends": "cohere:bare"}`,
		"reasoned/CohereSettings.json": `{"extends": "cohere:reasoned"}`,
	})
	bare := filepath.Join(directory, "bare", "CohereSettings.json")
	refusedWith(test, bare, want)
	if _, ignorePatternsOfError := IgnorePatternsOf(bare); ignorePatternsOfError == nil ||
		!strings.Contains(ignorePatternsOfError.Error(), want) {
		test.Errorf("IgnorePatternsOf did not refuse it saying %q: %v", want, ignorePatternsOfError)
	}
	loaded := loadOrFail(test, filepath.Join(directory, "reasoned", "CohereSettings.json"))
	if len(loaded.IgnoreEntries) != 1 || loaded.IgnoreEntries[0].Reason != "Build output." {
		test.Errorf("the reasoned set's entry loaded as %+v", loaded.IgnoreEntries)
	}
}

// The real tier set, so the chain is one of ours exactly as a repository's is, and the set's own entries, which
// load first, are held to the same rule.
func TestInOurTiersAnIgnoreGivesAReason(test *testing.T) {
	test.Parallel()
	want := `ignores "dist/**" and gives no reason: an ignore that says nothing reads as an allowance`
	for name, entry := range map[string]string{
		"a glob alone":             `"dist/**"`,
		"an object with no reason": `{"pattern": "dist/**", "kind": "generated"}`,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			directory := writeConfigs(test, map[string]string{
				"CohereSettings.json": `{"extends": "cohere:system-inc/base", "ignorePatterns": [` + entry + `]}`,
			})
			settings := filepath.Join(directory, "CohereSettings.json")
			refusedWith(test, settings, want)
			if _, ignorePatternsOfError := IgnorePatternsOf(settings); ignorePatternsOfError == nil ||
				!strings.Contains(ignorePatternsOfError.Error(), want) {
				test.Errorf("IgnorePatternsOf did not refuse it saying %q: %v", want, ignorePatternsOfError)
			}
		})
	}

	directory := writeConfigs(test, map[string]string{
		"CohereSettings.json": `{"extends": "cohere:system-inc/base", "ignorePatterns": [{"pattern": "dist/**", ` +
			`"reason": "Build output, written from sources that are checked.", "kind": "generated"}]}`,
	})
	loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))
	own := loaded.IgnoreEntries[len(loaded.IgnoreEntries)-1]
	if own.Pattern != "dist/**" || own.Kind != IgnoreKindGenerated || own.Reason == "" {
		test.Errorf("the project's own entry loaded as %+v", own)
	}
	for _, entry := range loaded.IgnoreEntries {
		if entry.Reason == "" {
			test.Errorf("%s ignores %q with no reason", entry.File, entry.Pattern)
		}
	}
}

// Outside our tiers a glob alone is the project's own business, as an off is (#bfxz13m). The sets it extends are
// still cohere's, and every entry they carry says why.
func TestOutsideOurTiersAGlobAloneLoads(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"CohereSettings.json": `{"extends": ["cohere:react", "cohere:next"], "ignorePatterns": ["dist/**"]}`,
	})
	loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))
	if last := loaded.IgnoreEntries[len(loaded.IgnoreEntries)-1]; last.Pattern != "dist/**" || last.Reason != "" {
		test.Errorf("the project's own glob loaded as %+v", last)
	}
	fromSets := 0
	for _, entry := range loaded.IgnoreEntries {
		if !IsSet(entry.File) {
			continue
		}
		fromSets++
		if entry.Reason == "" || entry.Kind == "" {
			test.Errorf("%s ignores %q with no reason or kind", entry.File, entry.Pattern)
		}
	}
	if fromSets == 0 {
		test.Error("cohere:next's ignores did not load")
	}
}

// A glob's literal prefix is the path every match lies at or below, which a count may look under when a walk
// skipped it.
func TestALiteralPrefixStopsAtTheFirstGlobSyntax(test *testing.T) {
	test.Parallel()
	for pattern, want := range map[string]string{
		"internal/load/testdata/0.1/refuse/**":           "internal/load/testdata/0.1/refuse",
		"internal/lower/testdata/input-spread/*_array.a": "internal/lower/testdata/input-spread",
		"base.js":            "base.js",
		"**/hooks/**":        "",
		"{a,b}/generated/**": "",
		"modules/?/data/**":  "modules",
	} {
		if got := LiteralPrefix(pattern); got != want {
			test.Errorf("LiteralPrefix(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// A walk counts every path it considers against each pattern, and a directory only by a pattern that takes it
// whole, as the format walk prunes one.
func TestAnIgnoreTallyCountsEachPatternsPaths(test *testing.T) {
	test.Parallel()
	tally := NewIgnoreTally([]string{"dist/**", "**/*.code.js", "missing/**"})
	for _, considered := range []struct {
		path      string
		directory bool
	}{
		{"dist", true},
		{"dist/index.js", false},
		{"source/worker.code.js", false},
		{"source/odd.code.js", true},
		{"source/index.ts", false},
	} {
		tally.Count(considered.path, considered.directory)
	}
	if want := []int{2, 1, 0}; !reflect.DeepEqual(tally.Matched, want) {
		test.Errorf("matched %v, want %v", tally.Matched, want)
	}
}
