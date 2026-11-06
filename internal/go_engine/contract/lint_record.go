package contract

// LintRecord summarizes the lint phase, for the lint line and its coverage notes.
type LintRecord struct {
	Findings            int `json:"findings"`
	RulesRun            int `json:"rulesRun"`
	FilesWalked         int `json:"filesWalked"`
	NodesVisited        int `json:"nodesVisited"`
	ElapsedMilliseconds int `json:"elapsedMilliseconds"`
	// ReusedFrom names the phase whose walk was reused, and is empty when lint walked on its own.
	ReusedFrom           string         `json:"reusedFrom"`
	RulesSilent          []string       `json:"rulesSilent"`
	RulesWatchedAndQuiet int            `json:"rulesWatchedAndQuiet"`
	Crashes              []Crash        `json:"crashes"`
	RulesScopedOff       map[string]int `json:"rulesScopedOff"`
	RulesNotConfigured   []string       `json:"rulesNotConfigured"`
	// ConfigNote is the line saying which defaults ran when no config was read, empty when one was.
	ConfigNote string `json:"configNote"`

	// WithheldBySuppression, WithheldAsGenerated and WithheldAsCopied are additive within contract 3, which the
	// front door ignores until it reads them, as it ignores any field the contract does not list. Each counts by
	// rule the findings a run found and did not write: those a //cohere:ignore comment withheld, those of a house
	// rule in a generated file, and those of a house rule in a file that declares itself a copy of upstream source.
	// Without them a run that hid everything reads like a run that found nothing. All are always written as
	// objects, `{}` when empty (see MarshalJSON).
	WithheldBySuppression map[string]int `json:"withheldBySuppression"`
	WithheldAsGenerated   map[string]int `json:"withheldAsGenerated"`
	WithheldAsCopied      map[string]int `json:"withheldAsCopied"`
}

func (LintRecord) kind() Kind { return KindLint }

// MarshalJSON writes the withheld counts as `{}` when there are none, never `null`, so a reader of the field
// never has to tell "none withheld" from "not counted".
func (record LintRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has LintRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler LintRecord
	counted := withoutMarshaler(record)
	if counted.WithheldBySuppression == nil {
		counted.WithheldBySuppression = map[string]int{}
	}
	if counted.WithheldAsGenerated == nil {
		counted.WithheldAsGenerated = map[string]int{}
	}
	if counted.WithheldAsCopied == nil {
		counted.WithheldAsCopied = map[string]int{}
	}
	return encodeWithoutHtmlEscaping(counted)
}
