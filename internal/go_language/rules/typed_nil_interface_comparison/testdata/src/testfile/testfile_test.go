package testfile

import "testing"

// The result of typedNil is left alone in a test, which may be enforcing on purpose that it is not nil, so its nilness
// is never asked; a value made from a concrete value is reported in a test too, which shows the rule walked the file.
func TestComparisons(test *testing.T) {
	if typedNil() == nil {
		test.Fatal("nil")
	}
	var made interface{} = 1
	_ = made == nil
}
