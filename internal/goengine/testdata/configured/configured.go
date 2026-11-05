// Package configured is checked under a config that turns printf off and nilness down to a warning.
package configured

import "fmt"

// Report has a printf finding nobody hears, since printf is off, and a directive for it that is neither used
// nor unused.
func Report() {
	//cohere:ignore printf the wrong verb is the fixture's point
	_ = fmt.Sprintf("%d\n", "one")
	_ = fmt.Sprintf("%d\n", "two")
}

// Dereference reads through a pointer that is always nil, which nilness reports as a warning here.
func Dereference() int {
	var pointer *int
	return *pointer
}
