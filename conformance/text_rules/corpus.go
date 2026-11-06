package text_rules

import "fmt"

// Corpus is the loaded corpus.
type Corpus struct {
	// Families are sorted by name.
	Families []Family
	Ledger   []LedgerRow
}

// CasesFor is every case an engine compares: each with a form in its language, in a family it has a rule for,
// across every family, in order.
func (corpus *Corpus) CasesFor(engine Engine) []Case {
	cases := []Case{}
	for _, family := range corpus.Families {
		if !family.Rules[engine].Runs() {
			continue
		}
		for _, testCase := range family.Cases {
			if testCase.Has(engine) {
				cases = append(cases, testCase)
			}
		}
	}
	return cases
}

// RuleFor is an engine's entry for a family, and false for a family the corpus does not have.
func (corpus *Corpus) RuleFor(family string, engine Engine) (EngineRule, bool) {
	for _, candidate := range corpus.Families {
		if candidate.Name == family {
			entry, has := candidate.Rules[engine]
			return entry, has
		}
	}
	return EngineRule{}, false
}

// RowFor is the ledger's row for a case in an engine, if there is one.
func (corpus *Corpus) RowFor(engine Engine, testCase Case) (LedgerRow, bool) {
	for _, row := range corpus.Ledger {
		if row.Engine == engine && row.Family == testCase.Family && row.Case == testCase.Name {
			return row, true
		}
	}
	return LedgerRow{}, false
}

// Judge says whether an engine's result on a case is acceptable, given the ledger: nil for a pass with no
// row, or for a failure whose row is of the failure's kind; an error otherwise. A control whose count or
// start lines are wrong is an error whatever the ledger says.
func (corpus *Corpus) Judge(engine Engine, testCase Case, result Result) error {
	if testCase.Control && !result.ControlHeld {
		return fmt.Errorf("%s is its family's control, and %s does not report it as a control must, which no "+
			"ledger row can excuse: %s", testCase.Id(), engine, result.Detail)
	}
	row, ledgered := corpus.RowFor(engine, testCase)
	switch {
	case result.Passed() && ledgered:
		return fmt.Errorf("%s now passes in %s, so its %s row (task %s) is stale: delete it", testCase.Id(),
			engine, row.Kind, row.Task)
	case result.Passed():
		return nil
	case !ledgered:
		return fmt.Errorf("%s fails in %s with a %s failure and no ledger row: %s", testCase.Id(), engine,
			result.Failure, result.Detail)
	case row.Kind != result.Failure:
		return fmt.Errorf("%s fails in %s with a %s failure, and its ledger row (task %s) says %s: %s",
			testCase.Id(), engine, result.Failure, row.Task, row.Kind, result.Detail)
	}
	return nil
}
