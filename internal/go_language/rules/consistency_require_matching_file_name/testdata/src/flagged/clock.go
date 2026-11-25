package flagged

// A //line directive naming the file ledger.go does not rename it: the file a reader opens is clock.go.

//line ledger.go:1
type Ledger struct { // want "`Ledger` is the one type with methods served in `clock.go`"
	total int
}

func (ledger *Ledger) Total() int {
	return ledger.total
}
