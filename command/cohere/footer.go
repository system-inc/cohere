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
//	✓ 💎 0.7s (480 rules • 3.9K files • 2.4M nodes)
//	✓ 💎 0.05s (480 rules • 3.9K files)
//	✗ ☠️ 0.8s • 1 type error • 2 findings (480 rules • 3.9K files • 2.4M nodes)
//
// In the parentheses, the rules that ran, the files in scope, and the nodes walked this run, which a
// replay walked none of. With `--phases`, or `"output": { "phases": true }` in the settings, where the
// time went comes first inside them:
//
//	✓ 💎 0.7s (🕸 0.2s • 🪄 0.1s • 💅 0.02s • 🔷 0.4s • 👑 0.2s • 480 rules • 3.9K files • 2.4M nodes)
//
// Under `--verbose` the footer also says how the files were checked: how many this run cohered (the
// brand's verb, checked fresh) against how many the cache answered for, or that the run was replayed.
//
//	✓ 💎 0.7s • 3 files cohered • 3.9K cached (480 rules • 3.9K files • 2.4M nodes)
//	✓ 💎 0.05s • replayed (480 rules • 3.9K files)
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

// footerOptions are the two ways a reader asks the footer for more.
type footerOptions struct {
	// Phases puts where the time went first inside the parentheses.
	Phases bool
	// Verbose says how the files were checked: cohered against cached, or replayed.
	Verbose bool
}

// footer renders a run's summary as its one line. The verdict and total lead in bold, what was found
// follows in red, and the parentheses are dim.
func footer(summary runSummary, style textStyle, options footerOptions) string {
	verdict := "✓ 💎 "
	if summary.failed() {
		verdict = "✗ ☠️ "
	}
	line := style.bold(verdict + footerSeconds(summary.Total))

	// What was found comes right after the time, the first thing a failing run's reader needs.
	if summary.TypeErrors > 0 {
		line += " • " + style.red(counted(summary.TypeErrors, "type error", "type errors"))
	}
	if summary.Findings > 0 {
		line += " • " + style.red(counted(summary.Findings, "finding", "findings"))
	}
	if summary.WouldChange > 0 {
		line += " • " + style.red(counted(summary.WouldChange, "file would change", "files would change"))
	}

	if options.Verbose {
		if summary.Cache.Replayed {
			line += " • replayed"
		} else {
			line += " • " + counted(summary.FilesCohered, "file cohered", "files cohered")
			if summary.FilesCached > 0 {
				line += " • " + abbreviated(summary.FilesCached) + " cached"
			}
		}
	}

	var inside []string
	// A replay ran no phase, so it has no breakdown to give.
	if options.Phases && !summary.Cache.Replayed {
		inside = append(inside, phaseTimes(summary.Graph, summary.Phases, summary.Formatting)...)
	}
	if summary.Rules > 0 {
		inside = append(inside, counted(summary.Rules, "rule", "rules"))
	}
	if files := summary.files(); files > 0 {
		inside = append(inside, counted(files, "file", "files"))
	}
	if summary.Nodes > 0 {
		inside = append(inside, counted(summary.Nodes, "node", "nodes"))
	}
	if len(inside) > 0 {
		line += " " + style.dim("("+strings.Join(inside, " • ")+")")
	}

	if markers := uncheckedMarkers(summary); len(markers) > 0 {
		line += " • " + strings.Join(markers, " • ")
	}
	if summary.Label != "" {
		line = summary.Label + "  " + line
	}
	return line
}

// phaseTimes is where the time went: the graph, then each phase that spent time, in pipeline order,
// formatting after fixing. A phase that did not run is left out, so 🧹 appears only when `--unused` ran
// it; one that reused another's walk says so instead of claiming a time of its own.
func phaseTimes(graph time.Duration, records []phaseRecord, formatting time.Duration) []string {
	var times []string
	if graph > 0 {
		times = append(times, "🕸 "+footerSeconds(graph))
	}
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
	if gaps.FilesInScope > 0 && gaps.FilesInScope < summary.files() {
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
//	✓ 💎 8.1s • 3 projects: 1 Swift, 2 TypeScript
//	✗ ☠️ 8.1s • 3 projects: 1 Swift, 2 TypeScript → ☠️ www (exit 1)
//
// The run is green only when every project is, and a project that did not finish is a gap, never a pass.
func overallFooter(facts overallFacts, style textStyle) string {
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

	verdict := "✓ 💎 "
	if len(facts.Failed) > 0 || facts.Unfinished > 0 {
		verdict = "✗ ☠️ "
	}
	line := style.bold(verdict+footerSeconds(facts.Total)) + " • " + counted(total, "project", "projects")
	if len(byEngine) > 1 {
		line += ": " + strings.Join(byEngine, ", ")
	}
	if len(facts.Failed) > 0 {
		line += " → " + style.red("☠️ "+strings.Join(facts.Failed, ", "))
	}
	if facts.Unfinished > 0 {
		line += " • ⚠ " + counted(facts.Unfinished, "project did not finish", "projects did not finish")
	}
	if facts.NestedNotEntered > 0 {
		line += " • ⚠ " + counted(facts.NestedNotEntered, "nested repository not checked", "nested repositories not checked")
	}
	return line
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
