// Acknowledged differences are the ones we decided on purpose.
//
// The harness exists to catch a port that drifted from the gate, so every difference it finds is a
// defect in one of the two until someone says otherwise. But the gate is software, and software has
// bugs: when a rule is ported to its stated semantics and the gate cannot see a case it claims to
// enforce, the two disagree and verify is the one that is right.
//
// "Verify and the gate agree" is the acceptance test for a faithful port, not the definition of a
// correct rule. Those are the same thing right up until the gate has a bug, and then they are
// opposites. Without somewhere to record that, the only ways forward are to reproduce the bug so the
// numbers match, or to let the harness sit red and lose the signal it exists to give.
package differential

import "fmt"

// AcknowledgedDifference is one finding we expect only one side to report, and why.
//
// Keyed to file, line, and rule rather than to the rule alone. A rule-wide excuse would also swallow
// the next genuine drift in that rule, which is the failure this whole package is built to prevent:
// an excuse broad enough to be convenient is an excuse broad enough to hide a defect.
type AcknowledgedDifference struct {
	File string
	Line int
	Rule string
	// Side is which gate is expected to report it alone.
	Side Side
	// Reason is why this difference is correct, in a sentence a reader can check.
	//
	// Required rather than optional. An acknowledgement with no reason is indistinguishable from a
	// suppression someone added to make a red build green, which is the shape the suppression note
	// in the main output already exists to count.
	Reason string
}

// Key is the identity an acknowledgement shares with the finding it excuses.
func (acknowledged AcknowledgedDifference) Key() string {
	return fmt.Sprintf("%s:%d:%s:%s", acknowledged.File, acknowledged.Line, acknowledged.Rule, acknowledged.Side)
}

// KnownGateDefects are the differences where verify is right and the gate cannot see the case.
//
// Each one names a defect in the tool being replaced, so each is a reason the migration is worth
// doing rather than a cost of it. They are listed here, in source, rather than passed in at the
// command line: an acknowledgement that can be supplied per-run can be supplied by whoever wants a
// green result, and this list is reviewed like any other code.
var KnownGateDefects = []AcknowledgedDifference{
	{
		File: "libraries/structure/source/services/network/NetworkService.ts",
		Line: 228,
		Rule: "storage-no-direct-local-storage",
		Side: SideVerify,
		Reason: "the gate's rule matches window.localStorage only as the object of an outer member " +
			"expression, so it sees window.localStorage.getItem(...) and never window.localStorage " +
			"passed as a value; this line is the latter and is a real violation of the rule's " +
			"stated intent",
	},
}

// acknowledgedIndex is the lookup the comparison uses, built once per run.
func acknowledgedIndex(acknowledged []AcknowledgedDifference) map[string]AcknowledgedDifference {
	index := make(map[string]AcknowledgedDifference, len(acknowledged))
	for _, one := range acknowledged {
		index[one.Key()] = one
	}
	return index
}
