// Acknowledged differences are the ones we decided on purpose.
//
// The harness exists to catch a port that drifted from the gate, so every difference it finds is a
// defect in one of the two until someone says otherwise. But the gate is software, and software has
// bugs: when a rule is ported to its stated semantics and the gate cannot see a case it claims to
// enforce, the two disagree and cohere is the one that is right.
//
// "Cohere and the gate agree" is the acceptance test for a faithful port, not the definition of a
// correct rule. Those are the same thing right up until the gate has a bug, and then they are
// opposites. Without somewhere to record that, the only ways forward are to reproduce the bug so the
// numbers match, or to let the harness sit red and lose the signal it exists to give.
package differential

import "strings"

// KnownGateDefects are the differences where cohere is right and the gate is wrong.
//
// Two directions. `SideCohere` is a true positive the gate cannot see. `SideGate` is a false positive
// the gate reports and cohere deliberately does not, where a rule was made to tell with the type
// checker rather than given an allowance (cohere's parity doctrine: never worse than ESLint, rule by
// rule, and differing only in its favour). Each gate-side entry names the rule document (`.md` beside
// the rule) that records the condition, and the fixture holding the site as a must-stay-silent case.
//
// Each one names a defect in the tool being replaced, so each is a reason the migration is worth
// doing rather than a cost of it. They are listed here, in source, rather than passed in at the
// command line: an acknowledgement that can be supplied per-run can be supplied by whoever wants a
// green result, and this list is reviewed like any other code.
//
// Empty since 2026-10-04 (#cn8sthd). Every one of the 23 entries it held had stopped naming a
// difference: the ESLint side now carries cohere's rulings through Nexus's twins and wrappers, the
// gate's own rule bugs were fixed in Nexus, and the rest of the sites were rewritten so neither engine
// reports them. Both engines read 0 on all 19 files the table named. An entry is added back when a
// real difference is decided, with its anchor and its reason.
var KnownGateDefects = []AcknowledgedDifference{}

// acknowledgementMatch is what one acknowledgement matched among a run's differences.
type acknowledgementMatch struct {
	acknowledgement AcknowledgedDifference
	// differenceIndexes are the positions in the difference list whose finding it names.
	differenceIndexes []int
}

// SourceLineReader returns the text of a line in a file of the tree being compared, 1-based, and
// false when the file or the line cannot be read.
type SourceLineReader func(file string, line int) (string, bool)

// matchAcknowledgements pairs each acknowledgement with the differences its anchor names.
//
// Only a difference on the acknowledgement's side, in its file and for its rule, can match, and only
// when its source line contains the anchor. Without a line reader nothing matches, which reports
// every acknowledgement stale: a comparison that cannot read the code cannot tell an anchor that
// moved from one that holds, and a stale report is the honest answer to that.
func matchAcknowledgements(
	acknowledged []AcknowledgedDifference,
	differences []Difference,
	readSourceLine SourceLineReader,
) []acknowledgementMatch {
	matches := make([]acknowledgementMatch, 0, len(acknowledged))
	for _, acknowledgement := range acknowledged {
		match := acknowledgementMatch{acknowledgement: acknowledgement}
		for index, difference := range differences {
			if difference.OnlyOn != acknowledgement.Side ||
				difference.Finding.File != acknowledgement.File ||
				difference.Finding.Rule != acknowledgement.Rule ||
				readSourceLine == nil {
				continue
			}
			text, readable := readSourceLine(difference.Finding.File, difference.Finding.Line)
			if readable && strings.Contains(strings.TrimSpace(text), acknowledgement.Anchor) {
				match.differenceIndexes = append(match.differenceIndexes, index)
			}
		}
		matches = append(matches, match)
	}
	return matches
}
