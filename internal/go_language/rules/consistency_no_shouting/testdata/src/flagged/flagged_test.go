package flagged

import "fmt"

// go/doc reads an example's output only from the last comment group in its body, so an earlier one is a comment.
func ExampleOutputNotLast() {
	// Output: HELLO // want `^"HELLO" in a comment`
	fmt.Println("hello")
	// the last comment is ALWAYS read // want `^"ALWAYS" in a comment`
}

// A function that is not an example has no output, whatever its comment calls itself.
func printsHello() {
	fmt.Println("HELLO")
	// Output: HELLO // want `^"HELLO" in a comment`
}

// An example method is no example: go/doc reads only functions.
type greeter struct{}

func (greeter) ExampleGreet() {
	fmt.Println("HELLO")
	// Output: HELLO // want `^"HELLO" in a comment`
}
