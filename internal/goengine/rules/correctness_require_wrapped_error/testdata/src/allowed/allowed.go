package allowed

import (
	"errors"
	"fmt"
	"time"
)

type holder struct{ cause error }

// %w keeps the chain, alone, twice, and through an explicit index.
func wrapped(first error, second error, path string) []error {
	return []error{
		fmt.Errorf("reading config: %w", first),
		fmt.Errorf("%w; then %w", first, second),
		fmt.Errorf("%[2]w: reading %[1]q", path, first),
	}
}

// %T and %p describe the error rather than stand in for it, and %#v prints it as a Go value, which %w would not
// reproduce.
func described(readError error) []error {
	return []error{
		fmt.Errorf("unexpected %T", readError),
		fmt.Errorf("error at %p", readError),
		fmt.Errorf("unexpected %#v", readError),
	}
}

// An error the same call also wraps keeps its chain, so printing it again, or its text, loses nothing.
func alsoWrapped(readError error, container holder) []error {
	return []error{
		fmt.Errorf("%v: %w", readError, readError),
		fmt.Errorf("%s: %w", readError.Error(), readError),
		fmt.Errorf("%v: %w", container.cause, container.cause),
	}
}

// Arguments that are no error: strings, numbers, a Stringer, a duration, and a percent sign that takes nothing.
func notErrors(path string, count int, elapsed time.Duration, readError error) []error {
	return []error{
		fmt.Errorf("reading %s: %d lines", path, count),
		fmt.Errorf("took %v", elapsed),
		fmt.Errorf("100%% of %5d: %w", count, readError),
		fmt.Errorf("%-10s|%+.2f", path, 1.5),
	}
}

// A constant message, and an error held in an any, whose static type says nothing about being an error.
func constant(readError error) []error {
	var held any = readError
	return []error{
		errors.New("reading config"),
		fmt.Errorf("reading config"),
		fmt.Errorf("held: %v", held),
		fmt.Errorf("nothing: %v", nil),
	}
}
