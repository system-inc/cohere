package clean

import "testing"

func TestDouble(test *testing.T) {
	test.Parallel()
	if Double(2) != 4 {
		test.Fatal("two doubled is not four")
	}
}
