package return_nilness

import "fmt"

// ReturnedNilness marks a function with the nilness of each of its results, exported only when one says more than
// that it may be nil. It prints as staticcheck's nilnessFact does, so the two compare by what they say.
type ReturnedNilness struct {
	Results []ValueNilness
}

func (*ReturnedNilness) AFact() {
	// AFact only marks the type as a fact, so it does nothing.
}

func (fact *ReturnedNilness) String() string {
	return fmt.Sprintf("nilness: %v", fact.Results)
}
