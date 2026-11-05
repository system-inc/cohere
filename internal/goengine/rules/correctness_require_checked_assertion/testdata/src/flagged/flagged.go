package flagged

import (
	"fmt"
	"io"
)

type node struct{ name string }

func (current *node) Name() string { return current.name }

// String makes *node a fmt.Stringer, so asserting a fmt.Stringer to *node is possible and compiles.
func (current *node) String() string { return current.name }

func use(value any) {}

// A single-value definition panics on a miss, the most common shape.
func definition(value any) {
	typed := value.(*node) // want `panics when the value is not a .\*node.`
	use(typed)
}

// So does a plain assignment, and a var declaration of one name.
func assignment(value any) {
	var typed *node
	typed = value.(*node)     // want `not a .\*node.`
	var counted = value.(int) // want `not a .int.`
	use(typed)
	use(counted)
}

// An assertion that is an argument, an operand, or a receiver of a call is single-valued.
func positions(value any) string {
	use(value.(string))          // want `not a .string.`
	fmt.Println(value.(int) + 1) // want `not a .int.`
	return value.(*node).Name()  // want `not a .\*node.`
}

// Two names on the left with two values on the right is no comma-ok: each value is read alone.
func paired(value any) {
	typed, other := value.(*node), 1 // want `not a .\*node.`
	use(typed)
	use(other)
}

// A comma-ok on the outer assertion does not check the one inside it.
func nested(value any) {
	typed, ok := value.(fmt.Stringer).(*node) // want `not a .fmt.Stringer.`
	use(typed)
	use(ok)
}

// Two assertions on one line are two findings.
func twice(left any, right any) bool {
	return left.(int) < right.(int) // want `not a .int.` `not a .int.`
}

// The static type implementing the target proves nothing, because a nil io.ReadCloser fails the assertion to
// io.Reader and panics like any other miss.
func implemented(reader io.ReadCloser) io.Reader {
	return reader.(io.Reader) // want `not a .io.Reader.`
}

// An assertion inside a type switch's case is still an assertion.
func insideSwitch(value any) {
	switch value.(type) {
	case int:
		use(value.(int)) // want `not a .int.`
	}
}

// A type parameter's value asserted through any panics on a miss like any other.
func generic[Element any](value any) Element {
	return value.(Element) // want `not a .Element.`
}

// A package-level declaration panics when the package initializes.
var initialized any = "text"

var asserted = initialized.(string) // want `not a .string.`

// A closure, a goroutine and a composite literal are no shelter.
func enclosed(value any) {
	go use(value.(int))                             // want `not a .int.`
	read := func() string { return value.(string) } // want `not a .string.`
	values := []int{value.(int)}                    // want `not a .int.`
	use(read)
	use(values)
}

// A comparison with the result is single-valued.
func compared(value any) bool {
	return value.(*node) != nil // want `not a .\*node.`
}
