package allowed

// Plain structs and an enum without methods carry no behavior to find, so they sit beside the one type that has some.
type Entry struct {
	amount int
}

type Account struct {
	name string
}

type Currency int

const (
	CurrencyDollar Currency = iota
	CurrencyEuro
)

type (
	Period struct{ days int }
	Tag    string
)

type Ledger struct {
	entries []Entry
}

func (ledger *Ledger) Add(entry Entry) {
	ledger.entries = append(ledger.entries, entry)
}
