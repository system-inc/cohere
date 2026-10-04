package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The footer is the one line a default run ends with, after its findings: the verdict, where the time
// went, and how much was checked. Everything else a run can say is under `--verbose`.
//
//	✓ 💎 2.4s → 🪄 0.4s • 💅 0.3s • 🔷 0.6s • 👑 1.1s · 3.9K files · 2.4M nodes
//	✗ ☠️ 2.4s → 1 type error · 1 finding · 3.9K files · 2.4M nodes
//	✓ 💎 0.05s ↺ replayed · 3.9K files
//
// The glyphs are the house's, from `s c`: 💎 a clean run, ☠️ a failed one, 🪄 fixing, 💅 formatting,
// 🔷 the type check and 👑 lint.
//
// One rule outranks the brevity. Anything the run did not check is in this line, green or not: a file a
// rule crashed on, a rule that skipped every file it was offered, a phase that could not run, formatting
// not checked, a run narrowed to some of the files, a project with nothing to check. The old phase line
// existed so that a run which checked nothing could not print like one that checked everything, and a
// one-line summary that dropped those cases would undo it in the line people actually read. So a gap is
// never left to `--verbose`.

// footerFacts is what the footer says beyond the phase records: the totals, and every way the run fell
// short of checking everything.
type footerFacts struct {
	// Label is the project's path in a repository with several, which the line leads with. Empty for a
	// project checked on its own.
	Label string

	// Total is the run's wall clock, the number a developer waited for.
	Total time.Duration
	// Replayed is a run answered from the run cache, which ran no phase.
	Replayed bool

	TypeErrors int
	Findings   int
	// WouldChange is the files a `--no-fix` run found a fix or formatting would change, which fail it.
	WouldChange int

	Files int
	Nodes int

	// FilesInScope is set when a run was narrowed to some of the program's Files.
	FilesInScope int

	CrashedFiles int
	// RulesSkippingEverything is the rules that declined every file they were offered.
	RulesSkippingEverything int
	// FormattingNotChecked is a run that did not check formatting.
	FormattingNotChecked bool
	// NothingToCheck is a project with no files to check.
	NothingToCheck bool
	// ModifiedBuild is a binary built from a modified tree, which no commit reproduces.
	ModifiedBuild bool
	// Unread is files the run could not check for any other reason, said as the run said it.
	Unread string
}

// failed is whether the run found anything, which is what the exit code says too.
func (facts footerFacts) failed() bool {
	return facts.TypeErrors > 0 || facts.Findings > 0 || facts.WouldChange > 0
}

// phaseGlyphs are the house's glyph for each phase that has one.
var phaseGlyphs = map[phaseName]string{
	phaseFix:    "🪄",
	phaseTypes:  "🔷",
	phaseLint:   "👑",
	phaseUnused: "🧹",
}

// footer renders the line. records are the run's phases, and formatting is how long formatting took
// when it ran (zero when it did not).
func footer(records []phaseRecord, formatting time.Duration, facts footerFacts) string {
	var line strings.Builder
	if facts.Label != "" {
		line.WriteString(facts.Label + "  ")
	}
	if facts.failed() {
		fmt.Fprintf(&line, "✗ ☠️ %s", footerSeconds(facts.Total))
	} else {
		fmt.Fprintf(&line, "✓ 💎 %s", footerSeconds(facts.Total))
	}

	var parts []string
	switch {
	case facts.Replayed:
		line.WriteString(" ↺ replayed")
	case facts.failed():
		line.WriteString(" →")
	default:
		line.WriteString(" →")
		parts = append(parts, strings.Join(phaseTimes(records, formatting), " • "))
	}

	if facts.TypeErrors > 0 {
		parts = append(parts, counted(facts.TypeErrors, "type error", "type errors"))
	}
	if facts.Findings > 0 {
		parts = append(parts, counted(facts.Findings, "finding", "findings"))
	}
	if facts.WouldChange > 0 {
		parts = append(parts, counted(facts.WouldChange, "file would change", "files would change"))
	}
	if facts.Files > 0 {
		parts = append(parts, abbreviated(facts.Files)+" files")
	}
	if facts.Nodes > 0 && !facts.Replayed {
		parts = append(parts, abbreviated(facts.Nodes)+" nodes")
	}
	parts = append(parts, uncheckedMarkers(records, facts)...)

	if len(parts) == 0 {
		return line.String()
	}
	separator := " · "
	if !facts.Replayed {
		// The arrow already separates the verdict from what follows it.
		return line.String() + " " + strings.Join(nonEmpty(parts), separator)
	}
	return line.String() + separator + strings.Join(nonEmpty(parts), separator)
}

// phaseTimes is each phase that spent time, in pipeline order, formatting after fixing. A phase that did
// not run is left out; one that reused another's walk says so instead of claiming a time of its own.
func phaseTimes(records []phaseRecord, formatting time.Duration) []string {
	var times []string
	for _, name := range phaseOrder {
		for _, record := range records {
			if record.Name != name {
				continue
			}
			switch record.Outcome {
			case outcomeRan, outcomeChecked:
				times = append(times, phaseGlyphs[name]+" "+footerSeconds(record.Elapsed))
			case outcomeReused:
				times = append(times, phaseGlyphs[name]+" in "+phaseGlyphs[phaseFix])
			}
		}
		if name == phaseFix && formatting > 0 {
			times = append(times, "💅 "+footerSeconds(formatting))
		}
	}
	return times
}

// uncheckedMarkers is every way the run fell short of checking everything, one marker each, in a fixed
// order so the line reads the same way every time.
func uncheckedMarkers(records []phaseRecord, facts footerFacts) []string {
	var markers []string
	if facts.NothingToCheck {
		markers = append(markers, "⚠ no files to check")
	}
	if facts.FilesInScope > 0 && facts.FilesInScope < facts.Files {
		markers = append(markers, fmt.Sprintf("⚠ only %s of the files", abbreviated(facts.FilesInScope)))
	}
	for _, name := range phaseOrder {
		for _, record := range records {
			if record.Name != name {
				continue
			}
			switch {
			case record.Outcome == outcomeNotReached:
				markers = append(markers, fmt.Sprintf("⚠ %s %s did not run", phaseGlyphs[name], name))
			// A reporting phase skipped is a gap, and it is said even when the caller narrowed the run on
			// purpose, since the line must not read like a whole check. Fix withholds no finding when
			// skipped, and an opt-in phase is skipped on every ordinary run.
			case record.Outcome == outcomeSkipped && name != phaseFix && !optInPhases[name]:
				markers = append(markers, fmt.Sprintf("⚠ %s %s not checked", phaseGlyphs[name], name))
			}
		}
	}
	if facts.FormattingNotChecked {
		markers = append(markers, "💅 formatting not checked")
	}
	if facts.CrashedFiles > 0 {
		markers = append(markers, "⚠ "+counted(facts.CrashedFiles, "file crashed", "files crashed"))
	}
	if facts.RulesSkippingEverything > 0 {
		markers = append(markers, "⚠ "+counted(facts.RulesSkippingEverything, "rule skipped every file", "rules skipped every file"))
	}
	if facts.Unread != "" {
		markers = append(markers, "⚠ "+facts.Unread)
	}
	if facts.ModifiedBuild {
		markers = append(markers, "⚠ built from a modified tree")
	}
	return markers
}

// overallFacts is a repository with several projects, each checked as its own run with its own footer,
// for the one verdict line printed after them all.
type overallFacts struct {
	Total time.Duration
	// ProjectsByEngine counts the projects checked, by the engine that checked them.
	ProjectsByEngine map[string]int
	// Failed is each project that found something, as its label with its exit code.
	Failed []string
	// Unfinished is the projects whose run did not finish, which checked nothing anyone can rely on.
	Unfinished int
	// NestedNotEntered is the nested repositories the run found and did not check. Ignored, dependency
	// and build directories are left out on purpose and are not gaps; a nested repository is code.
	NestedNotEntered int
}

// overallFooter renders the verdict line for a repository with several projects:
//
//	✓ 💎 8.1s · 3 projects (2 TypeScript, 1 Swift)
//	✗ ☠️ 8.1s · 3 projects (2 TypeScript, 1 Swift) · ☠️ www (exit 1)
//
// The run is green only when every project is, and a project that did not finish is a gap, never a pass.
func overallFooter(facts overallFacts) string {
	total := 0
	engines := make([]string, 0, len(facts.ProjectsByEngine))
	for engine, count := range facts.ProjectsByEngine {
		total += count
		engines = append(engines, engine)
	}
	sort.Strings(engines)
	byEngine := make([]string, 0, len(engines))
	for _, engine := range engines {
		byEngine = append(byEngine, fmt.Sprintf("%d %s", facts.ProjectsByEngine[engine], engine))
	}

	failed := len(facts.Failed) > 0 || facts.Unfinished > 0
	var line strings.Builder
	if failed {
		fmt.Fprintf(&line, "✗ ☠️ %s", footerSeconds(facts.Total))
	} else {
		fmt.Fprintf(&line, "✓ 💎 %s", footerSeconds(facts.Total))
	}
	fmt.Fprintf(&line, " · %s", counted(total, "project", "projects"))
	if len(byEngine) > 1 {
		fmt.Fprintf(&line, " (%s)", strings.Join(byEngine, ", "))
	}
	if len(facts.Failed) > 0 {
		fmt.Fprintf(&line, " · ☠️ %s", strings.Join(facts.Failed, ", "))
	}
	if facts.Unfinished > 0 {
		line.WriteString(" · ⚠ " + counted(facts.Unfinished, "project did not finish", "projects did not finish"))
	}
	if facts.NestedNotEntered > 0 {
		line.WriteString(" · ⚠ " + counted(facts.NestedNotEntered, "nested repository not checked", "nested repositories not checked"))
	}
	return line.String()
}

// footerSeconds is a duration as the footer says it: 0.05s, 0.4s, 2.4s, 12s.
func footerSeconds(duration time.Duration) string {
	seconds := duration.Seconds()
	switch {
	case seconds < 0.1:
		return fmt.Sprintf("%.2fs", seconds)
	case seconds < 10:
		return fmt.Sprintf("%.1fs", seconds)
	default:
		return fmt.Sprintf("%.0fs", seconds)
	}
}

// abbreviated is a count as the footer says it: 999, 1K, 1.2K, 120K, 3.4M.
func abbreviated(count int) string {
	if count < 1000 {
		return fmt.Sprintf("%d", count)
	}
	value, unit := float64(count)/1000, "K"
	if count >= 999_950 {
		value, unit = float64(count)/1_000_000, "M"
	}
	// One decimal below 100, none above, and none when it would be `.0`.
	text := fmt.Sprintf("%.1f", value)
	if value >= 99.95 {
		text = fmt.Sprintf("%.0f", value)
	}
	return strings.TrimSuffix(text, ".0") + unit
}

// counted is a count with its noun, singular or plural, abbreviated past 999.
func counted(count int, singular, pluralNoun string) string {
	if count == 1 {
		return "1 " + singular
	}
	return abbreviated(count) + " " + pluralNoun
}

func nonEmpty(parts []string) []string {
	kept := parts[:0]
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return kept
}
