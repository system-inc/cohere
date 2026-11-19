package go_engine

import "fmt"

// reachedBody is one body of a recorded package the build compiles, as the census judged it: where its declaration
// starts, by line and by offset in the file, its name, why it is reached, empty when it is not, what reached it first
// (bodyReacher.reason), and whether the graph's unit verdict calls it dependent.
type reachedBody struct {
	packageIdentifier string
	file              string
	line              int
	offset            int
	name              string
	reason            bodyReachReason
	driver            string
	// constantsOnlyReason is reason under the narrower load mode rule the general one replaced (loadModeReaches).
	constantsOnlyReason bodyReachReason
	unitDependent       bool
}

// String is the body as the census lists it: `package file:line name`.
func (body reachedBody) String() string {
	return body.packageIdentifier + " " + body.file + ":" + fmt.Sprint(body.line) + " " + body.name
}
