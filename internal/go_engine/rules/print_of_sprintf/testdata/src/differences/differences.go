// Package differences holds the shapes where the port could most plausibly part from the original: the messages that
// render the program's own syntax, a receiver of a generic type built in place, a variable that shadows the log
// package, a renamed import and an alias. The two agree on every one, so nothing here records a difference.
package differences

import (
	"fmt"
	format "fmt"
	"log"
	"testing"
)

// Generic embeds a test in a generic type.
type Generic[Value any] struct {
	*testing.T
	value Value
}

// Logged embeds a logger.
type Logged struct {
	*log.Logger
}

// Alias is another name for a test.
type Alias = *testing.T

func testOf(test *testing.T) *testing.T { return test }

func shapes(test *testing.T, aliased Alias) {
	// The receiver is rendered as the program writes it, over more than one line.
	testOf(
		test,
	).Error(fmt.Sprintf("%d", 1))
	// A composite literal of a generic type is no addressable receiver, and its embedded pointer still promotes Errorf.
	Generic[int]{T: test}.Error(fmt.Sprintf("%d", 1))
	(Generic[int]{T: test}).Log(fmt.Sprintf("%d", 1))
	aliased.Fatal(fmt.Sprintf("%d", 1))

	// A variable named log is a logger, and its Print is the method form's, in the log form's words.
	{
		log := Logged{}
		log.Print(fmt.Sprintf("%d", 1))
		log.Println(fmt.Sprintf("%d", 1))
	}

	// A renamed import still names fmt in the message.
	format.Print(format.Sprintf("%d", 1))
	format.Println(format.Sprintf("%d", 1))
	fmt.Println(fmt.Sprintf("%d"+"%d", 1, 2))
	fmt.Println(fmt.Sprintf(""))
}
