package differential

import "testing"

// An acknowledged difference is excused, and only at the exact place it was acknowledged.
func TestAnAcknowledgedDifferenceIsClassifiedAcknowledged(t *testing.T) {
	known := []AcknowledgedDifference{{
		File: "app/Probe.tsx", Line: 12, Rule: "storage-no-direct-local-storage",
		Side: SideVerify, Reason: "the gate cannot see this shape",
	}}
	index := acknowledgedIndex(known)
	inputs := Inputs{
		VerifyRules:     map[string]bool{"storage-no-direct-local-storage": true},
		ConfiguredRules: map[string]bool{"storage-no-direct-local-storage": true},
		Acknowledged:    known,
	}

	exact := Finding{File: "app/Probe.tsx", Line: 12, Rule: "storage-no-direct-local-storage"}
	if got := classifyFinding(exact, SideVerify, inputs, index); got != ClassificationAcknowledged {
		t.Errorf("the acknowledged finding was classified %q rather than acknowledged", got)
	}
}

// The other direction, and it is the one that matters: an acknowledgement must not excuse anything
// else in the same rule, file, or line.
//
// Without this, an index that matched on rule alone would pass the test above while silently
// excusing every future drift in that rule, which is the failure this whole package exists to
// prevent. The excuse has to be exactly as narrow as the reason written beside it.
func TestAnAcknowledgementExcusesNothingElse(t *testing.T) {
	known := []AcknowledgedDifference{{
		File: "app/Probe.tsx", Line: 12, Rule: "storage-no-direct-local-storage",
		Side: SideVerify, Reason: "the gate cannot see this shape",
	}}
	index := acknowledgedIndex(known)
	inputs := Inputs{
		VerifyRules:     map[string]bool{"storage-no-direct-local-storage": true},
		ConfiguredRules: map[string]bool{"storage-no-direct-local-storage": true},
		Acknowledged:    known,
	}

	for _, testCase := range []struct {
		name    string
		finding Finding
		side    Side
	}{
		{"a different line in the same file", Finding{File: "app/Probe.tsx", Line: 13, Rule: "storage-no-direct-local-storage"}, SideVerify},
		{"the same line in a different file", Finding{File: "app/Other.tsx", Line: 12, Rule: "storage-no-direct-local-storage"}, SideVerify},
		{"a different rule at the same place", Finding{File: "app/Probe.tsx", Line: 12, Rule: "no-self-assign"}, SideVerify},
		{"the same finding from the other side", Finding{File: "app/Probe.tsx", Line: 12, Rule: "storage-no-direct-local-storage"}, SideGate},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := classifyFinding(testCase.finding, testCase.side, inputs, index); got == ClassificationAcknowledged {
				t.Error("an unrelated difference was excused by the acknowledgement")
			}
		})
	}
}

// Every acknowledgement states a reason, because one without a reason is indistinguishable from a
// suppression added to turn a red run green.
func TestEveryKnownGateDefectStatesAReason(t *testing.T) {
	if len(KnownGateDefects) == 0 {
		t.Skip("no acknowledgements are recorded, so this proves nothing")
	}
	for _, defect := range KnownGateDefects {
		if defect.Reason == "" {
			t.Errorf("%s has no reason recorded", defect.Key())
		}
		if defect.File == "" || defect.Rule == "" || defect.Line == 0 {
			t.Errorf("%s is not keyed to an exact finding, so it would excuse more than one", defect.Key())
		}
	}
}
