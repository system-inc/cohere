package store

import "testing"

func TestParseURLKeepsAShortText(test *testing.T) {
	test.Parallel()
	if len(ParseURL("short")) > maxLen {
		test.Fatal("a short text came back long")
	}
}
