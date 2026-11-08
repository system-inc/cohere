package seed

import "testing"

// TestValue gives seed a test variant, so the plain seed its importer imports is no package the load analyzes.
func TestValue(test *testing.T) {
	if Value() != 1 {
		test.Fatal("Value is not 1")
	}
}
