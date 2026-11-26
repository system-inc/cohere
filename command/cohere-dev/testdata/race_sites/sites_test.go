package sites

import "testing"

// A fixture for the -race list's guard: two tests start goroutines of their own, one through two helpers.

func TestDirect(test *testing.T) {
	go func() {}()
}

func TestThroughHelpers(test *testing.T) {
	outer()
}

func outer() { inner() }

func inner() {
	go func() {}()
}

func TestFansOut(test *testing.T) { fanOut([]int{1, 2}) }

func TestRuns(test *testing.T) { (&pool{}).run(&pool{}) }

func TestPlain(test *testing.T) { plain() }
