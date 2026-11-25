package contract

// FindingRecord is one finding. Lines and columns are 1-based, and columns count UTF-8 bytes.
type FindingRecord struct {
	Source Source `json:"source"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	// EndLine and EndColumn are exclusive and optional. Zero is never a 1-based position, so zero means
	// not known and is left out.
	EndLine   int      `json:"endLine,omitempty"`
	EndColumn int      `json:"endColumn,omitempty"`
	Severity  Severity `json:"severity"`
	Rule      string   `json:"rule"`
	MessageId string   `json:"messageId"`
	Message   string   `json:"message"`
	// Fixes are what a `--no-fix` run withheld, never instructions for the front door.
	Fixes []Edit `json:"fixes"`
	// Suggestions are repairs offered and never applied.
	Suggestions []Suggestion `json:"suggestions"`
}

func (FindingRecord) kind() Kind { return KindFinding }
