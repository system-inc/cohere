package flagged

import "testing"

// A test's name is ours: only its Test prefix is go test's grammar.
func TestParseURL(test *testing.T) { // want `"TestParseURL" .* Use "TestParseUrl"`
	test.Parallel()
}

// A function named Examples is a helper, since go test reads Example followed by a lower-case letter as no example.
func ExamplesOfJSON() {} // want `"ExamplesOfJSON" .* Use "ExamplesOfJson"`
