package allowed

// A file of plain types, an interface and functions serves no type with methods, so it has no name to match.
type Settings struct {
	verbose bool
}

type Shape interface {
	Area() float64
}

type Currency int

const (
	CurrencyDollar Currency = iota
	CurrencyEuro
)

func DefaultSettings() Settings {
	return Settings{}
}
