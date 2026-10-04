package main

import (
	"strings"
	"testing"
	"time"
)

// cleanRecords is a default run in which every phase ran and lint reused the fix phase's walk.
func cleanRecords() []phaseRecord {
	return []phaseRecord{
		{Name: phaseFix, Outcome: outcomeRan, Elapsed: 400 * time.Millisecond},
		{Name: phaseTypes, Outcome: outcomeRan, Elapsed: 600 * time.Millisecond},
		{Name: phaseLint, Outcome: outcomeRan, Elapsed: 1100 * time.Millisecond},
		{Name: phaseUnused, Outcome: outcomeSkipped, Detail: "not requested"},
	}
}

func cleanFacts() footerFacts {
	return footerFacts{Total: 2400 * time.Millisecond, Files: 3926, Nodes: 2_412_345}
}

func TestFooterGolden(t *testing.T) {
	cases := []struct {
		name       string
		records    []phaseRecord
		formatting time.Duration
		facts      footerFacts
		want       string
	}{
		{
			name:       "pass",
			records:    cleanRecords(),
			formatting: 300 * time.Millisecond,
			facts:      cleanFacts(),
			want:       "✓ 💎 2.4s → 🪄 0.4s • 💅 0.3s • 🔷 0.6s • 👑 1.1s · 3.9K files · 2.4M nodes",
		},
		{
			name:       "fail",
			records:    cleanRecords(),
			formatting: 300 * time.Millisecond,
			facts: func() footerFacts {
				facts := cleanFacts()
				facts.TypeErrors, facts.Findings = 1, 1
				return facts
			}(),
			want: "✗ ☠️ 2.4s → 1 type error · 1 finding · 3.9K files · 2.4M nodes",
		},
		{
			name:    "replay",
			records: nil,
			facts:   footerFacts{Total: 50 * time.Millisecond, Replayed: true, Files: 3926},
			want:    "✓ 💎 0.05s ↺ replayed · 3.9K files",
		},
		{
			name: "lint reused the fix walk",
			records: []phaseRecord{
				{Name: phaseFix, Outcome: outcomeRan, Elapsed: 1500 * time.Millisecond},
				{Name: phaseTypes, Outcome: outcomeRan, Elapsed: 600 * time.Millisecond},
				{Name: phaseLint, Outcome: outcomeReused, Detail: "the fix phase's walk"},
			},
			facts: cleanFacts(),
			want:  "✓ 💎 2.4s → 🪄 1.5s • 🔷 0.6s • 👑 in 🪄 · 3.9K files · 2.4M nodes",
		},
		{
			name: "types bailed, so lint did not run",
			records: []phaseRecord{
				{Name: phaseFix, Outcome: outcomeRan, Elapsed: 400 * time.Millisecond},
				{Name: phaseTypes, Outcome: outcomeRan, Elapsed: 600 * time.Millisecond},
				{Name: phaseLint, Outcome: outcomeNotReached, Detail: "types bailed: 2 type errors"},
			},
			facts: func() footerFacts {
				facts := cleanFacts()
				facts.TypeErrors = 2
				return facts
			}(),
			want: "✗ ☠️ 2.4s → 2 type errors · 3.9K files · 2.4M nodes · ⚠ 👑 lint did not run",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := footer(testCase.records, testCase.formatting, testCase.facts); got != testCase.want {
				t.Errorf("footer:\n got  %s\n want %s", got, testCase.want)
			}
		})
	}
}

// uncheckedConditions are every way a green run can fall short of checking everything. Each is planted
// on an otherwise clean run, and the footer must say so with its marker.
var uncheckedConditions = []struct {
	name   string
	plant  func(records []phaseRecord, facts *footerFacts) []phaseRecord
	marker string
}{
	{
		name:   "a file crashed",
		plant:  func(records []phaseRecord, facts *footerFacts) []phaseRecord { facts.CrashedFiles = 1; return records },
		marker: "⚠ 1 file crashed",
	},
	{
		name: "a rule skipped every file",
		plant: func(records []phaseRecord, facts *footerFacts) []phaseRecord {
			facts.RulesSkippingEverything = 2
			return records
		},
		marker: "⚠ 2 rules skipped every file",
	},
	{
		name: "a phase could not run",
		plant: func(records []phaseRecord, facts *footerFacts) []phaseRecord {
			records[2] = phaseRecord{Name: phaseLint, Outcome: outcomeNotReached, Detail: "fix bailed"}
			return records
		},
		marker: "⚠ 👑 lint did not run",
	},
	{
		name: "a reporting phase was skipped",
		plant: func(records []phaseRecord, facts *footerFacts) []phaseRecord {
			records[1] = phaseRecord{Name: phaseTypes, Outcome: outcomeSkipped, Detail: "not requested"}
			return records
		},
		marker: "⚠ 🔷 types not checked",
	},
	{
		name: "formatting was not checked",
		plant: func(records []phaseRecord, facts *footerFacts) []phaseRecord {
			facts.FormattingNotChecked = true
			return records
		},
		marker: "💅 formatting not checked",
	},
	{
		name: "the project had nothing to check",
		plant: func(records []phaseRecord, facts *footerFacts) []phaseRecord {
			facts.NothingToCheck, facts.Files, facts.Nodes = true, 0, 0
			return records
		},
		marker: "⚠ no files to check",
	},
	{
		name: "the run was narrowed to some files",
		plant: func(records []phaseRecord, facts *footerFacts) []phaseRecord {
			facts.FilesInScope = 12
			return records
		},
		marker: "⚠ only 12 of the files",
	},
	{
		name: "a file could not be read",
		plant: func(records []phaseRecord, facts *footerFacts) []phaseRecord {
			facts.Unread = "1 file the compiler left no record for"
			return records
		},
		marker: "⚠ 1 file the compiler left no record for",
	},
	{
		name: "the binary was built from a modified tree",
		plant: func(records []phaseRecord, facts *footerFacts) []phaseRecord {
			facts.ModifiedBuild = true
			return records
		},
		marker: "⚠ built from a modified tree",
	},
}

// TestFooterSaysWhatWasNotChecked plants each condition on a clean, green run and requires its marker,
// and then requires the check itself to catch a footer that drops the marker. Without the second half a
// check that matched anything would pass, and a footer that went quiet about a gap would go unnoticed.
func TestFooterSaysWhatWasNotChecked(t *testing.T) {
	clean := footer(cleanRecords(), 300*time.Millisecond, cleanFacts())
	if strings.Contains(clean, "⚠") || strings.Contains(clean, "not checked") {
		t.Fatalf("a clean run's footer carries a marker it has no cause for: %s", clean)
	}
	for _, condition := range uncheckedConditions {
		t.Run(condition.name, func(t *testing.T) {
			facts := cleanFacts()
			records := condition.plant(cleanRecords(), &facts)
			if facts.failed() {
				t.Fatalf("the planted condition made the run fail, so it no longer tests a green footer")
			}
			got := footer(records, 300*time.Millisecond, facts)
			if !strings.HasPrefix(got, "✓ 💎") {
				t.Errorf("planting %s turned the verdict: %s", condition.name, got)
			}
			if err := carriesMarker(got, condition.marker); err != nil {
				t.Error(err)
			}

			// The mutant: the same run with the marker dropped from its footer. The check must fail on it.
			mutant := strings.Replace(got, " · "+condition.marker, "", 1)
			if mutant == got {
				t.Fatalf("the mutant could not drop %q from %s", condition.marker, got)
			}
			if carriesMarker(mutant, condition.marker) == nil {
				t.Errorf("the check passed a footer with %q dropped: %s", condition.marker, mutant)
			}
		})
	}
}

func carriesMarker(footerLine, marker string) error {
	if !strings.Contains(footerLine, marker) {
		return &missingMarker{footer: footerLine, marker: marker}
	}
	return nil
}

type missingMarker struct{ footer, marker string }

func (m *missingMarker) Error() string {
	return "the footer does not say " + m.marker + ": " + m.footer
}

func TestFooterAbbreviates(t *testing.T) {
	for count, want := range map[int]string{
		0: "0", 999: "999", 1000: "1K", 1234: "1.2K", 3926: "3.9K", 99_999: "100K", 120_000: "120K",
		999_999: "1M", 2_412_345: "2.4M", 340_000_000: "340M",
	} {
		if got := abbreviated(count); got != want {
			t.Errorf("abbreviated(%d) = %s, want %s", count, got, want)
		}
	}
	for duration, want := range map[time.Duration]string{
		50 * time.Millisecond: "0.05s", 400 * time.Millisecond: "0.4s", 2400 * time.Millisecond: "2.4s",
		12 * time.Second: "12s",
	} {
		if got := footerSeconds(duration); got != want {
			t.Errorf("footerSeconds(%s) = %s, want %s", duration, got, want)
		}
	}
}

func TestFooterLeadsWithItsProjectsLabel(t *testing.T) {
	facts := cleanFacts()
	facts.Label = "projects/www"
	want := "projects/www  ✓ 💎 2.4s → 🪄 0.4s • 🔷 0.6s • 👑 1.1s · 3.9K files · 2.4M nodes"
	if got := footer(cleanRecords(), 0, facts); got != want {
		t.Errorf("footer:\n got  %s\n want %s", got, want)
	}
}

func TestOverallFooterGolden(t *testing.T) {
	engines := map[string]int{"TypeScript": 2, "Swift": 1}
	cases := []struct {
		name  string
		facts overallFacts
		want  string
	}{
		{
			name:  "every project green",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: engines},
			want:  "✓ 💎 8.1s · 3 projects (1 Swift, 2 TypeScript)",
		},
		{
			name:  "one engine",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: map[string]int{"TypeScript": 2}},
			want:  "✓ 💎 8.1s · 2 projects",
		},
		{
			name:  "a project failed",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: engines, Failed: []string{"www (exit 1)"}},
			want:  "✗ ☠️ 8.1s · 3 projects (1 Swift, 2 TypeScript) · ☠️ www (exit 1)",
		},
		{
			// A project that did not finish checked nothing, so the run cannot be green.
			name:  "a project did not finish",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: engines, Unfinished: 1},
			want:  "✗ ☠️ 8.1s · 3 projects (1 Swift, 2 TypeScript) · ⚠ 1 project did not finish",
		},
		{
			// Green, and still saying what it left unchecked.
			name:  "a nested repository was not checked",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: engines, NestedNotEntered: 2},
			want:  "✓ 💎 8.1s · 3 projects (1 Swift, 2 TypeScript) · ⚠ 2 nested repositories not checked",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := overallFooter(testCase.facts); got != testCase.want {
				t.Errorf("overallFooter:\n got  %s\n want %s", got, testCase.want)
			}
		})
	}
}
