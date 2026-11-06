package top_test

import (
	"testing"

	"example.com/chain/top"
)

func TestTopFromOutside(test *testing.T) {
	if top.Top() == "" {
		test.Fatal("Top is empty")
	}
}
