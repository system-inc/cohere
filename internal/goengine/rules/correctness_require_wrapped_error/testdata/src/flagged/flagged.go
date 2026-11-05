package flagged

import (
	"errors"
	"fmt"
	"os"
)

type parseError struct{ line int }

func (problem *parseError) Error() string { return fmt.Sprint("line ", problem.line) }

type holder struct{ cause error }

// Every verb fmt answers with the error's text drops the error, with or without flags.
func verbs(readError error) []error {
	return []error{
		fmt.Errorf("reading config: %v", readError),   // want `formatted with .%v., which keeps its text`
		fmt.Errorf("reading config: %s", readError),   // want `formatted with .%s.`
		fmt.Errorf("reading config: %q", readError),   // want `formatted with .%q.`
		fmt.Errorf("reading config: %x", readError),   // want `formatted with .%x.`
		fmt.Errorf("reading config: %X", readError),   // want `formatted with .%X.`
		fmt.Errorf("reading config: %+v", readError),  // want `formatted with .%\+v.`
		fmt.Errorf("reading config: %-8s", readError), // want `formatted with .%-8s.`
	}
}

// An explicit index names the argument, and the verbs after it continue from there, as fmt reads them.
func indexes(path string, readError error) []error {
	return []error{
		fmt.Errorf("%[2]s: reading %[1]q", path, readError), // want `formatted with .%\[2\]s.`
		fmt.Errorf("%[1]s then %v", path, readError),        // want `formatted with .%v.`
		fmt.Errorf("%[2]v, %[1]s", readError, path),         // want `formatted with .%\[1\]s.`
	}
}

// The same error printed twice is one finding, at the argument.
func twice(readError error) error {
	return fmt.Errorf("%[1]v (%[1]s)", readError) // want `formatted with .%\[1\]v.`
}

// A concrete error type, a pointer to one from the standard library, a field, and a type parameter constrained by
// error are all errors by their static type.
func concrete(problem *parseError, pathError *os.PathError, container holder) []error {
	return []error{
		fmt.Errorf("parsing: %v", problem),            // want `formatted with .%v.`
		fmt.Errorf("opening: %v", pathError),          // want `formatted with .%v.`
		fmt.Errorf("loading: %v", container.cause),    // want `formatted with .%v.`
		fmt.Errorf("parsing: %s", &parseError{}),      // want `formatted with .%s.`
		fmt.Errorf("closing: %v", errors.New("gone")), // want `formatted with .%v.`
	}
}

func generic[Failure error](failure Failure) error {
	return fmt.Errorf("running: %v", failure) // want `formatted with .%v.`
}

// Wrapping one error does not wrap another beside it.
func partly(first error, second error) error {
	return fmt.Errorf("%w; then %v", first, second) // want `formatted with .%v.`
}

// Two unwrapped errors in one call are two findings.
func both(first error, second error) error {
	return fmt.Errorf("%v; then %s", first, second) // want `formatted with .%v.` `formatted with .%s.`
}

// Rebuilding an error from its text drops the original, through errors.New or fmt.Errorf, directly, inside a
// concatenation, through parentheses, or as a formatted argument.
func rebuilds(readError error, problem *parseError) []error {
	return []error{
		errors.New(readError.Error()),                // want `from the text of .readError.`
		errors.New("reading: " + readError.Error()),  // want `from the text of .readError.`
		errors.New((readError.Error())),              // want `from the text of .readError.`
		errors.New(problem.Error()),                  // want `from the text of .problem.`
		fmt.Errorf(readError.Error()),                // want `from the text of .readError.`
		fmt.Errorf("reading: " + readError.Error()),  // want `from the text of .readError.`
		fmt.Errorf("reading: %s", readError.Error()), // want `from the text of .readError.`
	}
}

// A closure is no shelter.
func enclosed(readError error) func() error {
	return func() error {
		return fmt.Errorf("later: %v", readError) // want `formatted with .%v.`
	}
}
