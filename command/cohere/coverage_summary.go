package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/types/program"
)

// coverageSummary is everything the coverage block says, built by each engine from its own records
// and printed by one writer, so a Swift run and a TypeScript run say the same thing about the same
// fact.
type coverageSummary struct {
	Entries []ruleCoverageEntry
	// Unnamed counts rules a category holds that the engine's record does not name: the Swift record
	// counts its quiet rules and names none. They count toward the total and print as a count.
	Unnamed map[coverageCategory]int
	// PartialFacts are facts about rules the record counts and does not name, said once by name under
	// `--coverage`: a Swift rule counted as quiet that the record also names as off for some files.
	PartialFacts []string
	// Crashes is each file a rule could not finish, as the run names it in every view, so the coverage notes
	// and the human view say the same words of it.
	Crashes      []runCrash
	RuleCrashes  []coverageRuleCrash
	FilesIgnored int
	Suppression  *suppressionCoverage
	// Notes is what rules counted rather than reported (rule.Context.Note), summed across files: rule
	// name to note key to count. Listed under `--coverage`, so an exemption reads as a number.
	Notes map[string]map[string]int
}

// count is how many rules a category holds, named or not.
func (summary coverageSummary) count(category coverageCategory) int {
	total := summary.Unnamed[category]
	for _, entry := range summary.Entries {
		if entry.Category == category {
			total++
		}
	}
	return total
}

// total is every rule the summary accounts for. It is the sum of the categories by construction, and
// a test pins that the counted line says so.
func (summary coverageSummary) total() int {
	total := len(summary.Entries)
	for _, count := range summary.Unnamed {
		total += count
	}
	return total
}

// sortEntries orders the entries by category, then by name, so the details read in the counted
// line's order and two runs can be diffed.
func (summary *coverageSummary) sortEntries() {
	position := make(map[coverageCategory]int, len(coverageCategoryOrder))
	for index, category := range coverageCategoryOrder {
		position[category] = index
	}
	sort.SliceStable(summary.Entries, func(first, second int) bool {
		if summary.Entries[first].Category != summary.Entries[second].Category {
			return position[summary.Entries[first].Category] < position[summary.Entries[second].Category]
		}
		return summary.Entries[first].Name < summary.Entries[second].Name
	})
}

// coverageCountedLine is the line whose terms add up to the rule total, said as arithmetic so a reader
// can check it: `coverage: 459 rules = 370 ran and found nothing + 14 registered no listener + ...`.
// A category holding no rule is left out, and the sum still holds.
func (summary coverageSummary) coverageCountedLine() string {
	terms := []string{}
	for _, category := range coverageCategoryOrder {
		if count := summary.count(category); count > 0 {
			terms = append(terms, fmt.Sprintf("%d %s", count, coverageCategoryTerms[category]))
		}
	}
	line := fmt.Sprintf("coverage: %d rules", summary.total())
	if len(terms) > 0 {
		line += " = " + strings.Join(terms, " + ")
	}
	return line
}

// coverageFilesLine is the second line: what happened to files and to findings rather than to rules,
// so none of it is part of the rule arithmetic.
//
// The crash count prints when it is zero, because "0 files crashed" is the claim a clean run makes and
// a reader should be able to see it made. The reasonless count prints every run rather than being
// enforced: measured across the codebase cohere gates, 281 of 306 suppressions of our own rules stated
// no reason, so requiring one would turn working code red for no defect, and printing it is what lets
// the convention be tightened later from evidence rather than from a guess.
func (summary coverageSummary) coverageFilesLine(details bool) string {
	terms := []string{fmt.Sprintf("%d files crashed", len(summary.Crashes))}
	if len(summary.RuleCrashes) > 0 {
		terms = append(terms, fmt.Sprintf("%d rule crashes, each costing one rule one file", len(summary.RuleCrashes)))
	}
	if summary.FilesIgnored > 0 {
		terms = append(terms, fmt.Sprintf("%d files excluded by ignorePatterns", summary.FilesIgnored))
	}
	if summary.Suppression != nil {
		terms = append(terms, fmt.Sprintf("%d findings suppressed, %d without a reason",
			summary.Suppression.Suppressed, summary.Suppression.SuppressedWithoutReason))
		// Keep the coverage count beside the walk's fixable unused-disable findings. The walk only
		// reports names whose rules ran, so a filtered run preserves unselected names.
		if summary.Suppression.Dead > 0 {
			terms = append(
				terms,
				fmt.Sprintf("%d disable comments silenced nothing while their rule ran", summary.Suppression.Dead),
			)
		}
	}
	if !details {
		terms = append(terms, "details: cohere --coverage")
	}
	return "  " + strings.Join(terms, " · ")
}

// excuseRulesThatSkippedEveryFile clears the dead-rule signal from a rule whose skips cover every file it
// was offered. A skip is a reason given at run time, as NoListener is one given at registration, and the
// skipped line already names it, so the same rule must not also read as having checked nothing for no
// reason: nexus/correctness-no-implicit-return stands down in every project that sets noImplicitReturns,
// and printed both (#yj96emr). A rule that skipped some files and registered nothing on the rest still
// needs action, since nothing explains the rest.
func (summary *coverageSummary) excuseRulesThatSkippedEveryFile(coverage program.Coverage) {
	skips, _ := splitRuleSkips(summary.Notes)
	for index := range summary.Entries {
		if !summary.Entries[index].NeedsAction {
			continue
		}
		skipped := 0
		for _, count := range skips[summary.Entries[index].Name] {
			skipped += count
		}
		if skipped >= coverage.RulesOffered[summary.Entries[index].Name] {
			summary.Entries[index].NeedsAction = false
			summary.Entries[index].Details = append(summary.Entries[index].Details, "skipped every file it was offered")
		}
	}
}

// offWithoutReasonCount is how many rules the settings turn off with no reason given.
func (summary coverageSummary) offWithoutReasonCount() int {
	count := 0
	for _, entry := range summary.Entries {
		if entry.OffWithoutReason {
			count++
		}
	}
	return count
}
