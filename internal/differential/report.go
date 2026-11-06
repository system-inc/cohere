package differential

// Report is a whole comparison: what differed, per rule, over what population.
type Report struct {
	CoherePopulation Population
	GatePopulation   Population
	Differences      []Difference
	Agreements       []RuleAgreement
	// Comparable is false when a side reported no coverage, which makes the diff meaningless
	// regardless of how it looks. An empty Differences with Comparable false is the vacuous case.
	Comparable bool
	// NotComparableReason states which side had nothing and is empty when Comparable is true.
	NotComparableReason string
	// ComparedRules and ConfiguredRules are how many rules the comparison could speak about and
	// how many the config enables. When they differ, the verdict covers a subset of the gate and
	// must say so.
	ComparedRules   int
	ConfiguredRules int
	// StaleAcknowledgements match no current difference, and AmbiguousAcknowledgements match more
	// than one. Either fails the run: a stale entry is an excuse waiting for whatever lands on its
	// site next, and an ambiguous one would excuse more than its reason was written about, so the
	// table is out of date until someone removes or re-anchors them.
	StaleAcknowledgements     []AcknowledgedDifference
	AmbiguousAcknowledgements []AcknowledgedDifference
	// Provenance is what actually ran, and whether the harness was ever shown able to detect a
	// difference at all.
	//
	// This does not overlap Comparable and neither substitutes for the other, which is the reason
	// both are here. Comparable answers "did both sides report coverage": a property of this run's
	// population. Provenance answers "has this instrument been demonstrated to work": a property
	// of the instrument, which a healthy population says nothing about. A run over 3,407 files by a
	// harness that parses one side's format wrong is perfectly Comparable and completely blind.
	Provenance Provenance
}

// Agreed reports whether every rule active on both sides agreed.
//
// Rules the migration has not reached yet do not count against agreement, because a rule cohere
// has never claimed to implement cannot disagree with anything. Only ClassificationBothActive does.
// Both guards are asked before the findings are, and in this order, because each one describes a
// way the finding list can be empty for a reason that has nothing to do with the code being clean.
func (report Report) Agreed() bool {
	if !report.Comparable {
		return false
	}
	// A run whose population is real but whose controls never fired has not been shown able to
	// report a difference, so its silence is not evidence. This is the guard that four vacuous
	// probes in one night got past: every one of them had a plausible population.
	if trustworthy, _ := report.Provenance.Trustworthy(); !trustworthy {
		return false
	}
	if len(report.StaleAcknowledgements) > 0 || len(report.AmbiguousAcknowledgements) > 0 {
		return false
	}
	// Observed only. A planted control is a difference this run caused on purpose, and counting it
	// against agreement would mean the harness could never agree with itself: proving detection
	// requires creating a difference, so a control that classified both-active would fail the very
	// run that demonstrated the instrument works.
	for _, difference := range report.ObservedDifferences() {
		if difference.Classification == ClassificationBothActive {
			return false
		}
	}
	return true
}

// ObservedDifferences returns the differences that are about the codebase rather than about the
// harness, which is the number a reader means when they ask how far apart the two gates are.
func (report Report) ObservedDifferences() []Difference {
	observed := make([]Difference, 0, len(report.Differences))
	for _, difference := range report.Differences {
		if !difference.Planted {
			observed = append(observed, difference)
		}
	}
	return observed
}
