package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// plain is the style with no color, as on a pipe or under NO_COLOR, and colored the style on a terminal.
var (
	plain   = textStyle{}
	colored = textStyle{color: true}
)

// cleanSummary is a green cold run: every phase ran, formatting included, every file checked fresh. The
// fix phase's 0.7s holds formatting's 0.3s, as a run's does.
func cleanSummary() runSummary {
	return runSummary{
		Total: 2400 * time.Millisecond,
		Graph: 900 * time.Millisecond,
		Phases: []phaseRecord{
			{Name: phaseFix, Outcome: outcomeRan, Elapsed: 700 * time.Millisecond},
			{Name: phaseTypes, Outcome: outcomeRan, Elapsed: 600 * time.Millisecond},
			{Name: phaseLint, Outcome: outcomeRan, Elapsed: 1100 * time.Millisecond},
			{Name: phaseUnused, Outcome: outcomeSkipped, Detail: "not requested"},
		},
		Formatting:   300 * time.Millisecond,
		Rules:        480,
		FilesInScope: 3926,
		FilesChecked: 3926,
		Nodes:        2_412_345,
	}
}

// warmSummary is a green warm run: three files edited and checked, the rest from the cache.
//
// Mutates: summary a case's summary func turns the clean summary it is handed into the run it names
func warmSummary(summary *runSummary) {
	summary.Total = 700 * time.Millisecond
	summary.Graph = 200 * time.Millisecond
	summary.Phases = []phaseRecord{
		{Name: phaseFix, Outcome: outcomeRan, Elapsed: 120 * time.Millisecond},
		{Name: phaseTypes, Outcome: outcomeRan, Elapsed: 400 * time.Millisecond},
		{Name: phaseLint, Outcome: outcomeRan, Elapsed: 200 * time.Millisecond},
	}
	summary.Formatting = 20 * time.Millisecond
	summary.FilesChecked, summary.FilesCached = 3, 3923
	summary.Nodes = 4_100
}

// replaySummary is a run replayed whole from the run cache.
//
// Mutates: summary a case's summary func turns the clean summary it is handed into the run it names
func replaySummary(summary *runSummary) {
	*summary = runSummary{
		Total:        50 * time.Millisecond,
		Cache:        cacheUse{Replayed: true},
		Rules:        480,
		FilesInScope: 3926,
		FilesCached:  3926,
	}
}

// The golden lines are Kirk's, from his final footer of 2026-10-04 12:55.
func TestFooterGolden(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name    string
		summary func(summary *runSummary)
		options footerOptions
		want    string
	}{
		{
			name:    "cold: everything checked fresh",
			summary: func(summary *runSummary) { /* The clean summary is this case as it stands. */ },
			want:    "✓ 💎 2.4s (480 rules • 3,926 checked)",
		},
		{
			name:    "three files edited, all clean",
			summary: warmSummary,
			want:    "✓ 💎 0.7s (480 rules • 3 checked • 3,923 cached)",
		},
		{
			name: "two got fixed or formatted",
			summary: func(summary *runSummary) {
				warmSummary(summary)
				summary.Changed = []changedFile{{Path: "a.ts", Fixed: true}, {Path: "b.ts", Formatted: true}}
			},
			want: "✓ 💎 0.7s • 2 cohered (480 rules • 3 checked • 3,923 cached)",
		},
		{
			name:    "nothing changed: a replay",
			summary: replaySummary,
			want:    "✓ 💎 0.05s (480 rules • 3,926 cached)",
		},
		{
			name: "fail",
			summary: func(summary *runSummary) {
				warmSummary(summary)
				summary.Total = 800 * time.Millisecond
				summary.TypeErrors, summary.Findings = 1, 2
			},
			want: "✗ ☠️ 0.8s • 1 type error • 2 findings (480 rules • 3 checked • 3,923 cached)",
		},
		{
			name: "fail, with a file cohered too",
			summary: func(summary *runSummary) {
				warmSummary(summary)
				summary.Findings = 1
				summary.Changed = []changedFile{{Path: "a.ts", Formatted: true}}
			},
			want: "✗ ☠️ 0.7s • 1 finding • 1 cohered (480 rules • 3 checked • 3,923 cached)",
		},
		{
			name:    "a gap, appended to a green run",
			summary: func(summary *runSummary) { summary.Gaps.FormattingNotChecked = true },
			want:    "✓ 💎 2.4s (480 rules • 3,926 checked) • 💅 formatting not checked",
		},
		{
			// A crash is a gap the run cannot pass with: a verdict is missing, and it is cohere's bug (#v1ah2qq).
			name:    "a crash, which fails the run",
			summary: func(summary *runSummary) { summary.Gaps.CrashedFiles = 1 },
			want:    "✗ ☠️ 2.4s (480 rules • 3,926 checked) • ⚠ 1 file crashed",
		},
		{
			name:    "cold, with --phases",
			summary: func(summary *runSummary) { /* The clean summary is this case as it stands. */ },
			options: footerOptions{Phases: true},
			want:    "✓ 💎 2.4s (🕸 0.9s • 🪄 0.4s • 💅 0.3s • 🔷 0.6s • 👑 1.1s • 480 rules • 3,926 checked)",
		},
		{
			name:    "warm, with --phases",
			summary: warmSummary,
			options: footerOptions{Phases: true},
			want:    "✓ 💎 0.7s (🕸 0.2s • 🪄 0.1s • 💅 0.02s • 🔷 0.4s • 👑 0.2s • 480 rules • 3 checked • 3,923 cached)",
		},
		{
			// A replay ran no phase, so there is no breakdown to show.
			name:    "replay, with --phases",
			summary: replaySummary,
			options: footerOptions{Phases: true},
			want:    "✓ 💎 0.05s (480 rules • 3,926 cached)",
		},
		{
			name: "with --phases, a phase that did not run is left out and unused shows only when it ran",
			summary: func(summary *runSummary) {
				summary.Formatting = 0
				summary.Phases[1] = phaseRecord{Name: phaseTypes, Outcome: outcomeSkipped, Detail: "not requested"}
				summary.Phases[3] = phaseRecord{Name: phaseUnused, Outcome: outcomeRan, Elapsed: 300 * time.Millisecond}
			},
			options: footerOptions{Phases: true},
			want:    "✓ 💎 2.4s (🕸 0.9s • 🪄 0.7s • 👑 1.1s • 🧹 0.3s • 480 rules • 3,926 checked) • ⚠ 🔷 types not checked",
		},
		{
			name: "with --phases, lint reused the fix walk",
			summary: func(summary *runSummary) {
				summary.Formatting = 0
				summary.Phases[2] = phaseRecord{Name: phaseLint, Outcome: outcomeReused, Detail: "the fix phase's walk"}
			},
			options: footerOptions{Phases: true},
			want:    "✓ 💎 2.4s (🕸 0.9s • 🪄 0.7s • 🔷 0.6s • 👑 in 🪄 • 480 rules • 3,926 checked)",
		},
		{
			name:    "warm, with --verbose: the nodes walked",
			summary: warmSummary,
			options: footerOptions{Verbose: true},
			want:    "✓ 💎 0.7s (480 rules • 3 checked • 3,923 cached • 4,100 nodes)",
		},
		{
			name:    "replay, with --verbose",
			summary: replaySummary,
			options: footerOptions{Verbose: true},
			want:    "✓ 💎 0.05s • replayed (480 rules • 3,926 cached)",
		},
		{
			name: "types bailed, so lint did not run",
			summary: func(summary *runSummary) {
				summary.TypeErrors = 2
				summary.Phases[2] = phaseRecord{
					Name:    phaseLint,
					Outcome: outcomeNotReached,
					Detail:  "types bailed: 2 type errors",
				}
			},
			want: "✗ ☠️ 2.4s • 2 type errors (480 rules • 3,926 checked) • ⚠ 👑 lint did not run",
		},
		{
			name: "a Go run whose standard library came from the binary",
			summary: func(summary *runSummary) {
				summary.PackageCache = &packageCacheUse{FromBinary: 1258}
			},
			want: "✓ 💎 2.4s (480 rules • 3,926 checked) • standard library: 1,258 from the binary",
		},
		{
			name:    "a Go run with nothing from the binary",
			summary: func(summary *runSummary) { summary.PackageCache = &packageCacheUse{On: true, Hit: 3} },
			want:    "✓ 💎 2.4s (480 rules • 3,926 checked)",
		},
		{
			name: "a Go run whose findings cache could not be written",
			summary: func(summary *runSummary) {
				summary.PackageCache = &packageCacheUse{On: true, Missed: 3, Problems: 2}
			},
			want: "✓ 💎 2.4s (480 rules • 3,926 checked) • ⚠ 2 findings cache problems",
		},
		{
			name: "a Go run that checked its files under two builds",
			summary: func(summary *runSummary) {
				summary.Builds = []checkedBuild{{Target: "darwin/arm64 cgo", Host: true}, {Target: "windows/arm64"}}
			},
			want: "✓ 💎 2.4s (480 rules • 3,926 checked • 2 builds)",
		},
		{
			name: "two builds, with --verbose: the count before the nodes",
			summary: func(summary *runSummary) {
				warmSummary(summary)
				summary.Builds = []checkedBuild{{Target: "darwin/arm64 cgo", Host: true}, {Target: "windows/arm64"}}
			},
			options: footerOptions{Verbose: true},
			want:    "✓ 💎 0.7s (480 rules • 3 checked • 3,923 cached • 2 builds • 4,100 nodes)",
		},
		{
			// The engine writes no builds for a run of one, and one says nothing a footer does not already.
			name: "a Go run of the host's build alone",
			summary: func(summary *runSummary) {
				summary.Builds = []checkedBuild{{Target: "darwin/arm64", Host: true}}
			},
			want: "✓ 💎 2.4s (480 rules • 3,926 checked)",
		},
		{
			name:    "labelled, in a repository with several projects",
			summary: func(summary *runSummary) { summary.Label = "projects/www" },
			want:    "projects/www  ✓ 💎 2.4s (480 rules • 3,926 checked)",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			summary := cleanSummary()
			testCase.summary(&summary)
			if countsAgreeError := summary.countsAgree(); countsAgreeError != nil {
				test.Fatalf("the fixture itself is inconsistent: %v", countsAgreeError)
			}
			if got := footer(summary, plain, testCase.options); got != testCase.want {
				test.Errorf("footer:\n got  %s\n want %s", got, testCase.want)
			}
		})
	}
}

// TestFooterColor pins the color on a terminal: the verdict and total bold, what was found red, the
// parentheses dim. Without color there is not one escape code.
func TestFooterColor(test *testing.T) {
	test.Parallel()
	summary := cleanSummary()
	warmSummary(&summary)
	want := "\x1b[1m✓ 💎 0.7s\x1b[22m \x1b[2m(480 rules • 3 checked • 3,923 cached)\x1b[22m"
	if got := footer(summary, colored, footerOptions{}); got != want {
		test.Errorf("colored footer:\n got  %q\n want %q", got, want)
	}

	summary.TypeErrors, summary.Findings = 1, 2
	want = "\x1b[1m✗ ☠️ 0.7s\x1b[22m • \x1b[31m1 type error\x1b[39m • \x1b[31m2 findings\x1b[39m \x1b[2m(480 rules • 3 checked • 3,923 cached)\x1b[22m"
	if got := footer(summary, colored, footerOptions{}); got != want {
		test.Errorf("colored failing footer:\n got  %q\n want %q", got, want)
	}
	for _, options := range []footerOptions{{}, {Phases: true}, {Verbose: true}} {
		if got := footer(summary, plain, options); strings.Contains(got, "\x1b") {
			test.Errorf("a footer without color carries an escape code: %q", got)
		}
	}
}

// TestCheckedAndCachedAreAtMostEveryFileInScope is the first count invariant: the two are at most the files in
// scope, short of it only by files no phase reached, which a gap names (#1hva1ya), and a summary where they are more
// is refused rather than printed.
func TestCheckedAndCachedAreAtMostEveryFileInScope(test *testing.T) {
	test.Parallel()
	summary := cleanSummary()
	warmSummary(&summary)
	if countsAgreeError := summary.countsAgree(); countsAgreeError != nil {
		test.Fatalf("a consistent summary was refused: %v", countsAgreeError)
	}
	summary.FilesCached--
	if countsAgreeError := summary.countsAgree(); countsAgreeError != nil {
		test.Errorf("one file no phase reached was refused: %v", countsAgreeError)
	}
	summary.FilesCached += 2
	if summary.countsAgree() == nil {
		test.Error("checked and cached are one more than the files in scope, and the summary was accepted")
	}
}

// TestCoheredIsTheListAboveTheFooter is the second count invariant: the cohered count in the footer is
// the number of files the 🪄 and 💅 lines list, including those the cap leaves to `--verbose`.
func TestCoheredIsTheListAboveTheFooter(test *testing.T) {
	test.Parallel()
	for _, count := range []int{0, 1, 3, changedFilesShown, changedFilesShown + 7} {
		summary := cleanSummary()
		for index := range count {
			summary.Changed = append(
				summary.Changed,
				changedFile{Path: fmt.Sprintf("file%03d.ts", index), Formatted: true},
			)
		}
		listed := 0
		for _, line := range changedFileLines(summary.Changed, plain) {
			var more int
			if _, scanError := fmt.Sscanf(line, "… %d more", &more); scanError == nil {
				listed += more
				continue
			}
			listed++
		}
		got := footer(summary, plain, footerOptions{})
		said := 0
		if index := strings.Index(got, " cohered"); index >= 0 {
			fields := strings.Fields(got[:index])
			if _, scanError := fmt.Sscanf(fields[len(fields)-1], "%d", &said); scanError != nil {
				test.Errorf("the footer's cohered count is not a number (%v): %s", scanError, got)
			}
		}
		if said != listed || said != count {
			test.Errorf(
				"%d files rewritten: the list accounts for %d and the footer says %d cohered (%s)",
				count,
				listed,
				said,
				got,
			)
		}
	}
}

// uncheckedConditions are every way a green run can fall short of checking everything. Each is planted
// on an otherwise clean run, and the footer must say so with its marker.
var uncheckedConditions = []struct {
	name   string
	plant  func(summary *runSummary)
	marker string
}{
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
			summary.Gaps.NothingToCheck, summary.FilesInScope, summary.FilesChecked, summary.Nodes = true, 0, 0, 0
		},
		marker: "⚠ no files to check",
	},
	{
		name: "the run was narrowed to some files",
		plant: func(summary *runSummary) {
			summary.FilesInScope, summary.FilesChecked, summary.Gaps.ProgramFiles = 12, 12, 3926
		},
		marker: "⚠ only 12 of 3,926 files",
	},
	{
		name:   "a file could not be read",
		plant:  func(summary *runSummary) { summary.Gaps.Unread = "1 file the compiler left no record for" },
		marker: "⚠ 1 file the compiler left no record for",
	},
	{
		name: "Swift files no target compiles",
		plant: func(summary *runSummary) {
			summary.Gaps.Unchecked = []uncheckedEngineFile{
				{File: "/package/Stray/A.swift", Reason: "no target of this package compiles it", Language: "Swift"},
				{File: "/package/Stray/B.swift", Reason: "no target of this package compiles it", Language: "Swift"},
			}
		},
		marker: "⚠ 2 Swift files not checked",
	},
	{
		name: "a Go file only another platform builds",
		plant: func(summary *runSummary) {
			summary.Gaps.Unchecked = []uncheckedEngineFile{
				{
					File:     "/module/open_windows.go",
					Reason:   "excluded by build constraints on darwin/arm64",
					Language: "Go",
				},
			}
		},
		marker: "⚠ 1 Go file not checked",
	},
	{
		name: "a Go run withheld type errors and declarations for cgo",
		plant: func(summary *runSummary) {
			summary.Gaps.WithheldInDependencyCgo = []withheldCgoFile{
				{Package: "net", File: "/goroot/src/net/cgo_unix.go", Errors: 1},
				{Package: "os/user", File: "/goroot/src/os/user/cgo_lookup_unix.go", Errors: 2},
			}
			summary.Gaps.WithheldDependencyCgoDeclarations = 1
		},
		marker: "⚠ cgo withheld: 3 type errors in 2 dependency files, 1 declaration",
	},
	{
		name:   "a Go run withheld only declarations for cgo",
		plant:  func(summary *runSummary) { summary.Gaps.WithheldDependencyCgoDeclarations = 14 },
		marker: "⚠ cgo withheld: 14 declarations",
	},
	{
		name:   "the binary was built from a modified tree",
		plant:  func(summary *runSummary) { summary.Gaps.ModifiedBuild = true },
		marker: "⚠ built from a modified tree",
	},
}

// TestACrashFailsTheFooterAndKeepsItsMarker is the gap a run cannot pass with (#v1ah2qq): a file nothing
// checked, or a rule's verdict on a file lost, because cohere crashed. The run fails, so the footer turns
// red, and it still says what was lost.
func TestACrashFailsTheFooterAndKeepsItsMarker(test *testing.T) {
	test.Parallel()
	for _, crash := range []struct {
		name   string
		plant  func(summary *runSummary)
		marker string
	}{
		{"a file crashed", func(summary *runSummary) { summary.Gaps.CrashedFiles = 1 }, "⚠ 1 file crashed"},
		{"a rule crashed", func(summary *runSummary) {
			summary.Gaps.RuleCrashes = 1
			summary.Gaps.Unread = "1 rule crash left a file's verdict missing"
		}, "⚠ 1 rule crash left a file's verdict missing"},
	} {
		summary := cleanSummary()
		crash.plant(&summary)
		if !summary.failed() {
			test.Errorf("%s and the run did not fail", crash.name)
		}
		got := footer(summary, plain, footerOptions{})
		if !strings.HasPrefix(got, "✗ ☠️") || !strings.Contains(got, crash.marker) {
			test.Errorf("%s: the footer is %q, want a failing verdict carrying %q", crash.name, got, crash.marker)
		}
	}
}

// TestFooterSaysWhatWasNotChecked plants each condition on a clean, green run and requires its marker,
// and then requires the check itself to catch a footer that drops the marker. Without the second half a
// check that matched anything would pass, and a footer that went quiet about a gap would go unnoticed.
func TestFooterSaysWhatWasNotChecked(test *testing.T) {
	test.Parallel()
	clean := footer(cleanSummary(), plain, footerOptions{})
	if strings.Contains(clean, "⚠") || strings.Contains(clean, "not checked") {
		test.Fatalf("a clean run's footer carries a marker it has no cause for: %s", clean)
	}
	for _, condition := range uncheckedConditions {
		test.Run(condition.name, func(test *testing.T) {
			test.Parallel()
			summary := cleanSummary()
			condition.plant(&summary)
			if summary.failed() {
				test.Fatalf("the planted condition made the run fail, so it no longer tests a green footer")
			}
			got := footer(summary, plain, footerOptions{})
			if !strings.HasPrefix(got, "✓ 💎") {
				test.Errorf("planting %s turned the verdict: %s", condition.name, got)
			}
			if carriesMarkerError := carriesMarker(got, condition.marker); carriesMarkerError != nil {
				test.Error(carriesMarkerError)
			}

			// The mutant: the same run with the marker dropped from its footer. The check must fail on it.
			mutant := strings.Replace(got, " • "+condition.marker, "", 1)
			if mutant == got {
				test.Fatalf("the mutant could not drop %q from %s", condition.marker, got)
			}
			if carriesMarker(mutant, condition.marker) == nil {
				test.Errorf("the check passed a footer with %q dropped: %s", condition.marker, mutant)
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

func (marker *missingMarker) Error() string {
	return "the footer does not say " + marker.marker + ": " + marker.footer
}

// TestFooterGroupsCounts is Kirk's: a count is exact, its thousands grouped, never abbreviated.
func TestFooterGroupsCounts(test *testing.T) {
	test.Parallel()
	for count, want := range map[int]string{
		0: "0", 7: "7", 999: "999", 1000: "1,000", 3893: "3,893", 99_999: "99,999", 120_000: "120,000",
		999_999: "999,999", 1_000_000: "1,000,000", 2_412_345: "2,412,345", 340_000_000: "340,000,000", -1234: "-1,234",
	} {
		if got := grouped(count); got != want {
			test.Errorf("grouped(%d) = %s, want %s", count, got, want)
		}
	}
	for duration, want := range map[time.Duration]string{
		4 * time.Millisecond:    "0.004s",
		50 * time.Millisecond:   "0.05s",
		400 * time.Millisecond:  "0.4s",
		2400 * time.Millisecond: "2.4s",
		12 * time.Second:        "12s",
	} {
		if got := footerSeconds(duration); got != want {
			test.Errorf("footerSeconds(%s) = %s, want %s", duration, got, want)
		}
	}
}

func TestOverallFooterGolden(test *testing.T) {
	test.Parallel()
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
			name: "a project failed",
			facts: overallFacts{
				Total:            8100 * time.Millisecond,
				ProjectsByEngine: engines,
				Failed:           []string{"www (exit 1)"},
			},
			want: "✗ ☠️ 8.1s • 3 projects: 1 Swift, 2 TypeScript → ☠️ www (exit 1)",
		},
		{
			// A project that did not finish checked nothing, so the run cannot be green.
			name:  "a project did not finish",
			facts: overallFacts{Total: 8100 * time.Millisecond, ProjectsByEngine: engines, Unfinished: 1},
			want:  "✗ ☠️ 8.1s • 3 projects: 1 Swift, 2 TypeScript • ⚠ 1 project did not finish",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if got := overallFooter(testCase.facts, plain); got != testCase.want {
				test.Errorf("overallFooter:\n got  %s\n want %s", got, testCase.want)
			}
		})
	}
}

// A run whose every file was excluded fails, and its footer and `--json` say how many files and why, quoting the rule
// that decided each reason; the warning a project with no file of its language keeps is not said beside it (#08cv2mp).
func TestARunThatExcludedEveryFileFailsAndSaysWhy(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name     string
		excluded []excludedReason
		want     string
	}{
		{"one reason", []excludedReason{{Reason: "ignored by git", Count: 1, Source: ".gitignore:1"}},
			"no files checked: 1 file excluded, ignored by git (.gitignore:1)"},
		{"two reasons", []excludedReason{
			{Reason: "ignored by git", Count: 2, Source: ".gitignore:12"},
			{Reason: "outside the named paths", Count: 1},
		}, "no files checked: 3 files excluded, 2 ignored by git (.gitignore:12); 1 outside the named paths"},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			summary := cleanSummary()
			summary.Gaps.NothingToCheck, summary.FilesInScope, summary.FilesChecked = true, 0, 0
			summary.Gaps.Excluded = testCase.excluded
			if !summary.failed() {
				test.Fatal("a run that excluded every file did not fail")
			}
			got := footer(summary, plain, footerOptions{})
			if !strings.HasPrefix(got, "✗ ☠️") || !strings.Contains(got, " • "+testCase.want) {
				test.Errorf("the footer is %q, want a failing verdict saying %q", got, testCase.want)
			}
			if strings.Contains(got, "⚠ no files to check") {
				test.Errorf("the footer says the empty project's warning beside the failure: %q", got)
			}
			if verdict := summaryAsJson(summary).Verdict; verdict != "fail" {
				test.Errorf("under --json the verdict is %q, want fail", verdict)
			}
		})
	}
}

// TestWouldChangeIsSaidAgainstItsOwnScope is #hb90yfp: at adamic's root a `--no-fix` footer read "6,394 files would
// change (3,165 checked)", nested drift added to the project's own and both set against the program's files. Each
// part is said against the files it was found among, and the parentheses keep the program's count.
func TestWouldChangeIsSaidAgainstItsOwnScope(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name                         string
		wouldChange, nested          int
		considered, nestedConsidered int
		want                         string
	}{
		{"this project's alone", 4943, 0, 5574, 0, "4,943 of 5,574 files would change"},
		{"nested repositories' alone", 1451, 1451, 0, 1533, "1,451 of 1,533 files would change in nested repositories"},
		{
			"both", 6394, 1451, 5574, 1533,
			"4,943 of 5,574 files would change, 1,451 of 1,533 more in nested repositories",
		},
		{"one of one", 1, 0, 1, 0, "1 of 1 file would change"},
		// A run cache's record from before the scopes were kept has none, and says its counts alone.
		{"no scope recorded", 3, 1, 0, 0, "2 files would change, 1 more in nested repositories"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			summary := cleanSummary()
			summary.FilesInScope, summary.FilesChecked = 3165, 3165
			summary.WouldChange, summary.NestedWouldChange = testCase.wouldChange, testCase.nested
			summary.WouldChangeConsidered, summary.NestedWouldChangeConsidered = testCase.considered,
				testCase.nestedConsidered
			want := "✗ ☠️ 2.4s • " + testCase.want + " (480 rules • 3,165 checked)"
			if got := footer(summary, plain, footerOptions{}); got != want {
				test.Errorf("the footer is\n%q, want\n%q", got, want)
			}
			// wouldChange stays the total, the finding lines' count, which the release cut's gate holds equal.
			line := summaryAsJson(summary)
			if line.WouldChange != testCase.wouldChange || line.NestedWouldChange != testCase.nested ||
				line.WouldChangeConsidered != testCase.considered ||
				line.NestedWouldChangeConsidered != testCase.nestedConsidered {
				test.Errorf("under --json the counts are %d, %d, %d and %d, want %d, %d, %d and %d", line.WouldChange,
					line.NestedWouldChange, line.WouldChangeConsidered, line.NestedWouldChangeConsidered,
					testCase.wouldChange, testCase.nested, testCase.considered, testCase.nestedConsidered)
			}
		})
	}
}
