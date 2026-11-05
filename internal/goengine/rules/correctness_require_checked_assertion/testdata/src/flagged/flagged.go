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
	typed := value.(*node) // want `panics when the value is not of type .\*node.`
	use(typed)
}

// So does a plain assignment, and a var declaration of one name.
func assignment(value any) {
	var typed *node
	typed = value.(*node)     // want `not of type .\*node.`
	var counted = value.(int) // want `not of type .int.`
	use(typed)
	use(counted)
}

// An assertion that is an argument, an operand, or a receiver of a call is single-valued.
func positions(value any) string {
	use(value.(string))          // want `not of type .string.`
	fmt.Println(value.(int) + 1) // want `not of type .int.`
	return value.(*node).Name()  // want `not of type .\*node.`
}

// Two names on the left with two values on the right is no comma-ok: each value is read alone.
func paired(value any) {
	typed, other := value.(*node), 1 // want `not of type .\*node.`
	use(typed)
	use(other)
}

// A comma-ok on the outer assertion does not check the one inside it.
func nested(value any) {
	typed, ok := value.(fmt.Stringer).(*node) // want `not of type .fmt.Stringer.`
	use(typed)
	use(ok)
}

// Two assertions on one line are two findings.
func twice(left any, right any) bool {
	return left.(int) < right.(int) // want `not of type .int.` `not of type .int.`
}

// The static type implementing the target proves nothing, because a nil io.ReadCloser fails the assertion to
// io.Reader and panics like any other miss.
func implemented(reader io.ReadCloser) io.Reader {
	return reader.(io.Reader) // want `not of type .io.Reader.`
}

// An assertion of the switch's own subject to the one type its case matched cannot miss, and is still flagged,
// with the message that asks for the switch's bound value instead of the one about panicking.
func matchingCase(value any) {
	switch value.(type) {
	case int:
		use(value.(int)) // want `already matched .value. as type .int.`
	case *node:
		if value.(*node) != nil { // want `already matched .value. as type .\*node.`
			use(value)
		}
	}
}

// A bound switch matches its subject the same way, and so does a case further out than the nearest one.
func matchingCaseBound(value any, other any) {
	switch typed := value.(type) {
	case string:
		use(typed)
		switch other.(type) {
		case int:
			use(value.(string)) // want `already matched .value. as type .string.`
		}
	}
}

// A different type in the case can miss, and so panics like any other.
func differentType(value any) {
	switch value.(type) {
	case int:
		use(value.(string)) // want `panics when the value is not of type .string.`
	}
}

// A case listing two types leaves the value as either, so asserting one of them can miss.
func multipleTypes(value any) {
	switch value.(type) {
	case int, string:
		use(value.(int)) // want `panics when the value is not of type .int.`
	}
}

// A different variable is not what the switch matched.
func differentVariable(value any, other any) {
	switch value.(type) {
	case int:
		use(other.(int)) // want `panics when the value is not of type .int.`
	}
}

// A closure inside the case may run after the switch, so its assertion gets the ordinary message.
func closureInCase(value any) {
	switch value.(type) {
	case int:
		read := func() int { return value.(int) } // want `panics when the value is not of type .int.`
		use(read)
	}
}

// The default case matched no one type.
func defaultCase(value any) {
	switch value.(type) {
	case int:
	default:
		use(value.(int)) // want `panics when the value is not of type .int.`
	}
}

// A type parameter's value asserted through any panics on a miss like any other.
func generic[Element any](value any) Element {
	return value.(Element) // want `not of type .Element.`
}

// A package-level declaration panics when the package initializes.
var initialized any = "text"

var asserted = initialized.(string) // want `not of type .string.`

// A closure, a goroutine and a composite literal are no shelter.
func enclosed(value any) {
	go use(value.(int))                             // want `not of type .int.`
	read := func() string { return value.(string) } // want `not of type .string.`
	values := []int{value.(int)}                    // want `not of type .int.`
	use(read)
	use(values)
}

// A comparison with the result is single-valued.
func compared(value any) bool {
	return value.(*node) != nil // want `not of type .\*node.`
}
