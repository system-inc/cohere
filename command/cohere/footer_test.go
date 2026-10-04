package main

import (
	"strings"
	"testing"
	"time"
)

// plain is the style with no color, as on a pipe or under NO_COLOR, and colored the style on a terminal.
var (
	plain   = textStyle{}
	colored = textStyle{color: true}
)

// cleanSummary is a green cold run: every phase ran, formatting included, every file cohered.
func cleanSummary() runSummary {
	return runSummary{
		Total: 2400 * time.Millisecond,
		Phases: []phaseRecord{
			{Name: phaseFix, Outcome: outcomeRan, Elapsed: 400 * time.Millisecond},
			{Name: phaseTypes, Outcome: outcomeRan, Elapsed: 600 * time.Millisecond},
			{Name: phaseLint, Outcome: outcomeRan, Elapsed: 1100 * time.Millisecond},
			{Name: phaseUnused, Outcome: outcomeSkipped, Detail: "not requested"},
		},
		Formatting:   300 * time.Millisecond,
		FilesCohered: 3926,
		Nodes:        2_412_345,
	}
}

// warmSummary is a green warm run: three files cohered, the rest from the cache.
func warmSummary(summary *runSummary) {
	summary.Total = 700 * time.Millisecond
	summary.Phases = []phaseRecord{
		{Name: phaseFix, Outcome: outcomeRan, Elapsed: 100 * time.Millisecond},
		{Name: phaseTypes, Outcome: outcomeRan, Elapsed: 400 * time.Millisecond},
		{Name: phaseLint, Outcome: outcomeRan, Elapsed: 200 * time.Millisecond},
	}
	summary.Formatting = 20 * time.Millisecond
	summary.FilesCohered, summary.FilesCached = 3, 3923
}

// The golden lines are Kirk's, from the visual spec of 2026-10-04.
func TestFooterGolden(t *testing.T) {
	cases := []struct {
		name    string
		summary func(summary *runSummary)
		want    string
	}{
		{
			name:    "cold",
			summary: func(summary *runSummary) {},
			want:    "✓ 💎 2.4s • 3.9K cohered (🪄 0.4s • 💅 0.3s • 🔷 0.6s • 👑 1.1s) • 2.4M nodes",
		},
		{
			name:    "warm",
			summary: warmSummary,
			want:    "✓ 💎 0.7s • 3 cohered (🪄 0.1s • 💅 0.02s • 🔷 0.4s • 👑 0.2s) 3.9K cached • 2.4M nodes",
		},
		{
			name: "replay",
			summary: func(summary *runSummary) {
				*summary = runSummary{Total: 50 * time.Millisecond, Cache: cacheUse{Replayed: true}, FilesCached: 3926}
			},
			want: "✓ 💎 0.05s • replayed • 3.9K cached",
		},
		{
			name: "fail",
			summary: func(summary *runSummary) {
				warmSummary(summary)
				summary.Total = 800 * time.Millisecond
				summary.TypeErrors, summary.Findings = 1, 2
			},
			want: "✗ ☠️ 0.8s • 3 cohered (🪄 0.1s • 💅 0.02s • 🔷 0.4s • 👑 0.2s) 3.9K cached → 1 type error • 2 findings",
		},
		{
			name:    "a gap, appended to a green run",
			summary: func(summary *runSummary) { summary.Gaps.CrashedFiles = 1 },
			want:    "✓ 💎 2.4s • 3.9K cohered (🪄 0.4s • 💅 0.3s • 🔷 0.6s • 👑 1.1s) • 2.4M nodes • ⚠ 1 file crashed",
		},
		{
			name: "a phase that did not run is left out of the parentheses",
			summary: func(summary *runSummary) {
				summary.Formatting = 0
				summary.Phases = summary.Phases[:2]
			},
			want: "✓ 💎 2.4s • 3.9K cohered (🪄 0.4s • 🔷 0.6s) • 2.4M nodes",
		},
		{
			name: "lint reused the fix walk",
			summary: func(summary *runSummary) {
				summary.Formatting = 0
				summary.Phases[2] = phaseRecord{Name: phaseLint, Outcome: outcomeReused, Detail: "the fix phase's walk"}
			},
			want: "✓ 💎 2.4s • 3.9K cohered (🪄 0.4s • 🔷 0.6s • 👑 in 🪄) • 2.4M nodes",
		},
		{
			name: "types bailed, so lint did not run",
			summary: func(summary *runSummary) {
				summary.TypeErrors = 2
				summary.Formatting = 0
				summary.Phases[2] = phaseRecord{Name: phaseLint, Outcome: outcomeNotReached, Detail: "types bailed: 2 type errors"}
			},
			want: "✗ ☠️ 2.4s • 3.9K cohered (🪄 0.4s • 🔷 0.6s) → 2 type errors • ⚠ 👑 lint did not run",
		},
		{
			name:    "labelled, in a repository with several projects",
			summary: func(summary *runSummary) { summary.Label = "projects/www" },
			want:    "projects/www  ✓ 💎 2.4s • 3.9K cohered (🪄 0.4s • 💅 0.3s • 🔷 0.6s • 👑 1.1s) • 2.4M nodes",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			summary := cleanSummary()
			testCase.summary(&summary)
			if got := footer(summary, plain); got != testCase.want {
				t.Errorf("footer:\n got  %s\n want %s", got, testCase.want)
			}
		})
	}
}

// TestFooterColor pins the color on a terminal: the verdict and total bold, the phases and the counts
// after them dim, what was found red. Without color there is not one escape code.
func TestFooterColor(t *testing.T) {
	summary := cleanSummary()
	warmSummary(&summary)
	want := "\x1b[1m✓ 💎 0.7s\x1b[22m • 3 cohered \x1b[2m(🪄 0.1s • 💅 0.02s • 🔷 0.4s • 👑 0.2s)\x1b[22m \x1b[2m3.9K cached\x1b[22m • \x1b[2m2.4M nodes\x1b[22m"
	if got := footer(summary, colored); got != want {
		t.Errorf("colored footer:\n got  %q\n want %q", got, want)
	}

	summary.TypeErrors = 1
	want = "\x1b[1m✗ ☠️ 0.7s\x1b[22m • 3 cohered \x1b[2m(🪄 0.1s • 💅 0.02s • 🔷 0.4s • 👑 0.2s)\x1b[22m \x1b[2m3.9K cached\x1b[22m → \x1b[31m1 type error\x1b[39m"
	if got := footer(summary, colored); got != want {
		t.Errorf("colored failing footer:\n got  %q\n want %q", got, want)
	}
	if got := footer(summary, plain); strings.Contains(got, "\x1b") {
		t.Errorf("a footer without color carries an escape code: %q", got)
	}
}

// uncheckedConditions are every way a green run can fall short of checking everything. Each is planted
// on an otherwise clean run, and the footer must say so with its marker.
var uncheckedConditions = []struct {
	name   string
	plant  func(summary *runSummary)
	marker string
}{
	{name: "a file crashed", plant: func(summary *runSummary) { summary.Gaps.CrashedFiles = 1 }, marker: "⚠ 1 file crashed"},
	{
		name:   "a rule skipped every file",
		plant:  func(summary *runSummary) { summary.Gaps.RulesSkippingEverything = 2 },
		marker: "⚠ 2 rules skipped every file",
	},
	{
		name: "a phase could not run",
		plant: func(summary *runSummary) {
			summary.Phases[2] = phaseRecord{Name: phaseLint, Outcome: outcomeNotReached, Detail: "fix bailed"}
		},
		marker: "⚠ 👑 lint did not run",
	},
	{
		name: "a reporting phase was skipped",
		plant: func(summary *runSummary) {
			summary.Phases[1] = phaseRecord{Name: phaseTypes, Outcome: outcomeSkipped, Detail: "not requested"}
		},
		marker: "⚠ 🔷 types not checked",
	},
	{
		name:   "formatting was not checked",
		plant:  func(summary *runSummary) { summary.Gaps.FormattingNotChecked, summary.Formatting = true, 0 },
		marker: "💅 formatting not checked",
	},
	{
		name: "the project had nothing to check",
		plant: func(summary *runSummary) {
			summary.Gaps.NothingToCheck, summary.FilesCohered, summary.Nodes = true, 0, 0
		},
		marker: "⚠ no files to check",
	},
	{
		name:   "the run was narrowed to some files",
		plant:  func(summary *runSummary) { summary.Gaps.FilesInScope = 12 },
		marker: "⚠ only 12 of the files",
	},
	{
		name:   "a file could not be read",
		plant:  func(summary *runSummary) { summary.Gaps.Unread = "1 file the compiler left no record for" },
		marker: "⚠ 1 file the compiler left no record for",
	},
	{
		name:   "the binary was built from a modified tree",
		plant:  func(summary *runSummary) { summary.Gaps.ModifiedBuild = true },
		marker: "⚠ built from a modified tree",
	},
}

// TestFooterSaysWhatWasNotChecked plants each condition on a clean, green run and requires its marker,
// and then requires the check itself to catch a footer that drops the marker. Without the second half a
// check that matched anything would pass, and a footer that went quiet about a gap would go unnoticed.
func TestFooterSaysWhatWasNotChecked(t *testing.T) {
	clean := footer(cleanSummary(), plain)
	if strings.Contains(clean, "⚠") || strings.Contains(clean, "not checked") {
		t.Fatalf("a clean run's footer carries a marker it has no cause for: %s", clean)
	}
	for _, condition := range uncheckedConditions {
		t.Run(condition.name, func(t *testing.T) {
			summary := cleanSummary()
			condition.plant(&summary)
			if summary.failed() {
				t.Fatalf("the planted condition made the run fail, so it no longer tests a green footer")
			}
			got := footer(summary, plain)
			if !strings.HasPrefix(got, "✓ 💎") {
				t.Errorf("planting %s turned the verdict: %s", condition.name, got)
			}
			if err := carriesMarker(got, condition.marker); err != nil {
				t.Error(err)
			}

			// The mutant: the same run with the marker dropped from its footer. The check must fail on it.
			mutant := strings.Replace(got, " • "+condition.marker, "", 1)
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
			want:  "✓ 💎 8.1s • 3 projects: 1 Swift, 2 TypeScript",
		},
		{
			name:  "one engine",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: map[string]int{"TypeScript": 2}},
			want:  "✓ 💎 8.1s • 2 projects",
		},
		{
			name:  "a project failed",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: engines, Failed: []string{"www (exit 1)"}},
			want:  "✗ ☠️ 8.1s • 3 projects: 1 Swift, 2 TypeScript → ☠️ www (exit 1)",
		},
		{
			// A project that did not finish checked nothing, so the run cannot be green.
			name:  "a project did not finish",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: engines, Unfinished: 1},
			want:  "✗ ☠️ 8.1s • 3 projects: 1 Swift, 2 TypeScript • ⚠ 1 project did not finish",
		},
		{
			// Green, and still saying what it left unchecked.
			name:  "a nested repository was not checked",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: engines, NestedNotEntered: 2},
			want:  "✓ 💎 8.1s • 3 projects: 1 Swift, 2 TypeScript • ⚠ 2 nested repositories not checked",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := overallFooter(testCase.facts, plain); got != testCase.want {
				t.Errorf("overallFooter:\n got  %s\n want %s", got, testCase.want)
			}
		})
	}
}
