package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// A root holding several projects ends its default run on one footer line, the way a single project does, with
// every project's counts summed into it (#d2xga9h). Kirk, 2026-11-07: "i liked just the one line". Before, each
// project printed its own footer and the root added a line counting the projects:
//
//	.  ✓ 💎 0.06s (428 rules • 4,900 cached)
//	. (Swift)  ✓ 💎 0.004s (57 rules • 4 cached)
//	✓ 💎 0.1s • 2 projects: 1 Swift, 1 TypeScript
//
// and now, by default:
//
//	✓ 💎 0.1s (485 rules • 4,904 cached)
//
// The time is the whole run's wall clock. Rules sum, since each language's rules are its own. Checked, cached and
// cohered sum. A failure is the same line with its counts summed, its findings printed above it with their paths.
//
// What a combined green must never do is hide a project that checked nothing, so no gap is summed or folded: each
// project's markers are appended to the line, each naming its project, `⚠ 🔷 types did not run (Phone)`. A project
// whose run did not finish, or that handed back no summary, is named too, and fails the line when it exited
// nonzero. `--verbose` and `--phases` keep each project's own footer and the line that counts the projects, since
// phase times don't sum across projects running side by side; `--json` is one summary per project, as before.

// projectSummaryVariable names the file a project's run hands its summary to, instead of printing its footer, when
// the root it belongs to prints one line for all of them. Set only for a default run without `--phases`.
const projectSummaryVariable = "COHERE_PROJECT_SUMMARY"

// projectSummary is what a project's run hands its root: its summary, and the engines it built, which a summary's
// own encoding leaves out since no record keeps them.
type projectSummary struct {
	Summary      runSummary
	EngineBuilds []engineBuild
}

// handSummaryToRoot writes summary for the root this run is one project of, and reports whether it did, so the
// run prints no footer of its own. A run with nowhere to hand it, or that cannot write it, prints its own footer,
// so nothing it would have said is lost; the root then names it as a project that handed back no summary.
func handSummaryToRoot(summary runSummary) bool {
	path := os.Getenv(projectSummaryVariable)
	if path == "" || activeOutput.Phases {
		return false
	}
	encoded, encodeError := json.Marshal(projectSummary{Summary: summary, EngineBuilds: summary.EngineBuilds})
	if encodeError != nil {
		return false
	}
	return os.WriteFile(path, encoded, 0o644) == nil
}

// readProjectSummary is the summary a project's run handed back, or nil when it handed none.
func readProjectSummary(path string) *projectSummary {
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return nil
	}
	handed := &projectSummary{}
	if json.Unmarshal(contents, handed) != nil {
		return nil
	}
	handed.Summary.EngineBuilds = handed.EngineBuilds
	return handed
}

// combinedFooter is the one line a root holding several projects ends its default run on: their summaries summed,
// this run's time, and every project's gap with its name. facts carries what the root itself found: its unused
// ignore patterns, failed coverage queries and the engines it built before its projects ran.
func combinedFooter(runs []projectRun, facts overallFacts, total time.Duration, style textStyle) string {
	combined := runSummary{Total: total, EngineBuilds: facts.EngineBuilds}
	var named []string
	failed := false
	for _, checked := range runs {
		switch {
		case checked.runError != nil:
			failed = true
			named = append(named, "⚠ did not finish ("+checked.label+")")
			continue
		case checked.summary == nil && checked.exitCode != 0:
			failed = true
			named = append(named, style.red(fmt.Sprintf("☠️ exit %d (%s)", checked.exitCode, checked.label)))
			continue
		case checked.summary == nil:
			named = append(named, "⚠ no summary ("+checked.label+")")
			continue
		}
		summary := checked.summary.Summary
		combined.TypeErrors += summary.TypeErrors
		combined.Findings += summary.Findings
		combined.WarningFindings += summary.WarningFindings
		combined.WouldChange += summary.WouldChange
		combined.NestedWouldChange += summary.NestedWouldChange
		combined.WouldChangeConsidered += summary.WouldChangeConsidered
		combined.NestedWouldChangeConsidered += summary.NestedWouldChangeConsidered
		combined.Changed = append(combined.Changed, summary.Changed...)
		combined.Rules += summary.Rules
		combined.FilesInScope += summary.FilesInScope
		combined.FilesChecked += summary.FilesChecked
		combined.FilesCached += summary.FilesCached
		combined.FilesTypesOnly += summary.FilesTypesOnly
		combined.EngineBuilds = append(combined.EngineBuilds, summary.EngineBuilds...)
		if summary.failed() {
			failed = true
		} else if checked.exitCode != 0 {
			// A run whose summary passed and which exited nonzero failed on something the summary does not
			// count, and the line says so rather than reading green.
			failed = true
			named = append(named, style.red(fmt.Sprintf("☠️ exit %d (%s)", checked.exitCode, checked.label)))
		}
		if segment := summary.Adamic.segment(); segment != "" {
			named = append(named, segment+" ("+checked.label+")")
		}
		for _, marker := range uncheckedMarkers(summary) {
			named = append(named, marker+" ("+checked.label+")")
		}
	}
	if facts.UnusedIgnores > 0 {
		failed = true
		named = append(
			named,
			style.red(counted(facts.UnusedIgnores, "unused ignore pattern", "unused ignore patterns")),
		)
	}
	if facts.CoverageUnknown > 0 {
		named = append(named, "⚠ "+counted(facts.CoverageUnknown,
			"Swift coverage query failed, so some files may be counted twice",
			"Swift coverage queries failed, so some files may be counted twice"))
	}
	line := footer(combined, style, footerOptions{})
	if failed && !combined.failed() {
		// The verdict is the line's first mark, so a failure the counts do not hold still turns it.
		line = strings.Replace(line, "✓ 💎 ", "✗ ☠️ ", 1)
	}
	if len(named) > 0 {
		line += " • " + strings.Join(named, " • ")
	}
	return line
}
