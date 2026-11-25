package contract

// FixRecord is the fix and format phase's summary, which the front door prints as an edit.Summary.
type FixRecord struct {
	FilesConsidered     int            `json:"filesConsidered"`
	FilesRewritten      int            `json:"filesRewritten"`
	FixesApplied        int            `json:"fixesApplied"`
	FixesRefused        int            `json:"fixesRefused"`
	RefusalsByReason    map[string]int `json:"refusalsByReason"`
	FilesReformatted    int            `json:"filesReformatted"`
	FilesNotFormatted   int            `json:"filesNotFormatted"`
	NotFormattedReasons map[string]int `json:"notFormattedReasons"`
	FormatScope         string         `json:"formatScope"`
	// ChangedFiles is every file the phase wrote, once each; its length is FilesRewritten. It is always
	// written, empty when nothing was, because the front door reads an absent or null list as "not
	// listed" rather than "no files changed" (see MarshalJSON).
	ChangedFiles []ChangedFile `json:"changedFiles"`
	// NotWritten is every file the phase would have written and did not, since it is reached through a link to a
	// real path outside the run's root, sorted by file: each is a gap, its findings stand, and the run is
	// incomplete. Additive within contract 3, and always written, `[]` when there were none, as ChangedFiles is.
	NotWritten []NotWrittenFile `json:"notWritten"`
}

func (FixRecord) kind() Kind { return KindFix }

// MarshalJSON writes an empty ChangedFiles or NotWritten as `[]`, never `null`. The front door decodes each list
// into a pointer and reads null as "not listed", which would make a run that wrote nothing look like a run that
// did not say.
func (record FixRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has FixRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler FixRecord
	listed := withoutMarshaler(record)
	if listed.ChangedFiles == nil {
		listed.ChangedFiles = []ChangedFile{}
	}
	if listed.NotWritten == nil {
		listed.NotWritten = []NotWrittenFile{}
	}
	return encodeWithoutHtmlEscaping(listed)
}
