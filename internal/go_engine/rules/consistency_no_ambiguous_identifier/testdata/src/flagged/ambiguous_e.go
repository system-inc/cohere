package flagged

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// An error constructor's name says only that an error was made, so the function the e is declared in names it.
func parseHeader(text string) error {
	e := fmt.Errorf("reading %q", text) // want `\(appears to be an error\)\. .* Rename it to .parseHeaderError.\.$`
	return e
}

func openArchive() error {
	var e = errors.New("closed") // want `\(appears to be an error\)\. .* Rename it to .openArchiveError.\.$`
	return e
}

type batch struct{ failures []error }

// Inside a method's body the e is not its receiver, so the method's name names it.
func (current batch) combine() error {
	e := errors.Join(current.failures...) // want `\(appears to be an error\)\. .* Rename it to .combineError.\.$`
	return e
}

// An e an error-returning call produced is named for the call. The first three wants overlap to read the whole text
// between them, each a part that fits on its line.
func remove(path string) error {
	e := os.Remove(path) // want `^The name .e. is too ambiguous \(appears to be an error\)\. It reads as`
	return e
}

func parse(text string) int {
	_, e := strconv.Atoi(text) // want `as an error by habit, so a reader has to find the declaration to learn whether`
	if e != nil {
		return 0
	}
	return 1
}

func changeDirectory(directory string) error {
	var e = os.Chdir(directory) // want `whether it holds one or something else\. Rename it to .chdirError.\.$`
	return e
}

func atoi(text string) error {
	_, e := strconv.Atoi(text) // want `\(appears to be an error\)\. .* Rename it to .atoiError.\.$`
	return e
}

// A function held in a variable gives its own name.
func run(step func() error) error {
	e := step() // want `\(appears to be an error\)\. .* Rename it to .stepError.\.$`
	return e
}

// A call of a literal has no name, so the function the e is declared in names it.
func immediate() error {
	e := func() error { return nil }() // want `\(appears to be an error\)\. .* Rename it to .immediateError.\.$`
	return e
}

// A parameter is named for its function, and an initialism opening the name lowers whole.
func HTTPReport(e error) string { // want `\(appears to be an error\)\. .* Rename it to .httpReportError.\.$`
	return e.Error()
}

// A receiver is named for its type, since an error type's method is usually Error.
type parseFailure struct{ text string }

func (e *parseFailure) Error() string { // want `\(appears to be an error\)\. .* Rename it to .parseFailure.\.$`
	return e.text
}

// An interface that embeds error is an error.
type wrapped interface {
	error
	Unwrap() error
}

func unwrap(e wrapped) error { // want `\(appears to be an error\)\. .* Rename it to .unwrapError.\.$`
	return e.Unwrap()
}

// A field is declared in no function, so nothing around it names what failed.
type holder struct {
	e error // want `\(appears to be an error\)\. .* Rename it to a descriptive name\.$`
}

// An e that is no error leaves the guess unclear.
func count(items []string) int {
	e := len(items) // want `^The name .e. is too ambiguous \(context unclear\)\. .* Rename it to a descriptive name\.$`
	return e
}

func each(items []string) int {
	total := 0
	for _, e := range items { // want `\(context unclear\)\. .* Rename it to a descriptive name\.$`
		total += len(e)
	}
	return total
}

// A type switch's e is an error when every clause gives it an error type.
func classify(value any) string {
	switch e := value.(type) { // want `\(appears to be an error\)\. .* Rename it to .classifyError.\.$`
	case *os.PathError:
		return e.Path
	case error:
		return e.Error()
	}
	return ""
}

// One clause that is no error leaves it unclear.
func describe(value any) string {
	switch e := value.(type) { // want `\(context unclear\)\. .* Rename it to a descriptive name\.$`
	case error:
		return e.Error()
	case string:
		return e
	}
	return ""
}
