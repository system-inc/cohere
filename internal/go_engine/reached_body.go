package go_engine

import "fmt"

// reachedBody is one body of a recorded package the build compiles, as the census judged it: where it starts, its
// name, why it is reached, empty when it is not, and whether the graph's unit verdict calls it dependent.
type reachedBody struct {
	packageIdentifier string
	file              string
	line              int
	name              string
	reason            bodyReachReason
	unitDependent     bool
}

// String is the body as the census lists it: `package file:line name`.
func (body reachedBody) String() string {
	return body.packageIdentifier + " " + body.file + ":" + fmt.Sprint(body.line) + " " + body.name
}
