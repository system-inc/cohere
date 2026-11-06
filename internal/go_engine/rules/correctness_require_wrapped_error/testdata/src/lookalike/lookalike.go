package lookalike

import (
	"errors"
	"fmt"
)

// A * width or precision takes an argument and shifts every verb after it, a missing argument or an index out of
// range or malformed leaves a verb with no argument, and a trailing percent has no verb. vet's printf reports each
// of those; this rule cannot pair verbs with arguments for certain, so it stays quiet on the whole call.
func uncertain(readError error, values []any) []error {
	return []error{
		fmt.Errorf("%*v", 5, readError),
		fmt.Errorf("%.*v", 2, readError),
		fmt.Errorf("%[1]*v", 5, readError),
		fmt.Errorf("%v %v", readError),
		fmt.Errorf("%[3]v", readError),
		fmt.Errorf("%[0]v", readError),
		fmt.Errorf("%[x]v", readError),
		fmt.Errorf("%[1v", readError),
		fmt.Errorf("%[1]5v", readError),
		fmt.Errorf("reading %v %", readError),
		fmt.Errorf("reading %v", values...),
	}
}

// These wrap correctly, and a reading that let * take no argument would pair the %s with the error instead of the
// path, which is why a * leaves the call unjudged rather than guessed at.
func starred(readError error, count int, ratio float64, path string) []error {
	return []error{
		fmt.Errorf("%*d: %w (%s)", 4, count, readError, path),
		fmt.Errorf("%.*f: %w (%s)", 2, ratio, readError, path),
	}
}

// A format that is not a constant is no format the rule can read.
func variableFormat(readError error) error {
	format := "reading: %v"
	return fmt.Errorf(format, readError)
}

// Formatting an error into a string or onto output makes no new error, so there is no chain to keep.
func notErrorConstruction(readError error) string {
	fmt.Printf("reading: %v\n", readError)
	return fmt.Sprintf("reading: %v", readError)
}

// An Errorf and a New of our own, a method named Errorf, and a function value are none of fmt's or errors'.
func Errorf(format string, arguments ...any) error { return nil }

func New(text string) error { return nil }

type logger struct{}

func (current logger) Errorf(format string, arguments ...any) {}

func (current logger) New(text string) error { return nil }

func ours(readError error, log logger) []error {
	log.Errorf("reading: %v", readError)
	return []error{
		Errorf("reading: %v", readError),
		New(readError.Error()),
		log.New("reading"),
	}
}

// An Error that takes an argument, one that returns no string, and a field holding a function named Error are not
// the error interface's method, so their results are no error's text.
type report struct{ code int }

func (current report) Error(code int) string { return fmt.Sprint(code) }

type counter struct{}

func (current counter) Error() int { return 0 }

type record struct {
	Error   func() string
	Message string
}

func notErrorText(current report, count counter, entry record) []error {
	return []error{
		errors.New(current.Error(3)),
		errors.New(fmt.Sprint(count.Error())),
		errors.New(entry.Error()),
		errors.New(entry.Message),
	}
}
