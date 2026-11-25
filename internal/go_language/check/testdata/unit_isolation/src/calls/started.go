package calls

import "testing"

// stops stops the test it is given, which the resolver reads from its body. The pin stubs it and keeps Starts.
func stops(test *testing.T) {
	test.FailNow()
}

// Starts starts stops as a goroutine, where it may not stop the test.
func Starts(test *testing.T) {
	go stops(test)
}
