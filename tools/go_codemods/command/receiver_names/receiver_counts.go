package main

// receiverCounts tallies the receivers of editable files.
type receiverCounts struct {
	seen           int
	unnamedOrBlank int
	notEligible    int
	eligible       int
}

func (counts *receiverCounts) add(other receiverCounts) {
	counts.seen += other.seen
	counts.unnamedOrBlank += other.unnamedOrBlank
	counts.notEligible += other.notEligible
	counts.eligible += other.eligible
}
