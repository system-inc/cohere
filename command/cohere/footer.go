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
//	✗ ☠️ 5.4s • 4,943 of 5,574 files would change, 1,451 of 1,533 more in nested repositories (3,165 checked)
//
// The words are the brand's. Cohered is altered: the files cohere rewrote, fixed or formatted, the same
// files the 🪄 and 💅 lines above the footer list, said only when there were any. Checked is the files a
// phase examined fresh this run, and cached the ones the cache answered for; together they are every file in
// scope but the ones no phase reached, which a marker after them names. A run whose files no phase examined
// reads no checked count at all, never the files it was to check (#1hva1ya). Types-only is the files an ignore
// naming the type check matched, which only that check reads, and a file a plain ignore matched is in no count
// (#cxeq6jw):
//
//	✓ 💎 2.4s (480 rules • 3,900 checked • 26 types-only)
//
// A Go run that checked its files under more builds than the host's, since some file of ours compiles only for
// another platform, says how many, and `--verbose` says each (#72n9mq0):
//
//	✓ 💎 4.1s (480 rules • 3,926 checked • 2 builds)
//
// A Go run whose standard library came from the entries a release binary carries says how many packages did, since
// no file count holds them:
//
//	✓ 💎 1.9s (480 rules • 3,926 checked) • standard library: 258 from the binary
//
// A count is exact, its thousands grouped, and a count of zero is left out. In order: the verdict
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
// The glyphs are the house's: 💎 a clean run, ☠️ a failed one, 🪄 fixing, 💅 formatting,
// 🔷 the type check and 👑 lint.
//
// One rule outranks the brevity. Anything the run did not check is in this line, green or not: a file a
// rule crashed on, a rule that skipped every file it was offered, a phase that could not run, formatting
// not checked, a run narrowed to some of the files, a project with nothing to check, a generated file the
// program asks for that does not exist yet, Swift files inside a package's directory that no target compiles
// and no other project's run checks, Go files only another platform builds, and what a
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
		line += " • " + style.red(wouldChangeSegment(summary))
	}
	if summary.Gaps.nothingChecked() {
		line += " • " + style.red(nothingCheckedSentence(summary.Gaps.Excluded))
	}
	if len(summary.Gaps.CensusMissed) > 0 {
		line += " • " + style.red(counted(len(summary.Gaps.CensusMissed), "file", "files")+
			" the load reached and the census never counted")
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
	if summary.FilesTypesOnly > 0 {
		inside = append(inside, grouped(summary.FilesTypesOnly)+" types-only")
	}
	// One build is every run's, so only a Go run that checked others beside the host's says how many.
	if len(summary.Builds) > 1 {
		inside = append(inside, counted(len(summary.Builds), "build", "builds"))
	}
	if options.Verbose && summary.Nodes > 0 {
		inside = append(inside, counted(summary.Nodes, "node", "nodes"))
	}
	if len(inside) > 0 {
		line += " " + style.dim("("+strings.Join(inside, " • ")+")")
	}

	// The standard library's packages a Go run restored from the binary rather than analyzed, which no file count
	// holds, so a cold run that reused them reads as such.
	if summary.PackageCache != nil && summary.PackageCache.FromBinary > 0 {
		line += " • " + summary.PackageCache.segment()
	}
	// A Go run whose findings cache could not be read or written, which stderr says one by one, says how many where
	// people look (#27r8r24). Marked as a warning, since the next run pays for it, but apart from the markers of what
	// went unchecked: a problem never changes a finding.
	if summary.PackageCache != nil && (summary.PackageCache.Problems > 0 || summary.PackageCache.WriteFailed > 0) {
		line += " • ⚠ " + summary.PackageCache.problemsSegment()
	}
	// Said apart from the time, which leaves them out, so a build is never read as checking (#ket0hrp).
	for _, segment := range engineBuildSegments(summary.EngineBuilds) {
		line += " • " + segment
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

// wouldChangeSegment is what a `--no-fix` run found would change, each part against the files it was found among:
// this project's against the files its fix phase examined, a nested repository's against the nested files the
// formatter read. Neither is set against the parentheses' checked count, the program's files, which holds no
// Markdown or JSON and no nested file (#hb90yfp):
//
//	4,943 of 5,574 files would change, 1,451 of 1,533 more in nested repositories
//	1,451 of 1,533 files would change in nested repositories
//
// A summary with no scope for a part, as one a run cache recorded before the scopes were kept, says its count alone.
func wouldChangeSegment(summary runSummary) string {
	own := summary.WouldChange - summary.NestedWouldChange
	var parts []string
	if own > 0 {
		parts = append(parts, ofConsidered(own, summary.WouldChangeConsidered)+" would change")
	}
	switch {
	case summary.NestedWouldChange > 0 && own > 0:
		// The part before has said "files", and "more" carries it.
		nested := grouped(summary.NestedWouldChange)
		if summary.NestedWouldChangeConsidered > 0 {
			nested += " of " + grouped(summary.NestedWouldChangeConsidered)
		}
		parts = append(parts, nested+" more in nested repositories")
	case summary.NestedWouldChange > 0:
		parts = append(parts, ofConsidered(summary.NestedWouldChange, summary.NestedWouldChangeConsidered)+
			" would change in nested repositories")
	}
	return strings.Join(parts, ", ")
}

// ofConsidered is a count of files against the files it was found among, "4,943 of 5,574 files", its noun the
// scope's, or the count alone, "3 files", when there is no scope to give.
func ofConsidered(count, considered int) string {
	if considered <= 0 {
		return counted(count, "file", "files")
	}
	return grouped(count) + " of " + counted(considered, "file", "files")
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
	// A run that excluded every file it had says so among what it found, and fails; this marker is the project
	// with no file of its language at all.
	if gaps.NothingToCheck && !gaps.nothingChecked() && len(gaps.CensusMissed) == 0 {
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
	// A phase the run stopped before has no record, so no outcome above can say it.
	for _, name := range gaps.PhasesNotRun {
		markers = append(markers, fmt.Sprintf("⚠ %s %s did not run", phaseGlyphs[phaseName(name)], name))
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
	if gaps.NestedUnread != "" {
		markers = append(markers, "⚠ nested repositories not read: "+gaps.NestedUnread)
	}
	if gaps.NestedUnchecked > 0 {
		markers = append(
			markers,
			"⚠ "+counted(gaps.NestedUnchecked, "nested file not checked", "nested files not checked"),
		)
	}
	if gaps.AdamicIgnored > 0 {
		markers = append(markers, "⚠ "+
			counted(gaps.AdamicIgnored, ".a file git ignores, not formatted", ".a files git ignores, not formatted"))
	}
	markers = append(markers, uncheckedEngineFileMarkers(gaps.Unchecked)...)
	if len(gaps.NotWritten) > 0 {
		markers = append(markers, "⚠ not written: "+counted(len(gaps.NotWritten),
			"file reached through a link outside the root", "files reached through a link outside the root"))
	}
	if len(gaps.ChangedDuringRun) > 0 {
		markers = append(markers, "⚠ changed during the run: "+counted(len(gaps.ChangedDuringRun),
			"file, its findings stale", "files, their findings stale"))
	}
	if marker := withheldCgoMarker(gaps.WithheldInDependencyCgo, gaps.WithheldDependencyCgoDeclarations); marker != "" {
		markers = append(markers, marker)
	}
	if gaps.ModifiedBuild {
		markers = append(markers, "⚠ built from a modified tree")
	}
	return markers
}

// uncheckedEngineFileMarkers counts a run's unchecked files in its language's noun, one marker per language in the
// order the files first name it, "⚠ 3 Swift files not checked" or "⚠ 1 Go file not checked". A run is one engine's,
// so there is one marker; the noun is each file's own, so no file is ever counted in another language's words.
func uncheckedEngineFileMarkers(files []uncheckedEngineFile) []string {
	var languages []string
	countByLanguage := map[string]int{}
	for _, file := range files {
		if countByLanguage[file.Language] == 0 {
			languages = append(languages, file.Language)
		}
		countByLanguage[file.Language]++
	}
	markers := make([]string, 0, len(languages))
	for _, language := range languages {
		markers = append(markers, "⚠ "+counted(countByLanguage[language], language+" file not checked",
			language+" files not checked"))
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
	// UnusedIgnores is the root's ignorePatterns entries that matched nothing, each a finding of this run's own
	// that fails it, whatever its projects found (#cxeq6jw).
	UnusedIgnores int
	// CoverageUnknown is the Swift packages whose coverage query failed. Everything they might check ran as
	// its own project too, so some files may be counted twice, and that is said here rather than only in
	// `--verbose`'s notes (#sxs26s3).
	CoverageUnknown int
	// EngineBuilds are the engines this run built before its projects ran, as a coverage query does: their
	// time is in Total, and said apart from it.
	EngineBuilds []engineBuild
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
	if len(facts.Failed) > 0 || facts.Unfinished > 0 || facts.UnusedIgnores > 0 {
		verdict = "✗ ☠️ "
	}
	line := style.bold(verdict+footerSeconds(facts.Total)) + " • " + counted(total, "project", "projects")
	if len(byEngine) > 1 {
		line += ": " + strings.Join(byEngine, ", ")
	}
	if facts.UnusedIgnores > 0 {
		line += " • " + style.red(counted(facts.UnusedIgnores, "unused ignore pattern", "unused ignore patterns"))
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
	for _, segment := range engineBuildSegments(facts.EngineBuilds) {
		line += " • " + segment
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

// nothingCheckedSentence is the footer's words for a run that excluded every file it had: how many, and each reason
// with the rule that decided it, as `no files checked: 1 file excluded, ignored by git (.gitignore:1)`.
func nothingCheckedSentence(excluded []excludedReason) string {
	total := 0
	reasons := make([]string, 0, len(excluded))
	for _, entry := range excluded {
		total += entry.Count
		reason := entry.Reason
		if len(excluded) > 1 {
			reason = fmt.Sprintf("%d %s", entry.Count, reason)
		}
		if entry.Source != "" {
			reason += " (" + entry.Source + ")"
		}
		reasons = append(reasons, reason)
	}
	return "no files checked: " + counted(total, "file excluded", "files excluded") + ", " +
		strings.Join(reasons, "; ")
}
