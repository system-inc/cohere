package differential

// RuleAgreement is the verdict for one rule across the whole tree.
type RuleAgreement struct {
	Rule           string
	Shared         int
	OnlyCohere     int
	OnlyGate       int
	Classification Classification
}

// Agrees reports whether the two gates said the same thing about this rule everywhere.
func (agreement RuleAgreement) Agrees() bool {
	return agreement.OnlyCohere == 0 && agreement.OnlyGate == 0
}
