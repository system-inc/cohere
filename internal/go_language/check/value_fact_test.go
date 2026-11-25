package check

import (
	"fmt"
	"time"
)

// valueFact is a fact with a field of each kind factValueDigest walks, and two that gob leaves out, an unexported
// field and a func.
type valueFact struct {
	Count    int
	Names    []string
	Weights  map[string]float64
	Next     *valueFact
	Held     fmt.Stringer
	When     time.Time
	Callback func()
	hidden   int
}

// AFact makes valueFact an analysis.Fact.
func (*valueFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}
