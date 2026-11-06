package top

import "testing"

func TestTop(test *testing.T) {
	if Top() == "" {
		test.Fatal("Top is empty")
	}
}
