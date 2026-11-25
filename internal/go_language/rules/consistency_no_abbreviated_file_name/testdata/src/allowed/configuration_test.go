// A test file spelled in full.
package allowed

import "testing"

func TestPrint(test *testing.T) {
	test.Parallel()
	if Print("text") != "text" {
		test.Fatal("Print changed its text")
	}
}
