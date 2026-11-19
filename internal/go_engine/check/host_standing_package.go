package check

import (
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// hostStandingPackage is one package's view of a HostStanding, the rule.StandingBodies its passes hold: every answer is
// for the package identified by packageIdentifier alone.
type hostStandingPackage struct {
	standing          *HostStanding
	packageIdentifier string
}

// Stands is whether the function declared at offset of filename stands in this package, as the build's judgment
// answers for it (rule.StandingBodies).
func (view *hostStandingPackage) Stands(filename string, offset int) bool {
	return view.standing.stands(view.packageIdentifier, filename, offset)
}

// Confines is whether the rule named ruleName is confined: the run enables it and it is among those named confined
// (rule.StandingBodies).
func (view *hostStandingPackage) Confines(ruleName string) bool {
	_, confined := view.standing.analyzerOf[ruleName]
	return confined
}

// Findings is the host's findings of the rule named ruleName on filename from the host's entry of this same package,
// every one, in the entry's order, before any dedupe or withholding (rule.StandingBodies). It is false, so the rule
// judges the whole file itself, when the rule is not confined, when the host's package did not compile the file, and
// when the entry does not decode or holds no analysis of the rule, as where it crashed or never ran
// (HostRun.analysisOf). Another holder of the file is never read in this package's place. A finding that edits another
// file is handed on like any other, since whether it places is the reader's to decide (rule.StandingFiles).
func (view *hostStandingPackage) Findings(ruleName string, filename string) ([]rule.InheritedFinding, bool) {
	analyzer, confined := view.standing.analyzerOf[ruleName]
	if !confined {
		return nil, false
	}
	// An entry that never compiled the file holds no finding on it, which would otherwise read as a rule that found
	// nothing there: a file of this build alone, or one a build names otherwise, as cgo's output is named for each run.
	if !slices.Contains(view.standing.host.holdersOf(filename), view.packageIdentifier) {
		return nil, false
	}
	analyzed, holds := view.standing.host.analysisOf(view.packageIdentifier, analyzer)
	if !holds {
		return nil, false
	}
	findings := []rule.InheritedFinding{}
	for _, finding := range analyzed.findings {
		if finding.File == filename {
			findings = append(findings, inheritedFinding(finding))
		}
	}
	return findings, true
}

// Took counts, as the build's take, the declarations that stood in filename for the rule named ruleName in this
// package and the findings of the host's it reported in them (rule.StandingBodies). A file the rule takes again, in
// another shard of the package or another load, is counted once.
func (view *hostStandingPackage) Took(ruleName string, filename string, declarations int, findings int) {
	view.standing.took(view.packageIdentifier, ruleName, filename, declarations, findings)
}
