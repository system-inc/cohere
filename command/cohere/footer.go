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
//	✓ 💎 (2.4s → 🪄 0.4s • 💅 0.3s • 🔷 0.6s • 👑 1.1s • 3.9K files • 2.4M nodes)
//	✗ ☠️ (2.4s → 1 type error • 1 finding • 3.9K files • 2.4M nodes)
//	✓ 💎 (0.05s ↺ replayed • 3.9K files)
//
// It renders a runSummary and decides nothing: what ran, what was found and what went unchecked are the
// summary's, so this file only says them.
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

// phaseGlyphs are the house's glyph for each phase that has one.
var phaseGlyphs = map[phaseName]string{
	phaseFix:    "🪄",
	phaseTypes:  "🔷",
	phaseLint:   "👑",
	phaseUnused: "🧹",
}

// footer renders a run's summary as its one line: the verdict, then everything else inside parentheses,
// the total first and each further item after a bullet.
func footer(summary runSummary) string {
	verdict := "✓ 💎"
	if summary.failed() {
		verdict = "✗ ☠️"
	}

	// The total leads, joined to what comes next by the arrow when phases or counts follow it, or by the
	// replay mark when no phase ran.
	opening := footerSeconds(summary.Total)
	var items []string
	switch {
	case summary.Cache.Replayed:
		opening += " ↺ replayed"
	case summary.failed():
		opening += " →"
	default:
		opening += " →"
		items = append(items, phaseTimes(summary.Phases, summary.Formatting)...)
	}

	if summary.TypeErrors > 0 {
		items = append(items, counted(summary.TypeErrors, "type error", "type errors"))
	}
	if summary.Findings > 0 {
		items = append(items, counted(summary.Findings, "finding", "findings"))
	}
	if summary.WouldChange > 0 {
		items = append(items, counted(summary.WouldChange, "file would change", "files would change"))
	}
	if summary.Files > 0 {
		items = append(items, abbreviated(summary.Files)+" files")
	}
	if summary.Nodes > 0 && !summary.Cache.Replayed {
		items = append(items, abbreviated(summary.Nodes)+" nodes")
	}
	items = append(items, uncheckedMarkers(summary)...)

	inside := opening
	if len(items) > 0 {
		// After the arrow the first item needs only a space; after the replay mark it takes a bullet.
		joiner := " "
		if summary.Cache.Replayed {
			joiner = " • "
		}
		inside += joiner + strings.Join(items, " • ")
	}
	line := fmt.Sprintf("%s (%s)", verdict, inside)
	if summary.Label != "" {
		line = summary.Label + "  " + line
	}
	return line
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
func uncheckedMarkers(summary runSummary) []string {
	gaps := summary.Gaps
	var markers []string
	if gaps.NothingToCheck {
		markers = append(markers, "⚠ no files to check")
	}
	if gaps.FilesInScope > 0 && gaps.FilesInScope < summary.Files {
		markers = append(markers, fmt.Sprintf("⚠ only %s of the files", abbreviated(gaps.FilesInScope)))
	}
	for _, name := range phaseOrder {
		for _, record := range summary.Phases {
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
	if gaps.FormattingNotChecked {
		markers = append(markers, "💅 formatting not checked")
	}
	if gaps.CrashedFiles > 0 {
		markers = append(markers, "⚠ "+counted(gaps.CrashedFiles, "file crashed", "files crashed"))
	}
	if gaps.RulesSkippingEverything > 0 {
		markers = append(markers, "⚠ "+counted(gaps.RulesSkippingEverything, "rule skipped every file", "rules skipped every file"))
	}
	if gaps.Unread != "" {
		markers = append(markers, "⚠ "+gaps.Unread)
	}
	if gaps.ModifiedBuild {
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
//	✓ 💎 (8.1s • 3 projects: 1 Swift, 2 TypeScript)
//	✗ ☠️ (8.1s • 3 projects: 1 Swift, 2 TypeScript • ☠️ www (exit 1))
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

	verdict := "✓ 💎"
	if len(facts.Failed) > 0 || facts.Unfinished > 0 {
		verdict = "✗ ☠️"
	}
	projects := counted(total, "project", "projects")
	if len(byEngine) > 1 {
		projects += ": " + strings.Join(byEngine, ", ")
	}
	items := []string{footerSeconds(facts.Total), projects}
	if len(facts.Failed) > 0 {
		items = append(items, "☠️ "+strings.Join(facts.Failed, ", "))
	}
	if facts.Unfinished > 0 {
		items = append(items, "⚠ "+counted(facts.Unfinished, "project did not finish", "projects did not finish"))
	}
	if facts.NestedNotEntered > 0 {
		items = append(items, "⚠ "+counted(facts.NestedNotEntered, "nested repository not checked", "nested repositories not checked"))
	}
	return fmt.Sprintf("%s (%s)", verdict, strings.Join(items, " • "))
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
