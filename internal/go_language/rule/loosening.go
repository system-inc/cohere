package rule

// Loosening is an option's flag value that knows, by its own type, whether it sets its rule looser than the house
// does. A project may loosen a house rule only by stating a departure with a reason in its settings, so the engine
// asks each option it sets whether it loosens, rather than keeping a list of which options and values do, which
// would drift from the rules it describes. An option whose value implements nothing here never loosens.
type Loosening interface {
	// Loosens is whether the value set is looser than the house's.
	Loosens() bool

	// HouseValue is the house's value as an option would spell it, so a refusal can name what a setting loosens.
	HouseValue() string
}
