package contract

// RuleRecord is one rule, under `--rules` and `--rules-enabled`.
type RuleRecord struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
}

func (RuleRecord) kind() Kind { return KindRule }
