package parity

import (
	"testing"

	"parity/producer"
)

// A result of a function never returning a nil interface is left alone in a test, which may be enforcing on purpose
// that it is not nil; a value made from a concrete value is reported in a test too.
func TestComparisons(test *testing.T) {
	if typedNil() == nil {
		test.Fatal("nil")
	}
	_ = producer.TypedNil() != nil
	var made interface{} = 1
	_ = made == nil
}
