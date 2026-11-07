package store_test

import (
	"testing"

	"example.com/rename/internal/store"
)

func TestFetchParses(test *testing.T) {
	test.Parallel()
	if store.ParseURL(store.Fetch()) == "" {
		test.Fatal("nothing was fetched")
	}
}
