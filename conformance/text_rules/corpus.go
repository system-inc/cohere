package text_rules

import "fmt"

// Corpus is the loaded corpus.
type Corpus struct {
	// Families are sorted by name.
	Families []Family
	Ledger   []LedgerRow
}

// CasesFor is every case a language compares: each with a form in its language, in a family it has a rule for,
// across every family, in order.
func (corpus *Corpus) CasesFor(language Language) []Case {
	cases := []Case{}
	for _, family := range corpus.Families {
		if !family.Rules[language].Runs() {
			continue
		}
		for _, testCase := range family.Cases {
			if testCase.Has(language) {
				cases = append(cases, testCase)
			}
		}
	}
	return cases
}

// RuleFor is a language's entry for a family, and false for a family the corpus does not have.
func (corpus *Corpus) RuleFor(family string, language Language) (LanguageRule, bool) {
	for _, candidate := range corpus.Families {
		if candidate.Name == family {
			entry, has := candidate.Rules[language]
			return entry, has
		}
	}
	return LanguageRule{}, false
}

// RowFor is the ledger's row for a case in a language, if there is one.
func (corpus *Corpus) RowFor(language Language, testCase Case) (LedgerRow, bool) {
	for _, row := range corpus.Ledger {
		if row.Language == language && row.Family == testCase.Family && row.Case == testCase.Name {
			return row, true
		}
	}
	return LedgerRow{}, false
}

// Judge says whether a language's result on a case is acceptable, given the ledger: nil for a pass with no
// row, or for a failure whose row is of the failure's kind; an error otherwise. A control whose count or
// start lines are wrong is an error whatever the ledger says.
func (corpus *Corpus) Judge(language Language, testCase Case, result Result) error {
	if testCase.Control && !result.ControlHeld {
		return fmt.Errorf("%s is its family's control, and %s does not report it as a control must, which no "+
			"ledger row can excuse: %s", testCase.Id(), language, result.Detail)
	}
	row, ledgered := corpus.RowFor(language, testCase)
	switch {
	case result.Passed() && ledgered:
		return fmt.Errorf("%s now passes in %s, so its %s row (task %s) is stale: delete it", testCase.Id(),
			language, row.Kind, row.Task)
	case result.Passed():
		return nil
	case !ledgered:
		return fmt.Errorf("%s fails in %s with a %s failure and no ledger row: %s", testCase.Id(), language,
			result.Failure, result.Detail)
	case row.Kind != result.Failure:
		return fmt.Errorf("%s fails in %s with a %s failure, and its ledger row (task %s) says %s: %s",
			testCase.Id(), language, result.Failure, row.Task, row.Kind, result.Detail)
	}
	return nil
}
