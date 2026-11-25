// Package differences holds the defers where the port and the original could plausibly disagree, and records no
// difference, since there is none: both print the message with staticcheck's own printer and read the same types.
// The message prints the call's function and the deferred call as written, comments, generic arguments, receivers
// built by expressions and literals included, so any difference in printing would show here.
package differences

import "os"

type opener struct{}

func (opener) open(name string) (*os.File, error) { return nil, nil }

func openPair[K comparable, V any](key K, value V) (*os.File, error) { return nil, nil }

// Found by both: a method of a value built by a composite literal.
func compositeReceiver() {
	file, err := opener{}.open("name")
	defer file.Close()
	_ = err
}

// Found by both: two explicit type arguments.
func twoTypeArguments() {
	file, err := openPair[string, int]("key", 1)
	defer file.Close()
	_ = err
}

// Found by both: a comment inside the deferred call, which the printer leaves out.
func commentInside() {
	file, err := os.Open("path")
	defer file.Close( /* nothing to pass */ )
	_ = err
}

// Found by both: a function literal called at once.
func literalCalled() {
	file, err := func() (*os.File, error) { return nil, nil }()
	defer file.Close()
	_ = err
}
