package react_conformance

import (
	"fmt"
	"sort"
	"strings"
)

// Report is the whole aggregated run.
type Report struct {
	Considered int
	Counts     map[Verdict]int
	Fixtures   []FixtureVerdict
}

// Check verifies the categories partition the corpus.
//
// Same guard as `Score.Check`, and for the same reason: the arithmetic is the only thing that
// notices a fixture counted twice or not at all.
func (report Report) Check() error {
	sum := 0
	for _, verdict := range AllVerdicts {
		sum += report.Counts[verdict]
	}
	if sum != report.Considered {
		return fmt.Errorf(
			"report does not partition the corpus: considered %d but categories sum to %d",
			report.Considered,
			sum,
		)
	}
	if report.Considered != len(report.Fixtures) {
		return fmt.Errorf("considered %d fixtures but recorded %d verdicts", report.Considered, len(report.Fixtures))
	}
	for _, fixtureVerdict := range report.Fixtures {
		if fixtureVerdict.Verdict != VerdictPassed && fixtureVerdict.Reason == "" {
			return fmt.Errorf(
				"%s: %s with no reason; an unreasoned category is somewhere to hide a failure",
				fixtureVerdict.Name,
				fixtureVerdict.Verdict,
			)
		}
	}
	return nil
}

// Summary is the scoreboard, one line per category.
//
// It leads with the number and its denominator and then names every category, because the headline
// alone is the thing this whole file exists to stop anyone quoting.
func (report Report) Summary() string {
	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"%d / %d passed against facebook/react@%s\n",
		report.Counts[VerdictPassed],
		report.Considered,
		UpstreamSha[:12],
	)
	for _, verdict := range AllVerdicts {
		fmt.Fprintf(&builder, "  %-30s %4d\n", verdict, report.Counts[verdict])
	}
	return builder.String()
}

// ByRule breaks the report down by the upstream rule each fixture belongs to.
func (report Report) ByRule() map[string]map[Verdict]int {
	breakdown := map[string]map[Verdict]int{}
	for _, fixtureVerdict := range report.Fixtures {
		for _, ruleName := range fixtureVerdict.Rules {
			if breakdown[ruleName] == nil {
				breakdown[ruleName] = map[Verdict]int{}
			}
			breakdown[ruleName][fixtureVerdict.Verdict]++
		}
	}
	return breakdown
}

// RuleNames returns the rules in the breakdown, sorted.
func (report Report) RuleNames() []string {
	names := map[string]bool{}
	for _, fixtureVerdict := range report.Fixtures {
		for _, ruleName := range fixtureVerdict.Rules {
			names[ruleName] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	return sorted
}
