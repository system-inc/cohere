// Package suppressed holds one directive used, one unused and one without a reason.
package suppressed

import "fmt"

// Used withholds the printf finding on the line below the directive.
func Used() {
	//cohere:ignore printf the wrong verb is the fixture's point
	_ = fmt.Sprintf("%d\n", "one")
}

// Unused names printf on a line with nothing for it to withhold.
func Unused() {
	//cohere:ignore printf nothing below is wrong
	_ = fmt.Sprintln("two")
}

// Reasonless gives no reason, so it withholds nothing and is a finding of its own.
func Reasonless() {
	//cohere:ignore printf
	_ = fmt.Sprintf("%d\n", "three")
}
