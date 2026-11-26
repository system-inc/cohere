package recorded

// Answers is an answers file as a recorder prints it: where the answers came from (the package and its
// version, where it was read from, what ran it), the date they were recorded, and one answer per input.
type Answers[Answer any] struct {
	Source   string   `json:"source"`
	Recorded string   `json:"recorded"`
	Answers  []Answer `json:"answers"`
}

// RecordedFor is what an answer records of its input, embedded in an answer's type: the input's name, and
// its text or, for a vendored input, the text's Digest in its place.
type RecordedFor struct {
	Name   string `json:"name"`
	Text   string `json:"text,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// Entry is the answer as Match pairs it: its name with the digest it records, or else its text.
func (recordedFor RecordedFor) Entry() Entry {
	if recordedFor.Sha256 != "" {
		return Entry{Name: recordedFor.Name, Input: recordedFor.Sha256}
	}
	return Entry{Name: recordedFor.Name, Input: recordedFor.Text}
}
