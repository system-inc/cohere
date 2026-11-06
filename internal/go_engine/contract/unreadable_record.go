package contract

// UnreadableRecord is a file the engine could not read, so nothing in it was checked. It goes between the
// project record and the first phase record, and it makes the run incomplete.
type UnreadableRecord struct {
	File string `json:"file"`
	// Error is a sentence.
	Error string `json:"error"`
}

func (UnreadableRecord) kind() Kind { return KindUnreadable }
