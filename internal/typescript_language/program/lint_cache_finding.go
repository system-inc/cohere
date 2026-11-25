package program

// LintCacheFinding is one finding, flattened.
//
// rule.Diagnostic carries a *ast.SourceFile belonging to a program that no longer exists by
// the time this is read back, so the file is implied by the entry rather than stored per
// finding.
//
// MessageDescription holds the rendered text. An earlier version of this comment said text was
// "derived from the id when a finding is rendered" and therefore safe to omit. That was untrue,
// and the field was absent because of it. Nothing in this tree derives text from an id: there is
// no id-to-text table, and `printRuleDiagnostic` prints `Message.Description` directly. Measured
// 2026-08-25 over `internal/rules`: 340 message ids, of which 11 build their description with
// `fmt.Sprintf` from runtime values, so for those one id maps to many strings and no table could
// exist. A cached finding replaying only the id would print an empty or wrong sentence beside a
// correct file, range and rule: the right metadata carrying the wrong message, which is the one
// failure here that looks exactly like success.
//
// The text is stored rather than the 11 being refused because refusing them buys bytes with a
// standing per-rule obligation, and this format already depends on `ProgramReads` carrying one.
// The size scales the right way: the artifact grows with a dirty tree, which is the run where
// this cache saves least, because changed files are recomputed anyway.
//
// Message ids are not unique on their own, which is a second reason the id could never have been
// the key. `unexpected` carries five distinct descriptions across six core rules, colliding across
// rules and never within one, so the honest identity of a message is the pair (RuleName,
// MessageId). Lookups key on the pair even though the text is now stored, so nobody later assumes
// ids are unique.
//
// Fixes and suggestions are counted rather than stored, and leaving them out is sound: a fix is a
// replacement over a byte range, and replaying one computed against different bytes would corrupt
// the file it claims to repair. A cached finding is a report, never an edit.
type LintCacheFinding struct {
	RuleName string
	Start    int32
	End      int32

	// MessageId identifies the message within its rule. Not unique across rules; see above.
	MessageId string

	// MessageDescription is the rendered sentence, stored because nothing can rebuild it.
	MessageDescription string

	FixCount        int32
	SuggestionCount int32
}

// MessageKey is the honest identity of a message: the pair, never the id alone.
func (finding LintCacheFinding) MessageKey() (string, string) {
	return finding.RuleName, finding.MessageId
}
