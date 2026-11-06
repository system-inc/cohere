package differential

import "fmt"

// Finding is one report from one gate, reduced to the parts both gates can express.
//
// Column is deliberately absent from the key. The two gates locate a finding at different offsets
// within the same line often enough that keying on column would report a difference for every
// finding they agree about, drowning the real disagreements in noise. Line is the granularity a
// person acts on.
type Finding struct {
	File string
	Line int
	// Column is carried for the reader and never compared, for the reason stated above. It is the
	// difference between "go look at this" and "go find this", and a harness whose output cannot be
	// navigated to is a harness people stop running.
	Column  int
	Rule    string
	Message string
	Side    Side
}

// Key is the identity two findings must share to be called the same finding.
func (finding Finding) Key() string {
	return fmt.Sprintf("%s:%d:%s", finding.File, finding.Line, finding.Rule)
}
