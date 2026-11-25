package rule

// OptionArity is how many option elements a rule's decoder reads, which is how the config layer learns what
// to hand it.
type OptionArity string

const (
	// OptionArityNone is a rule that takes no options: any option element the config writes is refused.
	OptionArityNone OptionArity = ""
	// OptionArityOne is a rule that reads one option element (Registration.Decode).
	OptionArityOne OptionArity = "One"
	// OptionArityList is a rule that reads every element as one JSON array (Registration.DecodeOptionList).
	OptionArityList OptionArity = "List"
	// OptionArityAt is a rule that reads one element and where the config was written
	// (Registration.DecodeAt).
	OptionArityAt OptionArity = "At"
)
