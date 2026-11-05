package allowed

// A //line directive naming another file does not rename this one, which is named for the type it serves.

//line elsewhere.go:1
type Ledger struct {
	total int
}

// Entry has no methods and never counts.
type Entry struct {
	amount int
}

func (ledger *Ledger) Add(entry Entry) {
	ledger.total += entry.amount
}
