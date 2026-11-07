package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The footer is the one line a default run ends with, after its findings: the verdict, how long it took,
// what it found and changed, and how much it covered. Everything else a run can say is under `--verbose`.
//
//	✓ 💎 2.4s (480 rules • 3,926 checked)                          cold: every file checked fresh
//	✓ 💎 0.7s (480 rules • 3 checked • 3,923 cached)               three files edited, all clean
//	✓ 💎 0.7s • 2 cohered (480 rules • 3 checked • 3,923 cached)   two rewritten, listed above
//	✓ 💎 0.05s (480 rules • 3,926 cached)                          nothing changed
//	✗ ☠️ 0.8s • 1 type error • 2 findings (480 rules • 3 checked • 3,923 cached)
//
// The words are the brand's. Cohered is altered: the files cohere rewrote, fixed or formatted, the same
// files the 🪄 and 💅 lines above the footer list, said only when there were any. Checked is the files
// examined fresh this run, and cached the ones the cache answered for; together they are every file in
// scope. A count is exact, its thousands grouped, and a count of zero is left out. In order: the verdict
// and time, what was found, what was cohered, the parentheses, and anything the run did not check.
//
// With `--phases`, or `"output": { "phases": true }` in the settings, where the time went comes first
// inside the parentheses:
//
//	✓ 💎 0.7s (🕸 0.2s • 🪄 0.1s • 💅 0.02s • 🔷 0.4s • 👑 0.2s • 480 rules • 3 checked • 3,923 cached)
//
// `--verbose`'s footer adds the nodes walked and says when the whole run was replayed:
//
//	✓ 💎 0.05s • replayed (480 rules • 3,926 cached)
//
// It renders a runSummary and decides nothing: what ran, what was found and what went unchecked are the
// summary's, so this file only says them.
//
// The glyphs are the house's, from `s c`: 💎 a clean run, ☠️ a failed one, 🪄 fixing, 💅 formatting,
// 🔷 the type check and 👑 lint.
//
// One rule outranks the brevity. Anything the run did not check is in this line, green or not: a file a
// rule crashed on, a rule that skipped every file it was offered, a phase that could not run, formatting
// not checked, a run narrowed to some of the files, a project with nothing to check, a generated file the
// program asks for that does not exist yet, Swift files beside the project that no package holds, Swift
// files inside a package's directory that no target compiles and no other project's run checks, and what a
// Go run withheld of its dependencies because no cgo tool ran. The old phase line existed so that a run which
// checked nothing could not print like one that checked everything, and a one-line summary that dropped those
// cases would undo it in the line people actually read. So a gap is never left to `--verbose`.

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
	// Verbose adds the nodes walked, and says when the whole run was replayed.
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

	if cohered := summary.cohered(); cohered > 0 {
		line += " • " + grouped(cohered) + " cohered"
	}
	if options.Verbose && summary.Cache.Replayed {
		line += " • replayed"
	}

	var inside []string
	// A replay ran no phase, so it has no breakdown to give.
	if options.Phases && !summary.Cache.Replayed {
		inside = append(inside, phaseTimes(summary.Graph, summary.Phases, summary.Formatting)...)
	}
	if summary.Rules > 0 {
		inside = append(inside, counted(summary.Rules, "rule", "rules"))
	}
	if summary.FilesChecked > 0 {
		inside = append(inside, grouped(summary.FilesChecked)+" checked")
	}
	if summary.FilesCached > 0 {
		inside = append(inside, grouped(summary.FilesCached)+" cached")
	}
	if options.Verbose && summary.Nodes > 0 {
		inside = append(inside, counted(summary.Nodes, "node", "nodes"))
	}
	if len(inside) > 0 {
		line += " " + style.dim("("+strings.Join(inside, " • ")+")")
	}

	if segment := summary.Adamic.segment(); segment != "" {
		line += " • " + segment
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
//
// Formatting happens inside the fix phase, each file formatted once its fixes converge, so the fix
// phase's time holds it. 🪄 is the phase less the time a format was in flight, and 💅 that time, so the
// two add up to the phase (see formatClock).
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
				elapsed := record.Elapsed
				if name == phaseFix {
					elapsed = max(elapsed-formatting, 0)
				}
				times = append(times, phaseGlyphs[name]+" "+footerSeconds(elapsed))
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
	if len(gaps.MissingGenerated) > 0 {
		markers = append(
			markers,
			"⚠ "+counted(len(gaps.MissingGenerated), "generated file missing", "generated files missing"),
		)
	}
	if gaps.FixesHeld {
		markers = append(markers, "⚠ 🪄 fixes held")
	}
	if gaps.ProgramFiles > summary.FilesInScope && summary.FilesInScope > 0 {
		markers = append(
			markers,
			fmt.Sprintf("⚠ only %s of %s files", grouped(summary.FilesInScope), grouped(gaps.ProgramFiles)),
		)
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
			// skipped, an opt-in phase is skipped on every ordinary run, and the format project has no
			// program to type-check or lint.
			case record.Outcome == outcomeSkipped && name != phaseFix && !optInPhases[name] && !summary.FormatProject:
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
		markers = append(
			markers,
			"⚠ "+counted(gaps.RulesSkippingEverything, "rule skipped every file", "rules skipped every file"),
		)
	}
	if gaps.Unread != "" {
		markers = append(markers, "⚠ "+gaps.Unread)
	}
	if gaps.AdamicIgnored > 0 {
		markers = append(markers, "⚠ "+
			counted(gaps.AdamicIgnored, ".a file git ignores, not formatted", ".a files git ignores, not formatted"))
	}
	if len(gaps.UnclaimedSwift) > 0 {
		markers = append(markers, unclaimedSwiftMarker(gaps.UnclaimedSwift))
	}
	if len(gaps.UncheckedSwift) > 0 {
		markers = append(markers, "⚠ "+
			counted(len(gaps.UncheckedSwift), "Swift file not checked", "Swift files not checked"))
	}
	if marker := withheldCgoMarker(gaps.WithheldInDependencyCgo, gaps.WithheldDependencyCgoDeclarations); marker != "" {
		markers = append(markers, marker)
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
	// CoverageUnknown is the Swift packages whose coverage query failed. Everything they might check ran as
	// its own project too, so some files may be counted twice, and that is said here rather than only in
	// `--verbose`'s notes (#sxs26s3).
	CoverageUnknown int
	// UnclaimedSwift is the directories of Swift files no package holds, which no project's run checked
	// (#ybspptb). A gap, not a failure: it never changes the verdict, and it is never left to the notes.
	UnclaimedSwift []unclaimedSwiftDirectory
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
	if facts.CoverageUnknown > 0 {
		line += " • ⚠ " +
			counted(facts.CoverageUnknown, "Swift coverage query failed, so some files may be counted twice",
				"Swift coverage queries failed, so some files may be counted twice")
	}
	if len(facts.UnclaimedSwift) > 0 {
		line += " • " + unclaimedSwiftMarker(facts.UnclaimedSwift)
	}
	return line
}

// withheldCgoMarker says what a Go run withheld because no cgo tool ran, each part only when there is some, or is
// empty when it withheld nothing. The account under `--verbose` names the files; this only counts them, so a
// green Linux run reaching `net` never reads like one that saw every dependency whole:
//
//	⚠ cgo withheld: 1 type error in 1 dependency file, 14 declarations
//	⚠ cgo withheld: 14 declarations
func withheldCgoMarker(files []withheldCgoFile, declarations int) string {
	var parts []string
	if len(files) > 0 {
		typeErrors := 0
		for _, file := range files {
			typeErrors += file.Errors
		}
		parts = append(parts, counted(typeErrors, "type error", "type errors")+" in "+
			counted(len(files), "dependency file", "dependency files"))
	}
	if declarations > 0 {
		parts = append(parts, counted(declarations, "declaration", "declarations"))
	}
	if len(parts) == 0 {
		return ""
	}
	return "⚠ cgo withheld: " + strings.Join(parts, ", ")
}

// unclaimedSwiftMarker says the directories of Swift files no package checks, the first three by name and
// the rest counted, so the line stays short however many there are:
//
//	⚠ Swift files no package checks: CapturePhone/
//	⚠ Swift files no package checks: A/, B/, C/ and 2 more
func unclaimedSwiftMarker(directories []unclaimedSwiftDirectory) string {
	const named = 3
	names := make([]string, 0, named)
	for _, unclaimed := range directories[:min(len(directories), named)] {
		names = append(names, unclaimed.Directory)
	}
	marker := "⚠ Swift files no package checks: " + strings.Join(names, ", ")
	if len(directories) > named {
		marker += fmt.Sprintf(" and %d more", len(directories)-named)
	}
	return marker
}

// footerSeconds is a duration as the footer says it: 0.004s, 0.05s, 0.4s, 2.4s, 12s. Below a hundredth it
// keeps a third decimal, so a fast replay never reads as taking no time at all.
func footerSeconds(duration time.Duration) string {
	seconds := duration.Seconds()
	switch {
	case seconds < 0.01:
		return fmt.Sprintf("%.3fs", seconds)
	case seconds < 0.1:
		return fmt.Sprintf("%.2fs", seconds)
	case seconds < 10:
		return fmt.Sprintf("%.1fs", seconds)
	default:
		return fmt.Sprintf("%.0fs", seconds)
	}
}

// grouped is a count as the footer says it, exact, with its thousands grouped: 999, 1,000, 3,893,
// 1,234,567. Exact, because a count is read to see how much was checked, and an abbreviation rounds that
// away. Only the human view groups; `--json` prints the integer.
func grouped(count int) string {
	digits := strconv.Itoa(count)
	sign := ""
	if count < 0 {
		sign, digits = "-", digits[1:]
	}
	var builder strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			builder.WriteByte(',')
		}
		builder.WriteRune(digit)
	}
	return sign + builder.String()
}

// counted is a count with its noun, singular or plural, grouped past 999.
func counted(count int, singular, pluralNoun string) string {
	if count == 1 {
		return "1 " + singular
	}
	return grouped(count) + " " + pluralNoun
}
