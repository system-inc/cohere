package tests

import "testing"

func TestTotal(test *testing.T) {
	if (record{value: 1}).total(2) != 3 {
		test.Error("total")
	}
}

var testInitialized = initialized + 1
