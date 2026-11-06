package lookalike

import (
	"errors"
	"fmt"
	"log"
)

var cause = errors.New("permission denied")

// A function named New or Errorf in this package is not the standard library's, whatever its text says.
func New(text string) error { return nil }

func Errorf(format string, arguments ...any) error { return nil }

// A method of the same name is not either.
type builder struct{}

func (current builder) New(text string) error { return nil }

func (current builder) Errorf(format string, arguments ...any) error { return nil }

func sameNames() []error {
	making := builder{}
	return []error{
		New("Failed to open."),
		Errorf("Failed to open: %w.", cause),
		making.New("Failed to open."),
		making.Errorf("Failed to open: %w.", cause),
	}
}

// Formatting that makes no error writes a sentence, where a capital and a period belong.
func otherFormatting() string {
	log.Printf("Failed to open %s.", "config")
	fmt.Println("Error: the config is missing.")
	return fmt.Sprintf("Could not open %s.", "config")
}

// A text that is not written where the error is made is not judged: a named constant, a variable, a text built with
// a variable, and a call. The finding would land away from the words, or on words the rule cannot see.
const openingFailed = "Failed to open."

func notLiteral(name string) []error {
	text := "Failed to open."
	return []error{
		errors.New(openingFailed),
		errors.New(text),
		errors.New("Opening " + name + "."),
		fmt.Errorf(text, cause),
		errors.New(fmt.Sprint("Failed to open.")),
	}
}

// A constructor held in a variable is called through the variable, whose value the rule does not trace.
func throughVariable() error {
	construct := errors.New
	return construct("Failed to open.")
}

// The standard library's other error functions take no text of their own.
func otherErrorFunctions(target error) bool {
	return errors.Is(cause, target) || errors.Unwrap(cause) != nil || errors.Join(cause) != nil
}
