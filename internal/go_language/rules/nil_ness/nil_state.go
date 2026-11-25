package nil_ness

// nilState is what is known of a value: nil, not nil, or unknown.
type nilState int

const (
	isNonNil nilState = -1
	unknown  nilState = 0
	isNil    nilState = 1
)

// nilnessStrings is how a message names each nilState, indexed by the state plus one.
var nilnessStrings = []string{"non-nil", "unknown", "nil"}

// String is how a message names state.
func (state nilState) String() string { return nilnessStrings[state+1] }
