// want `^File name "util_test\.go" holds "util",`

// A test file's _test is go test's, and the name before it is still the author's.
package flagged

import "testing"

func TestParse(test *testing.T) {
	test.Parallel()
	if Parse("text") != "text" {
		test.Fatal("Parse changed its text")
	}
}
