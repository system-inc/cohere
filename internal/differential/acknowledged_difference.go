package differential

import "fmt"

// AcknowledgedDifference is one finding we expect only one side to report, and why.
//
// # Keyed to a file, a rule, a side and an anchor, never to a line number
//
// Keyed to the one finding rather than to the rule alone. A rule-wide excuse would also swallow the
// next genuine drift in that rule, which is the failure this whole package is built to prevent: an
// excuse broad enough to be convenient is an excuse broad enough to hide a defect.
//
// The entries were first keyed by line, and a line is the wrong key for a table that lives in another
// repository from the code it describes. Any edit above a site moves it, so by 2026-10-04 most of the
// table named lines that no longer held its finding (Map.tsx 747, 765 and 784, Button.tsx 222, the
// loop sites at 305, 548 and 689, a misused spread in a file since renamed), and every such entry went
// on excusing whatever happened to land on that line next (#cn8sthd). So an entry names text from its
// finding's own source line instead, the enclosing declaration's name when it sits there or the
// expression the rule reports, which moves with the code and survives edits elsewhere in the file.
type AcknowledgedDifference struct {
	File string
	Rule string
	// Side is which gate is expected to report it alone.
	Side Side
	// Anchor is text on the line the finding sits on, compared against that line with its
	// indentation trimmed. It must pick out exactly one of the side's findings for this rule in this
	// file: an entry matching none excuses nothing and is reported stale, and one matching several is
	// reported ambiguous and excuses none of them, since it would otherwise excuse more than its
	// reason was written about.
	Anchor string
	// Reason is why this difference is correct, in a sentence a reader can check.
	//
	// Required rather than optional. An acknowledgement with no reason is indistinguishable from a
	// suppression someone added to make a red build green, which is the shape the suppression note
	// in the main output already exists to count.
	Reason string
}

// Key names an acknowledgement in output, the four things that pick out its finding.
func (acknowledged AcknowledgedDifference) Key() string {
	return fmt.Sprintf("%s:%s:%s:%q", acknowledged.File, acknowledged.Rule, acknowledged.Side, acknowledged.Anchor)
}
