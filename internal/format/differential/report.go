package differential

import (
	"fmt"
	"sort"
	"strings"
)

// Report is one corpus measured.
type Report struct {
	// Root is the corpus that was walked. A percentage means nothing without the root it was taken
	// against, which is how a measurement silently covers half a tree.
	Root string

	ByLanguage map[string]*Tally
	Files      []FileResult
}

// Total sums every language.
func (report Report) Total() Tally {
	total := Tally{}
	for _, tally := range report.ByLanguage {
		total.Identical += tally.Identical
		total.Different += tally.Different
		total.CandidateRefused += tally.CandidateRefused
		total.OracleFailed += tally.OracleFailed
		total.RewriteIdentical += tally.RewriteIdentical
		total.RewriteTotal += tally.RewriteTotal
	}
	return total
}

// Summary renders a report for a terminal: per language, then the total, then failures by name.
func (report Report) Summary() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "corpus %s\n", report.Root)

	languages := make([]string, 0, len(report.ByLanguage))
	for language := range report.ByLanguage {
		languages = append(languages, language)
	}
	sort.Strings(languages)

	row := func(name string, tally Tally) {
		fmt.Fprintf(
			&builder,
			"  %-8s %6.2f%%  %5d/%-5d identical  rewrites %6.2f%% (%d/%d)  refused %d  oracle failed %d\n",
			name,
			tally.Percent(),
			tally.Identical,
			tally.Measured(),
			tally.RewritePercent(),
			tally.RewriteIdentical,
			tally.RewriteTotal,
			tally.CandidateRefused,
			tally.OracleFailed,
		)
	}
	for _, language := range languages {
		row(language, *report.ByLanguage[language])
	}
	row("total", report.Total())

	for _, result := range report.Files {
		if result.Outcome == OracleFailed {
			fmt.Fprintf(&builder, "  oracle failed: %s: %s\n", result.Path, firstLine(result.Detail))
		}
	}
	return builder.String()
}
